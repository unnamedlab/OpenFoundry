//! Foundry-parity Streaming Profiles handlers.
//!
//! Surfaces the operator-facing CRUD, project import/remove flow, and
//! pipeline attachment endpoints. The effective-config endpoint
//! composes the union of attached profiles using the deterministic
//! resolver in [`crate::models::profile::compose_effective_config`].
//!
//! Authorization rules (mirrors the Foundry docs):
//!   * Profile CRUD requires admin / `streaming_admin` / explicit
//!     `streaming:profile-write` permission.
//!   * Project-import requires `compass:import-resource-to` on the
//!     target project. Restricted profiles additionally require the
//!     caller to hold `enrollment_resource_administrator`.
//!   * Pipeline attach requires `streaming:write` and the profile to
//!     have an active project ref in the pipeline's project (412
//!     `STREAMING_PROFILE_NOT_IMPORTED` otherwise).

use auth_middleware::claims::Claims;
use axum::{
    Extension, Json,
    extract::{Path, Query},
};
use chrono::Utc;
use serde::Deserialize;
use sqlx::{PgPool, Postgres, Transaction, types::Json as SqlJson};
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{
        ServiceResult, bad_request, db_error, forbidden, internal_error, not_found,
        precondition_failed, unprocessable,
    },
    models::{
        ListResponse,
        profile::{
            AttachProfileRequest, CreateProfileRequest, EffectiveFlinkConfig, PatchProfileRequest,
            PipelineProfileAttachment, ProfileCategory, ProfileSizeClass, StreamingProfile,
            StreamingProfileProjectRef, StreamingProfileRow, compose_effective_config,
            validate_config_keys,
        },
    },
    outbox as streaming_outbox,
};

// ---- Stable error codes ----------------------------------------------------

pub const ERR_PROFILE_RESTRICTED_REQUIRES_ADMIN: &str =
    "STREAMING_PROFILE_RESTRICTED_REQUIRES_ENROLLMENT_ADMIN";
pub const ERR_PROFILE_NOT_IMPORTED: &str = "STREAMING_PROFILE_NOT_IMPORTED";
pub const ERR_PROFILE_INVALID_KEY: &str = "STREAMING_PROFILE_INVALID_FLINK_KEY";

// ---- Permission helpers ----------------------------------------------------

const PERM_PROFILE_WRITE: &str = "streaming:profile-write";
const PERM_IMPORT_RESOURCE_TO: &str = "compass:import-resource-to";
const PERM_PIPELINE_WRITE: &str = "streaming:write";

const ROLE_ENROLLMENT_ADMIN: &str = "enrollment_resource_administrator";
const ROLE_STREAMING_ADMIN: &str = "streaming_admin";
const ROLE_ADMIN: &str = "admin";

fn can_admin_profiles(claims: &Claims) -> bool {
    claims.has_any_role(&[ROLE_ADMIN, ROLE_STREAMING_ADMIN])
        || claims.has_permission_key(PERM_PROFILE_WRITE)
}

fn can_import_to_project(claims: &Claims) -> bool {
    claims.has_any_role(&[ROLE_ADMIN, ROLE_STREAMING_ADMIN, ROLE_ENROLLMENT_ADMIN])
        || claims.has_permission_key(PERM_IMPORT_RESOURCE_TO)
}

fn can_import_restricted(claims: &Claims) -> bool {
    claims.has_any_role(&[ROLE_ADMIN, ROLE_ENROLLMENT_ADMIN])
}

fn can_attach_to_pipeline(claims: &Claims) -> bool {
    claims.has_any_role(&[ROLE_ADMIN, ROLE_STREAMING_ADMIN, "data_engineer"])
        || claims.has_permission_key(PERM_PIPELINE_WRITE)
}

fn emit_audit(actor: &Claims, event: &str, profile_id: Uuid, extra: serde_json::Value) {
    tracing::info!(
        target: "audit",
        event = event,
        actor.sub = %actor.sub,
        actor.email = %actor.email,
        resource.type = "streaming_profile",
        resource.id = %profile_id,
        extra = %extra,
        "streaming audit event"
    );
}

// ---- Loaders ---------------------------------------------------------------

const PROFILE_COLUMNS: &str = "id, name, description, category, size_class, restricted,
    config_json, version, created_by, created_at, updated_at";

async fn load_profile_row(db: &PgPool, id: Uuid) -> Result<StreamingProfileRow, sqlx::Error> {
    sqlx::query_as::<_, StreamingProfileRow>(
        "SELECT id, name, description, category, size_class, restricted, config_json,
                version, created_by, created_at, updated_at
           FROM streaming_profiles
          WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
    .await
}

async fn load_profile_row_tx(
    tx: &mut Transaction<'_, Postgres>,
    id: Uuid,
) -> Result<StreamingProfileRow, sqlx::Error> {
    sqlx::query_as::<_, StreamingProfileRow>(
        "SELECT id, name, description, category, size_class, restricted, config_json,
                version, created_by, created_at, updated_at
           FROM streaming_profiles
          WHERE id = $1",
    )
    .bind(id)
    .fetch_one(&mut **tx)
    .await
}

// ---- List + filters --------------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct ListProfilesQuery {
    pub category: Option<String>,
    pub size_class: Option<String>,
}

pub async fn list_profiles(
    axum::extract::State(state): axum::extract::State<AppState>,
    Query(params): Query<ListProfilesQuery>,
) -> ServiceResult<ListResponse<StreamingProfile>> {
    let mut sql = format!("SELECT {PROFILE_COLUMNS} FROM streaming_profiles WHERE 1=1");
    let mut binds: Vec<String> = Vec::new();
    if let Some(cat) = params.category.as_deref() {
        ProfileCategory::from_str(cat).map_err(bad_request)?;
        sql.push_str(&format!(" AND category = ${}", binds.len() + 1));
        binds.push(cat.to_string());
    }
    if let Some(size) = params.size_class.as_deref() {
        ProfileSizeClass::from_str(size).map_err(bad_request)?;
        sql.push_str(&format!(" AND size_class = ${}", binds.len() + 1));
        binds.push(size.to_string());
    }
    sql.push_str(" ORDER BY created_at ASC");

    let mut q = sqlx::query_as::<_, StreamingProfileRow>(&sql);
    for value in &binds {
        q = q.bind(value);
    }
    let rows = q.fetch_all(&state.db).await.map_err(|c| db_error(&c))?;
    Ok(Json(ListResponse {
        data: rows.into_iter().map(StreamingProfile::from).collect(),
    }))
}

// ---- Create ---------------------------------------------------------------

pub async fn create_profile(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Json(payload): Json<CreateProfileRequest>,
) -> ServiceResult<StreamingProfile> {
    if !can_admin_profiles(&claims) {
        return Err(forbidden(
            "caller cannot create streaming profiles (admin / streaming_admin only)",
        ));
    }
    if payload.name.trim().is_empty() {
        return Err(bad_request("profile name is required"));
    }
    if let Err(errors) = validate_config_keys(&payload.config_json) {
        return Err(unprocessable(ERR_PROFILE_INVALID_KEY, errors.join("; ")));
    }
    // LARGE size class defaults to restricted unless caller explicitly
    // overrides it. Foundry docs warn that LARGE profiles "must be
    // imported via Enrollment Settings" — that boils down to
    // `restricted = true` here.
    let restricted = payload
        .restricted
        .unwrap_or_else(|| matches!(payload.size_class, ProfileSizeClass::Large));

    let id = Uuid::now_v7();
    let mut tx = state.db.begin().await.map_err(|c| db_error(&c))?;
    sqlx::query(
        "INSERT INTO streaming_profiles (
             id, name, description, category, size_class, restricted, config_json,
             version, created_by
         ) VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8)",
    )
    .bind(id)
    .bind(payload.name.trim())
    .bind(payload.description.unwrap_or_default())
    .bind(payload.category.as_str())
    .bind(payload.size_class.as_str())
    .bind(restricted)
    .bind(SqlJson(&payload.config_json))
    .bind(claims.sub.to_string())
    .execute(&mut *tx)
    .await
    .map_err(|c| db_error(&c))?;

    let profile: StreamingProfile = load_profile_row_tx(&mut tx, id)
        .await
        .map_err(|c| db_error(&c))?
        .into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::profile_created(&profile))
        .await
        .map_err(|cause| {
            tracing::error!(profile_id = %id, error = %cause, "outbox emit failed");
            internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|c| db_error(&c))?;

    emit_audit(
        &claims,
        "streaming.profile.created",
        profile.id,
        serde_json::json!({
            "name": profile.name,
            "category": profile.category.as_str(),
            "size_class": profile.size_class.as_str(),
            "restricted": profile.restricted,
        }),
    );
    Ok(Json(profile))
}

// ---- Patch -----------------------------------------------------------------

pub async fn patch_profile(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
    Json(payload): Json<PatchProfileRequest>,
) -> ServiceResult<StreamingProfile> {
    if !can_admin_profiles(&claims) {
        return Err(forbidden("caller cannot modify streaming profiles"));
    }
    let existing = match load_profile_row(&state.db, id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("profile not found")),
        Err(c) => return Err(db_error(&c)),
    };

    if let Some(cfg) = payload.config_json.as_ref() {
        if let Err(errors) = validate_config_keys(cfg) {
            return Err(unprocessable(ERR_PROFILE_INVALID_KEY, errors.join("; ")));
        }
    }

    let new_name = payload.name.unwrap_or(existing.name);
    let new_description = payload.description.unwrap_or(existing.description);
    let new_category = payload
        .category
        .map(|c| c.as_str().to_string())
        .unwrap_or(existing.category);
    let new_size = payload
        .size_class
        .map(|s| s.as_str().to_string())
        .unwrap_or(existing.size_class);
    let new_restricted = payload.restricted.unwrap_or(existing.restricted);
    let new_config = payload
        .config_json
        .map(SqlJson)
        .unwrap_or(existing.config_json);

    let mut tx = state.db.begin().await.map_err(|c| db_error(&c))?;
    sqlx::query(
        "UPDATE streaming_profiles
            SET name        = $2,
                description = $3,
                category    = $4,
                size_class  = $5,
                restricted  = $6,
                config_json = $7,
                version     = version + 1,
                updated_at  = now()
          WHERE id = $1",
    )
    .bind(id)
    .bind(new_name)
    .bind(new_description)
    .bind(&new_category)
    .bind(&new_size)
    .bind(new_restricted)
    .bind(new_config)
    .execute(&mut *tx)
    .await
    .map_err(|c| db_error(&c))?;

    let profile: StreamingProfile = load_profile_row_tx(&mut tx, id)
        .await
        .map_err(|c| db_error(&c))?
        .into();
    streaming_outbox::emit(&mut tx, &streaming_outbox::profile_updated(&profile))
        .await
        .map_err(|cause| {
            tracing::error!(profile_id = %id, error = %cause, "outbox emit failed");
            internal_error("failed to enqueue outbox event")
        })?;
    tx.commit().await.map_err(|c| db_error(&c))?;

    emit_audit(
        &claims,
        "streaming.profile.updated",
        profile.id,
        serde_json::json!({
            "version": profile.version,
            "restricted": profile.restricted,
        }),
    );
    Ok(Json(profile))
}

// ---- Project refs ---------------------------------------------------------

pub async fn list_project_refs(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(profile_id): Path<Uuid>,
) -> ServiceResult<ListResponse<StreamingProfileProjectRef>> {
    // Confirm the profile exists so callers get 404 instead of an
    // empty list when they typo the id.
    if let Err(sqlx::Error::RowNotFound) = load_profile_row(&state.db, profile_id).await {
        return Err(not_found("profile not found"));
    }
    let rows = sqlx::query_as::<_, StreamingProfileProjectRef>(
        "SELECT project_rid, profile_id, imported_by, imported_at, imported_order
           FROM streaming_profile_project_refs
          WHERE profile_id = $1
          ORDER BY imported_at ASC",
    )
    .bind(profile_id)
    .fetch_all(&state.db)
    .await
    .map_err(|c| db_error(&c))?;
    Ok(Json(ListResponse { data: rows }))
}

pub async fn import_profile_to_project(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path((project_rid, profile_id)): Path<(String, Uuid)>,
) -> ServiceResult<StreamingProfileProjectRef> {
    if !can_import_to_project(&claims) {
        return Err(forbidden(
            "caller lacks 'compass:import-resource-to' permission",
        ));
    }
    let profile_row = match load_profile_row(&state.db, profile_id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("profile not found")),
        Err(c) => return Err(db_error(&c)),
    };
    let profile: StreamingProfile = profile_row.into();

    if profile.restricted && !can_import_restricted(&claims) {
        return Err(forbidden(format!(
            "{ERR_PROFILE_RESTRICTED_REQUIRES_ADMIN}: profile '{}' is restricted; only Enrollment Resource Administrators may import it",
            profile.name
        )));
    }

    let mut tx = state.db.begin().await.map_err(|c| db_error(&c))?;
    sqlx::query(
        "INSERT INTO streaming_profile_project_refs (project_rid, profile_id, imported_by)
         VALUES ($1, $2, $3)
         ON CONFLICT (project_rid, profile_id) DO NOTHING",
    )
    .bind(&project_rid)
    .bind(profile_id)
    .bind(claims.sub.to_string())
    .execute(&mut *tx)
    .await
    .map_err(|c| db_error(&c))?;

    let row: StreamingProfileProjectRef = sqlx::query_as::<_, StreamingProfileProjectRef>(
        "SELECT project_rid, profile_id, imported_by, imported_at, imported_order
           FROM streaming_profile_project_refs
          WHERE project_rid = $1 AND profile_id = $2",
    )
    .bind(&project_rid)
    .bind(profile_id)
    .fetch_one(&mut *tx)
    .await
    .map_err(|c| db_error(&c))?;

    streaming_outbox::emit(
        &mut tx,
        &streaming_outbox::profile_imported(
            &profile,
            &project_rid,
            &claims.sub.to_string(),
            row.imported_at,
        ),
    )
    .await
    .map_err(|cause| {
        tracing::error!(profile_id = %profile_id, error = %cause, "outbox emit failed");
        internal_error("failed to enqueue outbox event")
    })?;
    tx.commit().await.map_err(|c| db_error(&c))?;

    emit_audit(
        &claims,
        "streaming.profile.imported",
        profile.id,
        serde_json::json!({
            "project_rid": project_rid,
            "name": profile.name,
            "restricted": profile.restricted,
        }),
    );
    Ok(Json(row))
}

pub async fn remove_profile_from_project(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path((project_rid, profile_id)): Path<(String, Uuid)>,
) -> ServiceResult<serde_json::Value> {
    if !can_import_to_project(&claims) {
        return Err(forbidden(
            "caller lacks 'compass:import-resource-to' permission",
        ));
    }
    let profile_row = match load_profile_row(&state.db, profile_id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("profile not found")),
        Err(c) => return Err(db_error(&c)),
    };
    if profile_row.restricted && !can_import_restricted(&claims) {
        return Err(forbidden(format!(
            "{ERR_PROFILE_RESTRICTED_REQUIRES_ADMIN}: profile is restricted; only Enrollment Resource Administrators may remove its references"
        )));
    }

    let mut tx = state.db.begin().await.map_err(|c| db_error(&c))?;
    let deleted = sqlx::query(
        "DELETE FROM streaming_profile_project_refs
          WHERE project_rid = $1 AND profile_id = $2",
    )
    .bind(&project_rid)
    .bind(profile_id)
    .execute(&mut *tx)
    .await
    .map_err(|c| db_error(&c))?
    .rows_affected();

    if deleted == 0 {
        tx.rollback().await.ok();
        return Err(not_found("profile reference not found in project"));
    }

    streaming_outbox::emit(
        &mut tx,
        &streaming_outbox::profile_removed_from_project(
            profile_id,
            &project_rid,
            &claims.sub.to_string(),
            Utc::now(),
        ),
    )
    .await
    .map_err(|cause| {
        tracing::error!(profile_id = %profile_id, error = %cause, "outbox emit failed");
        internal_error("failed to enqueue outbox event")
    })?;
    tx.commit().await.map_err(|c| db_error(&c))?;

    emit_audit(
        &claims,
        "streaming.profile.removed_from_project",
        profile_id,
        serde_json::json!({ "project_rid": project_rid }),
    );

    Ok(Json(serde_json::json!({
        "removed": true,
        "warning": "any pipeline in this project that relies on this profile will fail until it is re-imported"
    })))
}

// ---- Pipeline attachment --------------------------------------------------

pub async fn attach_profile_to_pipeline(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(pipeline_rid): Path<String>,
    Json(payload): Json<AttachProfileRequest>,
) -> ServiceResult<PipelineProfileAttachment> {
    if !can_attach_to_pipeline(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }

    // Confirm the profile exists and the project ref is present.
    let profile_row = match load_profile_row(&state.db, payload.profile_id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("profile not found")),
        Err(c) => return Err(db_error(&c)),
    };
    let profile: StreamingProfile = profile_row.into();

    let imported: Option<(String,)> = sqlx::query_as(
        "SELECT project_rid FROM streaming_profile_project_refs
          WHERE project_rid = $1 AND profile_id = $2",
    )
    .bind(&payload.project_rid)
    .bind(payload.profile_id)
    .fetch_optional(&state.db)
    .await
    .map_err(|c| db_error(&c))?;
    if imported.is_none() {
        return Err(precondition_failed(
            ERR_PROFILE_NOT_IMPORTED,
            format!(
                "profile '{}' must be imported into project '{}' before it can be attached to a pipeline",
                profile.name, payload.project_rid
            ),
        ));
    }

    sqlx::query(
        "INSERT INTO streaming_pipeline_profiles (pipeline_rid, profile_id, attached_by)
         VALUES ($1, $2, $3)
         ON CONFLICT (pipeline_rid, profile_id) DO NOTHING",
    )
    .bind(&pipeline_rid)
    .bind(payload.profile_id)
    .bind(claims.sub.to_string())
    .execute(&state.db)
    .await
    .map_err(|c| db_error(&c))?;

    let row: PipelineProfileAttachment = sqlx::query_as::<_, PipelineProfileAttachment>(
        "SELECT pipeline_rid, profile_id, attached_by, attached_at, attached_order
           FROM streaming_pipeline_profiles
          WHERE pipeline_rid = $1 AND profile_id = $2",
    )
    .bind(&pipeline_rid)
    .bind(payload.profile_id)
    .fetch_one(&state.db)
    .await
    .map_err(|c| db_error(&c))?;

    emit_audit(
        &claims,
        "streaming.profile.attached_to_pipeline",
        profile.id,
        serde_json::json!({
            "pipeline_rid": pipeline_rid,
            "project_rid": payload.project_rid,
        }),
    );
    Ok(Json(row))
}

pub async fn detach_profile_from_pipeline(
    axum::extract::State(state): axum::extract::State<AppState>,
    Extension(claims): Extension<Claims>,
    Path((pipeline_rid, profile_id)): Path<(String, Uuid)>,
) -> ServiceResult<serde_json::Value> {
    if !can_attach_to_pipeline(&claims) {
        return Err(forbidden("caller lacks 'streaming:write' permission"));
    }
    let deleted = sqlx::query(
        "DELETE FROM streaming_pipeline_profiles
          WHERE pipeline_rid = $1 AND profile_id = $2",
    )
    .bind(&pipeline_rid)
    .bind(profile_id)
    .execute(&state.db)
    .await
    .map_err(|c| db_error(&c))?
    .rows_affected();
    if deleted == 0 {
        return Err(not_found("profile is not attached to pipeline"));
    }
    emit_audit(
        &claims,
        "streaming.profile.detached_from_pipeline",
        profile_id,
        serde_json::json!({ "pipeline_rid": pipeline_rid }),
    );
    Ok(Json(serde_json::json!({ "detached": true })))
}

pub async fn list_pipeline_profiles(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(pipeline_rid): Path<String>,
) -> ServiceResult<ListResponse<StreamingProfile>> {
    let profiles = load_pipeline_profiles(&state.db, &pipeline_rid)
        .await
        .map_err(|c| db_error(&c))?
        .into_iter()
        .map(|(p, _order)| p)
        .collect::<Vec<_>>();
    Ok(Json(ListResponse { data: profiles }))
}

pub async fn get_effective_flink_config(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(pipeline_rid): Path<String>,
) -> ServiceResult<EffectiveFlinkConfig> {
    let profiles = load_pipeline_profiles(&state.db, &pipeline_rid)
        .await
        .map_err(|c| db_error(&c))?;
    let effective = compose_effective_config(&pipeline_rid, &profiles);
    for warning in &effective.warnings {
        tracing::warn!(pipeline_rid = %pipeline_rid, "{warning}");
    }
    Ok(Json(effective))
}

async fn load_pipeline_profiles(
    db: &PgPool,
    pipeline_rid: &str,
) -> Result<Vec<(StreamingProfile, i64)>, sqlx::Error> {
    #[derive(sqlx::FromRow)]
    struct Joined {
        pub id: Uuid,
        pub name: String,
        pub description: String,
        pub category: String,
        pub size_class: String,
        pub restricted: bool,
        pub config_json: SqlJson<serde_json::Value>,
        pub version: i32,
        pub created_by: String,
        pub created_at: chrono::DateTime<chrono::Utc>,
        pub updated_at: chrono::DateTime<chrono::Utc>,
        pub attached_order: i64,
    }

    let rows: Vec<Joined> = sqlx::query_as::<_, Joined>(
        "SELECT p.id, p.name, p.description, p.category, p.size_class, p.restricted,
                p.config_json, p.version, p.created_by, p.created_at, p.updated_at,
                a.attached_order
           FROM streaming_pipeline_profiles a
           JOIN streaming_profiles p ON p.id = a.profile_id
          WHERE a.pipeline_rid = $1
          ORDER BY a.attached_order ASC",
    )
    .bind(pipeline_rid)
    .fetch_all(db)
    .await?;

    Ok(rows
        .into_iter()
        .map(|r| {
            let row = StreamingProfileRow {
                id: r.id,
                name: r.name,
                description: r.description,
                category: r.category,
                size_class: r.size_class,
                restricted: r.restricted,
                config_json: r.config_json,
                version: r.version,
                created_by: r.created_by,
                created_at: r.created_at,
                updated_at: r.updated_at,
            };
            (StreamingProfile::from(row), r.attached_order)
        })
        .collect())
}
