//! Repository for ontology funnel definitions and run ledger.
//!
//! Object writes performed by funnel execution already go through the
//! Cassandra-backed `ObjectStore`. Funnel source definitions stay in
//! PostgreSQL, while the run ledger is reconstructed from the append-only
//! `actions_log` store.

use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::Deserialize;
use serde_json::Value;
use sqlx::PgPool;
use storage_abstraction::repositories::{
    ActionLogEntry, ActionLogStore, ObjectId, Page, ReadConsistency, RepoError, TenantId,
};
use uuid::Uuid;

use crate::models::funnel::{
    OntologyFunnelHealthMetricsRow, OntologyFunnelRun, OntologyFunnelSource,
    OntologyFunnelSourceRow,
};

#[derive(Debug, thiserror::Error)]
pub enum FunnelRepoError {
    #[error(transparent)]
    Sql(#[from] sqlx::Error),
    #[error(transparent)]
    Store(#[from] RepoError),
    #[error("failed to decode ontology funnel source: {0}")]
    DecodeSource(serde_json::Error),
}

pub struct ListSourcesParams<'a> {
    pub object_type_id: Option<Uuid>,
    pub status_filter: &'a str,
    pub is_admin: bool,
    pub actor_id: Uuid,
    pub offset: i64,
    pub limit: i64,
}

pub struct HealthSourcesParams {
    pub object_type_id: Option<Uuid>,
    pub is_admin: bool,
    pub actor_id: Uuid,
}

pub struct CreateSourceInput {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub object_type_id: Uuid,
    pub dataset_id: Uuid,
    pub pipeline_id: Option<Uuid>,
    pub dataset_branch: Option<String>,
    pub dataset_version: Option<i32>,
    pub preview_limit: i32,
    pub default_marking: String,
    pub status: String,
    pub property_mappings: Value,
    pub trigger_context: Value,
    pub owner_id: Uuid,
}

pub struct UpdateSourceInput {
    pub id: Uuid,
    pub name: Option<String>,
    pub description: Option<String>,
    pub pipeline_id: Option<Uuid>,
    pub dataset_branch: Option<String>,
    pub dataset_version: Option<i32>,
    pub preview_limit: i32,
    pub default_marking: String,
    pub status: String,
    pub property_mappings: Value,
    pub trigger_context: Value,
}

pub struct CreateRunInput {
    pub id: Uuid,
    pub source_id: Uuid,
    pub object_type_id: Uuid,
    pub dataset_id: Uuid,
    pub pipeline_id: Option<Uuid>,
    pub trigger_type: String,
    pub started_by: Uuid,
    pub details: Value,
}

pub struct CompleteRunInput {
    pub id: Uuid,
    pub source_id: Uuid,
    pub pipeline_run_id: Option<Uuid>,
    pub status: String,
    pub rows_read: i32,
    pub inserted_count: i32,
    pub updated_count: i32,
    pub skipped_count: i32,
    pub error_count: i32,
    pub details: Value,
    pub error_message: Option<String>,
    pub finished_at: DateTime<Utc>,
}

const FUNNEL_RUN_KIND: &str = "funnel_run";
const FUNNEL_RUN_STARTED_EVENT: &str = "funnel_run_started";
const FUNNEL_RUN_COMPLETED_EVENT: &str = "funnel_run_completed";
const FUNNEL_RUN_FAILED_EVENT: &str = "funnel_run_failed";
const ACTION_LOG_SCAN_PAGE_SIZE: u32 = 5_000;

#[derive(Debug, Deserialize)]
struct FunnelRunEventPayload {
    event: Option<String>,
    run_id: Uuid,
    source_id: Uuid,
    object_type_id: Option<Uuid>,
    dataset_id: Option<Uuid>,
    pipeline_id: Option<Uuid>,
    pipeline_run_id: Option<Uuid>,
    status: Option<String>,
    trigger_type: Option<String>,
    started_by: Option<Uuid>,
    rows_read: Option<i32>,
    inserted_count: Option<i32>,
    updated_count: Option<i32>,
    skipped_count: Option<i32>,
    error_count: Option<i32>,
    details: Option<Value>,
    error_message: Option<String>,
    started_at: Option<DateTime<Utc>>,
    finished_at: Option<DateTime<Utc>>,
}

#[derive(Debug)]
struct FunnelRunAccumulator {
    id: Uuid,
    source_id: Uuid,
    object_type_id: Option<Uuid>,
    dataset_id: Option<Uuid>,
    pipeline_id: Option<Uuid>,
    pipeline_run_id: Option<Uuid>,
    status: Option<String>,
    trigger_type: Option<String>,
    started_by: Option<Uuid>,
    rows_read: i32,
    inserted_count: i32,
    updated_count: i32,
    skipped_count: i32,
    error_count: i32,
    details: Value,
    error_message: Option<String>,
    started_at: Option<DateTime<Utc>>,
    finished_at: Option<DateTime<Utc>>,
}

impl FunnelRunAccumulator {
    fn new(id: Uuid, source_id: Uuid) -> Self {
        Self {
            id,
            source_id,
            object_type_id: None,
            dataset_id: None,
            pipeline_id: None,
            pipeline_run_id: None,
            status: None,
            trigger_type: None,
            started_by: None,
            rows_read: 0,
            inserted_count: 0,
            updated_count: 0,
            skipped_count: 0,
            error_count: 0,
            details: Value::Null,
            error_message: None,
            started_at: None,
            finished_at: None,
        }
    }

    fn apply(&mut self, payload: FunnelRunEventPayload, recorded_at: DateTime<Utc>) {
        let event = payload.event.clone();
        self.object_type_id = self.object_type_id.or(payload.object_type_id);
        self.dataset_id = self.dataset_id.or(payload.dataset_id);
        self.pipeline_id = self.pipeline_id.or(payload.pipeline_id);
        self.pipeline_run_id = self.pipeline_run_id.or(payload.pipeline_run_id);
        if self.trigger_type.is_none() {
            self.trigger_type = payload.trigger_type;
        }
        self.started_by = self.started_by.or(payload.started_by);

        match event.as_deref() {
            Some(FUNNEL_RUN_STARTED_EVENT) => {
                self.status
                    .get_or_insert_with(|| payload.status.unwrap_or_else(|| "running".to_string()));
                if self.details.is_null()
                    && let Some(details) = payload.details
                {
                    self.details = details;
                }
                self.started_at = self.started_at.or(payload.started_at).or(Some(recorded_at));
            }
            Some(FUNNEL_RUN_COMPLETED_EVENT) | Some(FUNNEL_RUN_FAILED_EVENT) => {
                if let Some(status) = payload.status {
                    self.status = Some(status);
                } else if event.as_deref() == Some(FUNNEL_RUN_FAILED_EVENT) {
                    self.status = Some("failed".to_string());
                }
                self.rows_read = payload.rows_read.unwrap_or(self.rows_read);
                self.inserted_count = payload.inserted_count.unwrap_or(self.inserted_count);
                self.updated_count = payload.updated_count.unwrap_or(self.updated_count);
                self.skipped_count = payload.skipped_count.unwrap_or(self.skipped_count);
                self.error_count = payload.error_count.unwrap_or(self.error_count);
                if let Some(details) = payload.details {
                    self.details = details;
                }
                self.error_message = payload.error_message.or(self.error_message.take());
                self.finished_at = payload.finished_at.or(Some(recorded_at));
            }
            _ => {
                if let Some(status) = payload.status {
                    self.status.get_or_insert(status);
                }
                self.started_at = self.started_at.or(payload.started_at);
                self.finished_at = self.finished_at.or(payload.finished_at);
                if self.details.is_null()
                    && let Some(details) = payload.details
                {
                    self.details = details;
                }
            }
        }
    }

    fn into_run(self) -> Option<OntologyFunnelRun> {
        Some(OntologyFunnelRun {
            id: self.id,
            source_id: self.source_id,
            object_type_id: self.object_type_id?,
            dataset_id: self.dataset_id?,
            pipeline_id: self.pipeline_id,
            pipeline_run_id: self.pipeline_run_id,
            status: self.status.unwrap_or_else(|| "running".to_string()),
            trigger_type: self.trigger_type.unwrap_or_else(|| "manual".to_string()),
            started_by: self.started_by,
            rows_read: self.rows_read,
            inserted_count: self.inserted_count,
            updated_count: self.updated_count,
            skipped_count: self.skipped_count,
            error_count: self.error_count,
            details: self.details,
            error_message: self.error_message,
            started_at: self.started_at?,
            finished_at: self.finished_at,
        })
    }
}

fn utc_from_millis(ms: i64) -> Option<DateTime<Utc>> {
    chrono::TimeZone::timestamp_millis_opt(&Utc, ms).single()
}

fn percentile_cont(sorted_values: &[f64], percentile: f64) -> Option<f64> {
    if sorted_values.is_empty() {
        return None;
    }
    if sorted_values.len() == 1 {
        return sorted_values.first().copied();
    }

    let rank = percentile.clamp(0.0, 1.0) * (sorted_values.len() - 1) as f64;
    let lower = rank.floor() as usize;
    let upper = rank.ceil() as usize;
    if lower == upper {
        return sorted_values.get(lower).copied();
    }

    let lower_value = sorted_values[lower];
    let upper_value = sorted_values[upper];
    Some(lower_value + (upper_value - lower_value) * (rank - lower as f64))
}

fn decode_source(row: OntologyFunnelSourceRow) -> Result<OntologyFunnelSource, FunnelRepoError> {
    OntologyFunnelSource::try_from(row).map_err(FunnelRepoError::DecodeSource)
}

fn entry_recorded_at(entry: &ActionLogEntry) -> Option<DateTime<Utc>> {
    utc_from_millis(entry.recorded_at_ms)
}

fn decode_funnel_event(entry: ActionLogEntry) -> Option<(FunnelRunEventPayload, DateTime<Utc>)> {
    if entry.kind != FUNNEL_RUN_KIND {
        return None;
    }
    let recorded_at = entry_recorded_at(&entry)?;
    let payload = serde_json::from_value::<FunnelRunEventPayload>(entry.payload).ok()?;
    Some((payload, recorded_at))
}

fn append_payload(
    input: &CreateRunInput,
    started_at: DateTime<Utc>,
) -> Result<Value, FunnelRepoError> {
    Ok(serde_json::json!({
        "event": FUNNEL_RUN_STARTED_EVENT,
        "run_id": input.id,
        "source_id": input.source_id,
        "object_type_id": input.object_type_id,
        "dataset_id": input.dataset_id,
        "pipeline_id": input.pipeline_id,
        "status": "running",
        "trigger_type": input.trigger_type,
        "started_by": input.started_by,
        "details": input.details,
        "started_at": started_at,
    }))
}

fn terminal_payload(event: &str, input: &CompleteRunInput) -> Result<Value, FunnelRepoError> {
    Ok(serde_json::json!({
        "event": event,
        "run_id": input.id,
        "source_id": input.source_id,
        "pipeline_run_id": input.pipeline_run_id,
        "status": input.status,
        "rows_read": input.rows_read,
        "inserted_count": input.inserted_count,
        "updated_count": input.updated_count,
        "skipped_count": input.skipped_count,
        "error_count": input.error_count,
        "details": input.details,
        "error_message": input.error_message,
        "finished_at": input.finished_at,
    }))
}

async fn append_funnel_event(
    actions: &dyn ActionLogStore,
    tenant: TenantId,
    source_id: Uuid,
    subject: String,
    payload: Value,
    recorded_at: DateTime<Utc>,
) -> Result<(), FunnelRepoError> {
    actions
        .append(ActionLogEntry {
            tenant,
            event_id: None,
            action_id: Uuid::now_v7().to_string(),
            kind: FUNNEL_RUN_KIND.to_string(),
            subject,
            object: Some(ObjectId(source_id.to_string())),
            payload,
            recorded_at_ms: recorded_at.timestamp_millis(),
        })
        .await?;
    Ok(())
}

async fn list_funnel_events_for_source(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    source_id: Uuid,
) -> Result<Vec<(FunnelRunEventPayload, DateTime<Utc>)>, FunnelRepoError> {
    let source_object = ObjectId(source_id.to_string());
    let mut token = None;
    let mut events = Vec::new();

    loop {
        let page = actions
            .list_for_object(
                tenant,
                &source_object,
                Page {
                    size: ACTION_LOG_SCAN_PAGE_SIZE,
                    token: token.clone(),
                },
                ReadConsistency::Strong,
            )
            .await?;

        events.extend(page.items.into_iter().filter_map(decode_funnel_event));

        match page.next_token {
            Some(next) => token = Some(next),
            None => return Ok(events),
        }
    }
}

async fn load_funnel_events_for_run(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    run_id: Uuid,
) -> Result<Vec<(FunnelRunEventPayload, DateTime<Utc>)>, FunnelRepoError> {
    let mut token = None;
    let mut events = Vec::new();

    loop {
        let page = actions
            .list_recent(
                tenant,
                Page {
                    size: ACTION_LOG_SCAN_PAGE_SIZE,
                    token: token.clone(),
                },
                ReadConsistency::Strong,
            )
            .await?;

        for item in page.items {
            let Some((payload, recorded_at)) = decode_funnel_event(item) else {
                continue;
            };
            if payload.run_id == run_id {
                events.push((payload, recorded_at));
                if events
                    .iter()
                    .any(|(event, _)| event.event.as_deref() == Some(FUNNEL_RUN_STARTED_EVENT))
                {
                    return Ok(events);
                }
            }
        }

        match page.next_token {
            Some(next) => token = Some(next),
            None => return Ok(events),
        }
    }
}

async fn list_funnel_events_for_tenant(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
) -> Result<Vec<(FunnelRunEventPayload, DateTime<Utc>)>, FunnelRepoError> {
    let mut token = None;
    let mut events = Vec::new();

    loop {
        let page = actions
            .list_recent(
                tenant,
                Page {
                    size: ACTION_LOG_SCAN_PAGE_SIZE,
                    token: token.clone(),
                },
                ReadConsistency::Strong,
            )
            .await?;

        events.extend(page.items.into_iter().filter_map(decode_funnel_event));

        match page.next_token {
            Some(next) => token = Some(next),
            None => return Ok(events),
        }
    }
}

fn runs_from_events(events: Vec<(FunnelRunEventPayload, DateTime<Utc>)>) -> Vec<OntologyFunnelRun> {
    let mut by_run = HashMap::<Uuid, FunnelRunAccumulator>::new();
    for (payload, recorded_at) in events {
        let run_id = payload.run_id;
        let source_id = payload.source_id;
        by_run
            .entry(run_id)
            .or_insert_with(|| FunnelRunAccumulator::new(run_id, source_id))
            .apply(payload, recorded_at);
    }

    let mut runs = by_run
        .into_values()
        .filter_map(FunnelRunAccumulator::into_run)
        .collect::<Vec<_>>();
    runs.sort_by(|left, right| {
        right
            .started_at
            .cmp(&left.started_at)
            .then_with(|| right.id.cmp(&left.id))
    });
    runs
}

pub async fn dataset_exists(db: &PgPool, dataset_id: Uuid) -> Result<bool, FunnelRepoError> {
    Ok(
        sqlx::query_scalar::<_, bool>("SELECT EXISTS (SELECT 1 FROM datasets WHERE id = $1)")
            .bind(dataset_id)
            .fetch_one(db)
            .await?,
    )
}

pub async fn pipeline_exists(db: &PgPool, pipeline_id: Uuid) -> Result<bool, FunnelRepoError> {
    Ok(
        sqlx::query_scalar::<_, bool>("SELECT EXISTS (SELECT 1 FROM pipelines WHERE id = $1)")
            .bind(pipeline_id)
            .fetch_one(db)
            .await?,
    )
}

pub async fn load_source(
    db: &PgPool,
    id: Uuid,
) -> Result<Option<OntologyFunnelSource>, FunnelRepoError> {
    let row = sqlx::query_as::<_, OntologyFunnelSourceRow>(
        r#"SELECT id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
                  dataset_version, preview_limit, default_marking, status, property_mappings,
                  trigger_context, owner_id, last_run_at, created_at, updated_at
           FROM ontology_funnel_sources
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(db)
    .await?;

    row.map(decode_source).transpose()
}

pub async fn load_health_metrics(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    source_id: Uuid,
) -> Result<OntologyFunnelHealthMetricsRow, FunnelRepoError> {
    let runs = runs_from_events(list_funnel_events_for_source(actions, tenant, source_id).await?);
    let now = Utc::now();
    let mut durations = runs
        .iter()
        .map(|run| {
            (run.finished_at.unwrap_or(now) - run.started_at)
                .num_milliseconds()
                .max(0) as f64
        })
        .collect::<Vec<_>>();
    durations.sort_by(f64::total_cmp);

    let p95_duration_ms = percentile_cont(&durations, 0.95);
    let avg_duration_ms = if durations.is_empty() {
        None
    } else {
        Some(durations.iter().sum::<f64>() / durations.len() as f64)
    };
    let max_duration_ms = durations.last().map(|duration| *duration as i64);

    let total_runs = runs.len() as i64;
    let successful_runs = runs
        .iter()
        .filter(|run| matches!(run.status.as_str(), "completed" | "dry_run"))
        .count() as i64;
    let failed_runs = runs.iter().filter(|run| run.status == "failed").count() as i64;
    let warning_runs = runs
        .iter()
        .filter(|run| {
            matches!(
                run.status.as_str(),
                "completed_with_errors" | "dry_run_with_errors"
            )
        })
        .count() as i64;

    Ok(OntologyFunnelHealthMetricsRow {
        total_runs,
        successful_runs,
        failed_runs,
        warning_runs,
        avg_duration_ms,
        p95_duration_ms,
        max_duration_ms,
        latest_run_status: runs.first().map(|run| run.status.clone()),
        last_run_at: runs
            .iter()
            .filter_map(|run| run.finished_at.or(Some(run.started_at)))
            .max(),
        last_success_at: runs
            .iter()
            .filter(|run| matches!(run.status.as_str(), "completed" | "dry_run"))
            .filter_map(|run| run.finished_at)
            .max(),
        last_failure_at: runs
            .iter()
            .filter(|run| run.status == "failed")
            .filter_map(|run| run.finished_at)
            .max(),
        last_warning_at: runs
            .iter()
            .filter(|run| {
                matches!(
                    run.status.as_str(),
                    "completed_with_errors" | "dry_run_with_errors"
                )
            })
            .filter_map(|run| run.finished_at)
            .max(),
        rows_read: runs.iter().map(|run| i64::from(run.rows_read)).sum(),
        inserted_count: runs.iter().map(|run| i64::from(run.inserted_count)).sum(),
        updated_count: runs.iter().map(|run| i64::from(run.updated_count)).sum(),
        skipped_count: runs.iter().map(|run| i64::from(run.skipped_count)).sum(),
        error_count: runs.iter().map(|run| i64::from(run.error_count)).sum(),
    })
}

pub async fn list_sources(
    db: &PgPool,
    params: ListSourcesParams<'_>,
) -> Result<Vec<OntologyFunnelSource>, FunnelRepoError> {
    let rows = sqlx::query_as::<_, OntologyFunnelSourceRow>(
        r#"SELECT id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
                  dataset_version, preview_limit, default_marking, status, property_mappings,
                  trigger_context, owner_id, last_run_at, created_at, updated_at
           FROM ontology_funnel_sources
           WHERE ($1::uuid IS NULL OR object_type_id = $1)
             AND ($2 = '' OR status = $2)
             AND ($3::boolean OR owner_id = $4)
           ORDER BY created_at DESC
           OFFSET $5 LIMIT $6"#,
    )
    .bind(params.object_type_id)
    .bind(params.status_filter)
    .bind(params.is_admin)
    .bind(params.actor_id)
    .bind(params.offset)
    .bind(params.limit)
    .fetch_all(db)
    .await?;

    rows.into_iter().map(decode_source).collect()
}

pub async fn count_sources(
    db: &PgPool,
    object_type_id: Option<Uuid>,
    status_filter: &str,
    is_admin: bool,
    actor_id: Uuid,
) -> Result<i64, FunnelRepoError> {
    Ok(sqlx::query_scalar::<_, i64>(
        r#"SELECT COUNT(*)
           FROM ontology_funnel_sources
           WHERE ($1::uuid IS NULL OR object_type_id = $1)
             AND ($2 = '' OR status = $2)
             AND ($3::boolean OR owner_id = $4)"#,
    )
    .bind(object_type_id)
    .bind(status_filter)
    .bind(is_admin)
    .bind(actor_id)
    .fetch_one(db)
    .await?)
}

pub async fn list_sources_for_health(
    db: &PgPool,
    params: HealthSourcesParams,
) -> Result<Vec<OntologyFunnelSource>, FunnelRepoError> {
    let rows = sqlx::query_as::<_, OntologyFunnelSourceRow>(
        r#"SELECT id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
                  dataset_version, preview_limit, default_marking, status, property_mappings,
                  trigger_context, owner_id, last_run_at, created_at, updated_at
           FROM ontology_funnel_sources
           WHERE ($1::uuid IS NULL OR object_type_id = $1)
             AND ($2::boolean OR owner_id = $3)
           ORDER BY created_at DESC"#,
    )
    .bind(params.object_type_id)
    .bind(params.is_admin)
    .bind(params.actor_id)
    .fetch_all(db)
    .await?;

    rows.into_iter().map(decode_source).collect()
}

pub async fn create_source(
    db: &PgPool,
    input: CreateSourceInput,
) -> Result<OntologyFunnelSource, FunnelRepoError> {
    let row = sqlx::query_as::<_, OntologyFunnelSourceRow>(
        r#"INSERT INTO ontology_funnel_sources (
               id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
               dataset_version, preview_limit, default_marking, status, property_mappings,
               trigger_context, owner_id
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14)
           RETURNING id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
                     dataset_version, preview_limit, default_marking, status, property_mappings,
                     trigger_context, owner_id, last_run_at, created_at, updated_at"#,
    )
    .bind(input.id)
    .bind(input.name)
    .bind(input.description)
    .bind(input.object_type_id)
    .bind(input.dataset_id)
    .bind(input.pipeline_id)
    .bind(input.dataset_branch)
    .bind(input.dataset_version)
    .bind(input.preview_limit)
    .bind(input.default_marking)
    .bind(input.status)
    .bind(input.property_mappings)
    .bind(input.trigger_context)
    .bind(input.owner_id)
    .fetch_one(db)
    .await?;

    decode_source(row)
}

pub async fn update_source(
    db: &PgPool,
    input: UpdateSourceInput,
) -> Result<Option<OntologyFunnelSource>, FunnelRepoError> {
    let row = sqlx::query_as::<_, OntologyFunnelSourceRow>(
        r#"UPDATE ontology_funnel_sources
           SET name = COALESCE($2, name),
               description = COALESCE($3, description),
               pipeline_id = $4,
               dataset_branch = $5,
               dataset_version = $6,
               preview_limit = $7,
               default_marking = $8,
               status = $9,
               property_mappings = $10::jsonb,
               trigger_context = $11::jsonb,
               updated_at = NOW()
           WHERE id = $1
           RETURNING id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
                     dataset_version, preview_limit, default_marking, status, property_mappings,
                     trigger_context, owner_id, last_run_at, created_at, updated_at"#,
    )
    .bind(input.id)
    .bind(input.name)
    .bind(input.description)
    .bind(input.pipeline_id)
    .bind(input.dataset_branch)
    .bind(input.dataset_version)
    .bind(input.preview_limit)
    .bind(input.default_marking)
    .bind(input.status)
    .bind(input.property_mappings)
    .bind(input.trigger_context)
    .fetch_optional(db)
    .await?;

    row.map(decode_source).transpose()
}

pub async fn delete_source(db: &PgPool, id: Uuid) -> Result<bool, FunnelRepoError> {
    let result = sqlx::query("DELETE FROM ontology_funnel_sources WHERE id = $1")
        .bind(id)
        .execute(db)
        .await?;
    Ok(result.rows_affected() > 0)
}

pub async fn create_run(
    actions: &dyn ActionLogStore,
    tenant: TenantId,
    input: CreateRunInput,
) -> Result<(), FunnelRepoError> {
    let started_at = Utc::now();
    let subject = input.started_by.to_string();
    let source_id = input.source_id;
    let payload = append_payload(&input, started_at)?;
    append_funnel_event(actions, tenant, source_id, subject, payload, started_at).await?;
    Ok(())
}

pub async fn complete_run(
    actions: &dyn ActionLogStore,
    tenant: TenantId,
    subject: Uuid,
    input: CompleteRunInput,
) -> Result<(), FunnelRepoError> {
    let event = if input.status == "failed" {
        FUNNEL_RUN_FAILED_EVENT
    } else {
        FUNNEL_RUN_COMPLETED_EVENT
    };
    let source_id = input.source_id;
    let finished_at = input.finished_at;
    let payload = terminal_payload(event, &input)?;
    append_funnel_event(
        actions,
        tenant,
        source_id,
        subject.to_string(),
        payload,
        finished_at,
    )
    .await?;
    Ok(())
}

pub async fn mark_source_ran(
    db: &PgPool,
    source_id: Uuid,
    finished_at: DateTime<Utc>,
) -> Result<(), FunnelRepoError> {
    sqlx::query(
        r#"UPDATE ontology_funnel_sources
           SET last_run_at = $2, updated_at = NOW()
           WHERE id = $1"#,
    )
    .bind(source_id)
    .bind(finished_at)
    .execute(db)
    .await?;
    Ok(())
}

pub async fn fail_run(
    actions: &dyn ActionLogStore,
    tenant: TenantId,
    source_id: Uuid,
    run_id: Uuid,
    subject: Uuid,
    error: &str,
) -> Result<(), FunnelRepoError> {
    let finished_at = Utc::now();
    let input = CompleteRunInput {
        id: run_id,
        source_id,
        pipeline_run_id: None,
        status: "failed".to_string(),
        rows_read: 0,
        inserted_count: 0,
        updated_count: 0,
        skipped_count: 0,
        error_count: 1,
        details: serde_json::json!({}),
        error_message: Some(error.to_string()),
        finished_at,
    };
    complete_run(actions, tenant, subject, input).await
}

pub async fn load_run(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    run_id: Uuid,
) -> Result<Option<OntologyFunnelRun>, FunnelRepoError> {
    Ok(
        runs_from_events(load_funnel_events_for_run(actions, tenant, run_id).await?)
            .into_iter()
            .next(),
    )
}

pub async fn count_runs_for_source(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    source_id: Uuid,
) -> Result<i64, FunnelRepoError> {
    Ok(
        runs_from_events(list_funnel_events_for_source(actions, tenant, source_id).await?).len()
            as i64,
    )
}

pub async fn list_runs_for_source(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    source_id: Uuid,
    offset: i64,
    limit: i64,
) -> Result<Vec<OntologyFunnelRun>, FunnelRepoError> {
    let runs = runs_from_events(list_funnel_events_for_source(actions, tenant, source_id).await?);
    Ok(runs
        .into_iter()
        .skip(offset.max(0) as usize)
        .take(limit.max(0) as usize)
        .collect())
}

pub async fn list_runs_for_tenant(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
) -> Result<Vec<OntologyFunnelRun>, FunnelRepoError> {
    Ok(runs_from_events(
        list_funnel_events_for_tenant(actions, tenant).await?,
    ))
}

pub async fn load_run_for_source(
    actions: &dyn ActionLogStore,
    tenant: &TenantId,
    source_id: Uuid,
    run_id: Uuid,
) -> Result<Option<OntologyFunnelRun>, FunnelRepoError> {
    Ok(runs_from_events(
        list_funnel_events_for_source(actions, tenant, source_id)
            .await?
            .into_iter()
            .filter(|(payload, _)| payload.run_id == run_id)
            .collect(),
    )
    .into_iter()
    .next())
}
