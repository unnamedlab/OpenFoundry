//! CRUD over the `schedules` table — the declarative source of truth
//! for every schedule. The trigger / target shape lives in
//! [`super::trigger`]. Versioning is delegated to the
//! `schedules_version_snapshot` BEFORE-UPDATE trigger; we just bump
//! `version` here so the snapshot row carries the prior version
//! number.

use chrono::{DateTime, Utc};
use serde_json::Value as JsonValue;
use sqlx::{PgPool, Row, postgres::PgRow};
use uuid::Uuid;

use super::trigger::{Schedule, ScheduleScopeKind, ScheduleTarget, Trigger};

#[derive(Debug, thiserror::Error)]
pub enum StoreError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("invalid trigger payload: {0}")]
    InvalidTrigger(serde_json::Error),
    #[error("invalid target payload: {0}")]
    InvalidTarget(serde_json::Error),
    #[error("schedule '{0}' not found")]
    NotFound(String),
}

#[derive(Debug, Clone)]
pub struct CreateSchedule {
    pub project_rid: String,
    pub name: String,
    pub description: String,
    pub trigger: Trigger,
    pub target: ScheduleTarget,
    pub paused: bool,
    pub created_by: String,
    /// Override the `run_as_user_id`. Defaults to `created_by` parsed
    /// as a UUID when `None`.
    #[allow(dead_code)]
    pub run_as_user_id: Option<Uuid>,
}

#[derive(Debug, Default, Clone)]
pub struct UpdateSchedule {
    pub name: Option<String>,
    pub description: Option<String>,
    pub trigger: Option<Trigger>,
    pub target: Option<ScheduleTarget>,
    pub paused: Option<bool>,
    pub edited_by: String,
    pub change_comment: String,
}

#[derive(Debug, Default, Clone)]
pub struct ListFilter {
    pub project_rid: Option<String>,
    pub paused: Option<bool>,
    pub owner: Option<String>,
    pub query: Option<String>,
    pub limit: i64,
    pub offset: i64,
}

/// Column list selected by every read path. Kept in one constant so
/// the auto-pause / coalesce columns added in P2 stay in sync across
/// `create`, `get_by_rid`, `list`, `update`, and the event_listener
/// JSONB scan.
pub(crate) const SCHEDULE_COLUMNS: &str = "id, rid, project_rid, name, description, \
                  trigger_json, target_json, paused, version, \
                  created_by, created_at, updated_at, last_run_at, \
                  paused_reason, paused_at, auto_pause_exempt, \
                  pending_re_run, active_run_id, \
                  scope_kind, project_scope_rids, run_as_user_id, \
                  service_principal_id";

pub async fn create(pool: &PgPool, req: CreateSchedule) -> Result<Schedule, StoreError> {
    let id = Uuid::now_v7();
    let trigger_json = serde_json::to_value(&req.trigger).map_err(StoreError::InvalidTrigger)?;
    let target_json = serde_json::to_value(&req.target).map_err(StoreError::InvalidTarget)?;
    // Default a USER-mode schedule's `run_as_user_id` to the creator
    // when the caller omitted an explicit override (the common case).
    let run_as = req
        .run_as_user_id
        .or_else(|| Uuid::parse_str(&req.created_by).ok());
    let sql = format!(
        "INSERT INTO schedules (
            id, project_rid, name, description,
            trigger_json, target_json, paused, version,
            created_by, created_at, updated_at,
            run_as_user_id
         ) VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, NOW(), NOW(), $9)
         RETURNING {SCHEDULE_COLUMNS}"
    );
    let row = sqlx::query(&sql)
        .bind(id)
        .bind(&req.project_rid)
        .bind(&req.name)
        .bind(&req.description)
        .bind(trigger_json)
        .bind(target_json)
        .bind(req.paused)
        .bind(&req.created_by)
        .bind(run_as)
        .fetch_one(pool)
        .await?;
    schedule_from_row(&row)
}

pub async fn get_by_rid(pool: &PgPool, rid: &str) -> Result<Schedule, StoreError> {
    let sql = format!("SELECT {SCHEDULE_COLUMNS} FROM schedules WHERE rid = $1");
    let row = sqlx::query(&sql).bind(rid).fetch_optional(pool).await?;
    match row {
        Some(r) => schedule_from_row(&r),
        None => Err(StoreError::NotFound(rid.to_string())),
    }
}

pub async fn get_by_id(pool: &PgPool, id: Uuid) -> Result<Schedule, StoreError> {
    let sql = format!("SELECT {SCHEDULE_COLUMNS} FROM schedules WHERE id = $1");
    let row = sqlx::query(&sql).bind(id).fetch_optional(pool).await?;
    match row {
        Some(r) => schedule_from_row(&r),
        None => Err(StoreError::NotFound(id.to_string())),
    }
}

pub async fn list(pool: &PgPool, filter: ListFilter) -> Result<Vec<Schedule>, StoreError> {
    let limit = if filter.limit <= 0 {
        50
    } else {
        filter.limit.min(500)
    };
    let offset = filter.offset.max(0);
    let q_pattern = filter.query.as_deref().map(|s| format!("%{s}%"));
    let sql = format!(
        "SELECT {SCHEDULE_COLUMNS} FROM schedules
         WHERE ($1::TEXT IS NULL OR project_rid = $1)
           AND ($2::BOOL IS NULL OR paused = $2)
           AND ($3::TEXT IS NULL OR created_by = $3)
           AND ($4::TEXT IS NULL OR name ILIKE $4)
         ORDER BY updated_at DESC
         LIMIT $5 OFFSET $6"
    );
    let rows = sqlx::query(&sql)
        .bind(filter.project_rid)
        .bind(filter.paused)
        .bind(filter.owner)
        .bind(q_pattern)
        .bind(limit)
        .bind(offset)
        .fetch_all(pool)
        .await?;
    rows.iter().map(schedule_from_row).collect()
}

pub async fn update(
    pool: &PgPool,
    rid: &str,
    patch: UpdateSchedule,
) -> Result<Schedule, StoreError> {
    let mut tx = pool.begin().await?;

    // Set per-statement settings so the BEFORE-UPDATE trigger can pick
    // up `edited_by` and `change_comment` for the snapshot row.
    sqlx::query("SELECT set_config('app.editor', $1, true)")
        .bind(&patch.edited_by)
        .execute(&mut *tx)
        .await?;
    sqlx::query("SELECT set_config('app.change_comment', $1, true)")
        .bind(&patch.change_comment)
        .execute(&mut *tx)
        .await?;

    let trigger_json: Option<JsonValue> = match patch.trigger {
        Some(t) => Some(serde_json::to_value(&t).map_err(StoreError::InvalidTrigger)?),
        None => None,
    };
    let target_json: Option<JsonValue> = match patch.target {
        Some(t) => Some(serde_json::to_value(&t).map_err(StoreError::InvalidTarget)?),
        None => None,
    };

    let sql = format!(
        "UPDATE schedules SET
              name         = COALESCE($1, name),
              description  = COALESCE($2, description),
              trigger_json = COALESCE($3, trigger_json),
              target_json  = COALESCE($4, target_json),
              paused       = COALESCE($5, paused),
              version      = version + 1
         WHERE rid = $6
         RETURNING {SCHEDULE_COLUMNS}"
    );
    let row = sqlx::query(&sql)
        .bind(patch.name)
        .bind(patch.description)
        .bind(trigger_json)
        .bind(target_json)
        .bind(patch.paused)
        .bind(rid)
        .fetch_optional(&mut *tx)
        .await?;

    let schedule = match row {
        Some(r) => schedule_from_row(&r)?,
        None => {
            tx.rollback().await?;
            return Err(StoreError::NotFound(rid.to_string()));
        }
    };

    tx.commit().await?;
    Ok(schedule)
}

pub async fn delete(pool: &PgPool, rid: &str) -> Result<(), StoreError> {
    let res = sqlx::query("DELETE FROM schedules WHERE rid = $1")
        .bind(rid)
        .execute(pool)
        .await?;
    if res.rows_affected() == 0 {
        return Err(StoreError::NotFound(rid.to_string()));
    }
    Ok(())
}

pub async fn mark_run(pool: &PgPool, rid: &str, run_at: DateTime<Utc>) -> Result<(), StoreError> {
    sqlx::query("UPDATE schedules SET last_run_at = $1 WHERE rid = $2")
        .bind(run_at)
        .bind(rid)
        .execute(pool)
        .await?;
    Ok(())
}

fn schedule_from_row(row: &PgRow) -> Result<Schedule, StoreError> {
    let trigger_json: JsonValue = row.try_get("trigger_json")?;
    let target_json: JsonValue = row.try_get("target_json")?;
    let trigger: Trigger =
        serde_json::from_value(trigger_json).map_err(StoreError::InvalidTrigger)?;
    let target: ScheduleTarget =
        serde_json::from_value(target_json).map_err(StoreError::InvalidTarget)?;

    let scope_kind_str: String = row.try_get("scope_kind")?;
    let scope_kind = ScheduleScopeKind::parse(&scope_kind_str).unwrap_or(ScheduleScopeKind::User);

    Ok(Schedule {
        id: row.try_get("id")?,
        rid: row.try_get("rid")?,
        project_rid: row.try_get("project_rid")?,
        name: row.try_get("name")?,
        description: row.try_get("description")?,
        trigger,
        target,
        paused: row.try_get("paused")?,
        version: row.try_get("version")?,
        created_by: row.try_get("created_by")?,
        created_at: row.try_get("created_at")?,
        updated_at: row.try_get("updated_at")?,
        last_run_at: row.try_get("last_run_at")?,
        paused_reason: row.try_get("paused_reason")?,
        paused_at: row.try_get("paused_at")?,
        auto_pause_exempt: row.try_get("auto_pause_exempt")?,
        pending_re_run: row.try_get("pending_re_run")?,
        active_run_id: row.try_get("active_run_id")?,
        scope_kind,
        project_scope_rids: row.try_get("project_scope_rids")?,
        run_as_user_id: row.try_get("run_as_user_id")?,
        service_principal_id: row.try_get("service_principal_id")?,
    })
}

// ---- pause / resume primitives ---------------------------------------------

/// Set the paused flag plus its reason in one statement. Used by the
/// manual pause/resume endpoint and the auto-pause supervisor.
pub async fn set_paused(
    pool: &PgPool,
    rid: &str,
    paused: bool,
    reason: Option<&str>,
) -> Result<Schedule, StoreError> {
    let sql = format!(
        "UPDATE schedules SET
            paused        = $1,
            paused_reason = $2,
            paused_at     = CASE WHEN $1 THEN NOW() ELSE NULL END
         WHERE rid = $3
         RETURNING {SCHEDULE_COLUMNS}"
    );
    let row = sqlx::query(&sql)
        .bind(paused)
        .bind(reason)
        .bind(rid)
        .fetch_optional(pool)
        .await?;
    match row {
        Some(r) => schedule_from_row(&r),
        None => Err(StoreError::NotFound(rid.to_string())),
    }
}

pub async fn set_auto_pause_exempt(
    pool: &PgPool,
    rid: &str,
    exempt: bool,
) -> Result<Schedule, StoreError> {
    let sql = format!(
        "UPDATE schedules SET auto_pause_exempt = $1
         WHERE rid = $2
         RETURNING {SCHEDULE_COLUMNS}"
    );
    let row = sqlx::query(&sql)
        .bind(exempt)
        .bind(rid)
        .fetch_optional(pool)
        .await?;
    match row {
        Some(r) => schedule_from_row(&r),
        None => Err(StoreError::NotFound(rid.to_string())),
    }
}

// ---- coalesce / active-run primitives --------------------------------------

pub async fn set_active_run(
    pool: &PgPool,
    schedule_id: Uuid,
    run_id: Option<Uuid>,
) -> Result<(), StoreError> {
    sqlx::query("UPDATE schedules SET active_run_id = $1 WHERE id = $2")
        .bind(run_id)
        .bind(schedule_id)
        .execute(pool)
        .await?;
    Ok(())
}

pub async fn set_pending_re_run(
    pool: &PgPool,
    schedule_id: Uuid,
    pending: bool,
) -> Result<(), StoreError> {
    sqlx::query("UPDATE schedules SET pending_re_run = $1 WHERE id = $2")
        .bind(pending)
        .bind(schedule_id)
        .execute(pool)
        .await?;
    Ok(())
}

// ---- scope conversion ------------------------------------------------------

/// Flip a schedule from `User` to `ProjectScoped`. Sets the new
/// project_scope_rids and service_principal_id, and nulls out
/// `run_as_user_id` to keep the CHECK constraint happy.
pub async fn convert_to_project_scope(
    pool: &PgPool,
    rid: &str,
    project_scope_rids: Vec<String>,
    service_principal_id: Uuid,
) -> Result<Schedule, StoreError> {
    let sql = format!(
        "UPDATE schedules SET
            scope_kind           = 'PROJECT_SCOPED',
            project_scope_rids   = $1,
            run_as_user_id       = NULL,
            service_principal_id = $2,
            version              = version + 1
         WHERE rid = $3
         RETURNING {SCHEDULE_COLUMNS}"
    );
    let row = sqlx::query(&sql)
        .bind(&project_scope_rids)
        .bind(service_principal_id)
        .bind(rid)
        .fetch_optional(pool)
        .await?;
    match row {
        Some(r) => schedule_from_row(&r),
        None => Err(StoreError::NotFound(rid.to_string())),
    }
}

/// Flip a schedule back to `User` mode. The caller passes the user
/// the schedule should run as going forward (typically the original
/// creator).
pub async fn convert_to_user_scope(
    pool: &PgPool,
    rid: &str,
    run_as_user_id: Uuid,
) -> Result<Schedule, StoreError> {
    let sql = format!(
        "UPDATE schedules SET
            scope_kind           = 'USER',
            project_scope_rids   = '{{}}',
            run_as_user_id       = $1,
            service_principal_id = NULL,
            version              = version + 1
         WHERE rid = $2
         RETURNING {SCHEDULE_COLUMNS}"
    );
    let row = sqlx::query(&sql)
        .bind(run_as_user_id)
        .bind(rid)
        .fetch_optional(pool)
        .await?;
    match row {
        Some(r) => schedule_from_row(&r),
        None => Err(StoreError::NotFound(rid.to_string())),
    }
}
