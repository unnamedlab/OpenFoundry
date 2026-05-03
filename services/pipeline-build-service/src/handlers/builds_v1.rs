//! `/v1/builds` surface — the Foundry-aligned Builds application API.
//!
//! Distinct from `handlers::builds`, which serves the legacy
//! `pipeline_runs`-backed Builds queue. The two coexist while the
//! migration to the formal lifecycle (D1.1.5 P2) is in flight; once
//! all callers move over, the old surface can be retired.

use std::sync::Arc;

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;

use crate::AppState;
use crate::domain::build_resolution::{
    DatasetVersioningClient, JobSpecRepo, ResolveBuildArgs, resolve_build,
};
use crate::domain::job_lifecycle::{transition_job_in_tx};
use crate::domain::metrics;
use crate::models::build::{
    Build, BuildEnvelope, BuildState, CreateBuildRequest, ListBuildsQuery,
};
use crate::models::job::{Job, JobState};
use core_models::dataset::transaction::BranchName;

/// Snapshot of the lifecycle plug-points the `/v1/builds` handlers need
/// at runtime. Stored on [`AppState`] so tests can swap the inner
/// implementations against `wiremock`-backed fakes.
#[derive(Clone)]
pub struct BuildLifecyclePorts {
    pub versioning: Arc<dyn DatasetVersioningClient>,
    pub job_specs: Arc<dyn JobSpecRepo>,
    /// P4 — composite log sink. The runners emit through this; the
    /// SSE / WebSocket endpoints subscribe via `broadcaster`.
    pub log_sink: Arc<dyn crate::domain::logs::LogSink>,
    /// Broadcast handle for the live-log fan-out side. Dedicated
    /// field (not just the `LogSink` trait) because subscribers need
    /// `subscribe(&job_rid)`, which is not part of the trait.
    pub broadcaster: Arc<crate::domain::logs::BroadcastLogSink>,
}

fn audit(action: &str, actor: &str, build_rid: &str, details: serde_json::Value) {
    tracing::info!(
        target: "audit",
        actor = actor,
        action = action,
        build_rid = build_rid,
        details = %details,
        "pipeline-build-service lifecycle event"
    );
}

// ---------------------------------------------------------------------------
// POST /v1/builds
// ---------------------------------------------------------------------------

pub async fn create_build(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreateBuildRequest>,
) -> impl IntoResponse {
    let ports = match state.lifecycle_ports.clone() {
        Some(p) => p,
        None => {
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                "Build lifecycle ports not configured",
            )
                .into_response();
        }
    };

    let build_branch: BranchName = match body.build_branch.parse() {
        Ok(b) => b,
        Err(error) => {
            return (
                StatusCode::BAD_REQUEST,
                format!("invalid build_branch: {error}"),
            )
                .into_response();
        }
    };

    if body.output_dataset_rids.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            "output_dataset_rids must declare at least one dataset",
        )
            .into_response();
    }
    let outputs = body.output_dataset_rids.clone();

    let trigger_kind = body
        .trigger_kind
        .as_deref()
        .unwrap_or("MANUAL")
        .to_uppercase();
    let abort_policy = body.abort_policy.unwrap_or_default();

    let result = resolve_build(
        &state.db,
        ResolveBuildArgs {
            pipeline_rid: &body.pipeline_rid,
            build_branch: &build_branch,
            job_spec_fallback: &body.job_spec_fallback,
            output_dataset_rids: &outputs,
            force_build: body.force_build,
            requested_by: &claims.sub.to_string(),
            trigger_kind: &trigger_kind,
            abort_policy: abort_policy.as_str(),
        },
        &*ports.job_specs,
        &*ports.versioning,
    )
    .await;

    match result {
        Ok(resolved) => {
            (
                StatusCode::ACCEPTED,
                Json(json!({
                    "build_id": resolved.build_id,
                    "state": resolved.state.as_str(),
                    "queued_reason": resolved.queued_reason,
                    "job_count": resolved.job_specs.len(),
                    "output_transactions": resolved.opened_transactions,
                })),
            )
                .into_response()
        }
        Err(error) => {
            tracing::warn!(error = %error, "build resolution failed");
            (StatusCode::BAD_REQUEST, error.to_string()).into_response()
        }
    }
}

// ---------------------------------------------------------------------------
// GET /v1/builds/{rid}
// ---------------------------------------------------------------------------

pub async fn get_build(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    let build = match sqlx::query_as::<_, Build>("SELECT * FROM builds WHERE rid = $1")
        .bind(&rid)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(b)) => b,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(err) => return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response(),
    };

    let jobs = sqlx::query_as::<_, Job>(
        "SELECT * FROM jobs WHERE build_id = $1 ORDER BY created_at ASC",
    )
    .bind(build.id)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    Json(BuildEnvelope { build, jobs }).into_response()
}

// ---------------------------------------------------------------------------
// POST /v1/builds/{rid}:abort
// ---------------------------------------------------------------------------

pub async fn abort_build_v1(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    let build = match sqlx::query_as::<_, Build>("SELECT * FROM builds WHERE rid = $1 FOR UPDATE")
        .bind(&rid)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(b)) => b,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(err) => return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response(),
    };

    let current = match build.build_state() {
        Ok(s) => s,
        Err(err) => return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response(),
    };
    if current.is_terminal() || current == BuildState::Aborting {
        return StatusCode::CONFLICT.into_response();
    }

    let mut tx = match state.db.begin().await {
        Ok(t) => t,
        Err(err) => return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response(),
    };

    if let Err(err) = sqlx::query(
        r#"UPDATE builds
              SET state = $1,
                  finished_at = NULL,
                  error_message = COALESCE(error_message, 'aborted by user')
            WHERE id = $2"#,
    )
    .bind(BuildState::Aborting.as_str())
    .bind(build.id)
    .execute(&mut *tx)
    .await
    {
        return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response();
    }

    let job_rows = sqlx::query_as::<_, (uuid::Uuid, String)>(
        "SELECT id, state FROM jobs WHERE build_id = $1 FOR UPDATE",
    )
    .bind(build.id)
    .fetch_all(&mut *tx)
    .await
    .unwrap_or_default();

    for (job_id, state_str) in job_rows {
        let state: JobState = match state_str.parse() {
            Ok(s) => s,
            Err(_) => continue,
        };
        let target = match state {
            JobState::Running | JobState::RunPending => Some(JobState::AbortPending),
            JobState::Waiting => Some(JobState::Aborted),
            _ => None,
        };
        if let Some(target) = target {
            let _ = transition_job_in_tx(
                &mut tx,
                job_id,
                Some(state),
                target,
                Some("build aborted by user"),
            )
            .await;
        }
    }

    if let Err(err) = tx.commit().await {
        return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response();
    }

    metrics::record_build_state(BuildState::Aborting);
    audit(
        "build.aborted",
        &claims.sub.to_string(),
        &build.rid,
        json!({"pipeline_rid": build.pipeline_rid}),
    );

    Json(json!({"rid": build.rid, "state": BuildState::Aborting.as_str()})).into_response()
}

// ---------------------------------------------------------------------------
// GET /v1/builds (with cursor paging)
// ---------------------------------------------------------------------------

pub async fn list_builds_v1(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<ListBuildsQuery>,
) -> impl IntoResponse {
    let limit = params.limit.unwrap_or(50).clamp(1, 200);
    let cursor_created_at = params
        .cursor
        .as_deref()
        .and_then(|c| chrono::DateTime::parse_from_rfc3339(c).ok().map(|d| d.with_timezone(&chrono::Utc)));

    let rows = sqlx::query_as::<_, Build>(
        r#"SELECT * FROM builds
            WHERE ($1::text IS NULL OR build_branch = $1)
              AND ($2::text IS NULL OR state = $2)
              AND ($3::text IS NULL OR pipeline_rid = $3)
              AND ($4::timestamptz IS NULL OR created_at >= $4)
              AND ($5::timestamptz IS NULL OR created_at < $5)
            ORDER BY created_at DESC
            LIMIT $6"#,
    )
    .bind(params.branch.as_deref())
    .bind(params.status.as_deref())
    .bind(params.pipeline_rid.as_deref())
    .bind(params.since)
    .bind(cursor_created_at)
    .bind(limit)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    let next_cursor = rows.last().map(|b| b.created_at.to_rfc3339());

    Json(json!({
        "data": rows,
        "next_cursor": next_cursor,
        "limit": limit,
    }))
    .into_response()
}

// ---------------------------------------------------------------------------
// POST /v1/job-specs/{kind}
// ---------------------------------------------------------------------------

#[derive(serde::Deserialize)]
pub struct CreateJobSpecRequest {
    pub pipeline_rid: String,
    pub branch_name: String,
    /// Inputs declared on the spec — typed `InputSpec` payload so the
    /// caller can include `view_filter` selectors directly.
    #[serde(default)]
    pub inputs: Vec<crate::domain::build_resolution::InputSpec>,
    /// Outputs the spec produces. Empty is only legal for EXPORT.
    #[serde(default)]
    pub output_dataset_rids: Vec<String>,
    /// Logic-specific payload. The handler validates it against the
    /// `kind` path parameter.
    #[serde(default)]
    pub logic_payload: serde_json::Value,
    /// Optional pre-computed content hash. When omitted the service
    /// derives one from the canonical (inputs, payload, kind) tuple.
    #[serde(default)]
    pub content_hash: Option<String>,
}

/// Stores a JobSpec record. The kind is the URL path segment (one of
/// `SYNC|TRANSFORM|HEALTH_CHECK|ANALYTICAL|EXPORT`). Validation
/// reuses [`crate::domain::runners::validate_logic_kind`] so the
/// resolver and the publisher agree on arity.
pub async fn create_job_spec(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(kind): Path<String>,
    Json(body): Json<CreateJobSpecRequest>,
) -> impl IntoResponse {
    let kind_upper = kind.to_uppercase();
    if !crate::domain::runners::logic_kinds::is_known(&kind_upper) {
        return (
            StatusCode::BAD_REQUEST,
            format!("unknown logic_kind in path: {kind}"),
        )
            .into_response();
    }
    if let Err(reason) =
        crate::domain::runners::validate_logic_kind(&kind_upper, body.output_dataset_rids.len())
    {
        return (StatusCode::BAD_REQUEST, reason).into_response();
    }
    if let Err(reason) = validate_kind_payload(&kind_upper, &body) {
        return (StatusCode::BAD_REQUEST, reason).into_response();
    }

    let id = uuid::Uuid::now_v7();
    let rid = format!("ri.foundry.main.job_spec.{id}");
    let content_hash = body.content_hash.clone().unwrap_or_else(|| {
        derive_content_hash(&kind_upper, &body)
    });

    let inputs_json = serde_json::to_value(&body.inputs).unwrap_or_else(|_| serde_json::json!([]));

    if let Err(err) = sqlx::query(
        r#"INSERT INTO job_specs (
              id, rid, pipeline_rid, branch_name, logic_kind,
              inputs, output_dataset_rids, logic_payload,
              content_hash, created_by
           ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)"#,
    )
    .bind(id)
    .bind(&rid)
    .bind(&body.pipeline_rid)
    .bind(&body.branch_name)
    .bind(&kind_upper)
    .bind(&inputs_json)
    .bind(&body.output_dataset_rids)
    .bind(&body.logic_payload)
    .bind(&content_hash)
    .bind(claims.sub.to_string())
    .execute(&state.db)
    .await
    {
        return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response();
    }

    audit(
        "job_spec.published",
        &claims.sub.to_string(),
        &rid,
        json!({
            "pipeline_rid": body.pipeline_rid,
            "kind": kind_upper,
            "output_count": body.output_dataset_rids.len(),
        }),
    );

    (
        StatusCode::CREATED,
        Json(json!({
            "rid": rid,
            "logic_kind": kind_upper,
            "content_hash": content_hash,
        })),
    )
        .into_response()
}

fn derive_content_hash(kind: &str, body: &CreateJobSpecRequest) -> String {
    use sha2::{Digest, Sha256};
    let mut hasher = Sha256::new();
    hasher.update(kind.as_bytes());
    hasher.update(b"|");
    hasher.update(body.pipeline_rid.as_bytes());
    hasher.update(b"|");
    hasher.update(body.branch_name.as_bytes());
    hasher.update(b"|");
    hasher.update(body.logic_payload.to_string().as_bytes());
    hasher.update(b"|");
    for o in &body.output_dataset_rids {
        hasher.update(o.as_bytes());
        hasher.update(b",");
    }
    format!("{:x}", hasher.finalize())
}

fn validate_kind_payload(kind: &str, body: &CreateJobSpecRequest) -> Result<(), String> {
    use crate::domain::runners::{logic_kinds, AnalyticalConfig, ExportConfig, HealthCheckConfig, SyncConfig};
    match kind {
        logic_kinds::SYNC => {
            let cfg: SyncConfig = serde_json::from_value(body.logic_payload.clone())
                .map_err(|e| format!("SYNC payload invalid: {e}"))?;
            if cfg.source_rid.is_empty() {
                return Err("SYNC source_rid required".into());
            }
            Ok(())
        }
        logic_kinds::HEALTH_CHECK => {
            let cfg: HealthCheckConfig = serde_json::from_value(body.logic_payload.clone())
                .map_err(|e| format!("HEALTH_CHECK payload invalid: {e}"))?;
            // Foundry semantics: the check produces a finding *on*
            // its single declared output dataset.
            if !body.output_dataset_rids.iter().any(|r| r == &cfg.target_dataset_rid) {
                return Err(format!(
                    "HEALTH_CHECK target {} must match the JobSpec's output dataset",
                    cfg.target_dataset_rid
                ));
            }
            Ok(())
        }
        logic_kinds::ANALYTICAL => {
            let _: AnalyticalConfig = serde_json::from_value(body.logic_payload.clone())
                .map_err(|e| format!("ANALYTICAL payload invalid: {e}"))?;
            Ok(())
        }
        logic_kinds::EXPORT => {
            let cfg: ExportConfig = serde_json::from_value(body.logic_payload.clone())
                .map_err(|e| format!("EXPORT payload invalid: {e}"))?;
            if cfg.acl_alias.is_none() {
                return Err("EXPORT acl_alias required: refusing unconfigured external push".into());
            }
            Ok(())
        }
        logic_kinds::TRANSFORM => Ok(()), // payload is opaque transform code
        _ => Ok(()),
    }
}

// ---------------------------------------------------------------------------
// GET /v1/jobs/{rid}/input-resolutions
// ---------------------------------------------------------------------------

pub async fn get_job_input_resolutions(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    let row: Option<(uuid::Uuid, serde_json::Value)> = sqlx::query_as(
        "SELECT id, input_view_resolutions FROM jobs WHERE rid = $1",
    )
    .bind(&rid)
    .fetch_optional(&state.db)
    .await
    .ok()
    .flatten();
    let Some((_id, resolutions)) = row else {
        return StatusCode::NOT_FOUND.into_response();
    };
    Json(json!({ "rid": rid, "input_view_resolutions": resolutions })).into_response()
}

// ---------------------------------------------------------------------------
// GET /v1/jobs/{rid}/outputs
// ---------------------------------------------------------------------------

#[derive(serde::Serialize, sqlx::FromRow)]
pub struct JobOutputRow {
    pub output_dataset_rid: String,
    pub transaction_rid: String,
    pub committed: bool,
    pub aborted: bool,
}

pub async fn get_job_outputs(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    // Resolve job_id from job RID (which encodes the UUID).
    let job: Option<(uuid::Uuid, String, bool)> = sqlx::query_as(
        "SELECT id, state, stale_skipped FROM jobs WHERE rid = $1",
    )
    .bind(&rid)
    .fetch_optional(&state.db)
    .await
    .ok()
    .flatten();
    let Some((job_id, job_state, stale_skipped)) = job else {
        return StatusCode::NOT_FOUND.into_response();
    };

    let rows = sqlx::query_as::<_, JobOutputRow>(
        r#"SELECT output_dataset_rid, transaction_rid, committed, aborted
             FROM job_outputs
            WHERE job_id = $1
            ORDER BY output_dataset_rid ASC"#,
    )
    .bind(job_id)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    Json(json!({
        "rid": rid,
        "state": job_state,
        "stale_skipped": stale_skipped,
        "outputs": rows,
    }))
    .into_response()
}

// ---------------------------------------------------------------------------
// GET /v1/datasets/{rid}/builds
// ---------------------------------------------------------------------------

pub async fn list_dataset_builds(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(dataset_rid): Path<String>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, Build>(
        r#"SELECT b.* FROM builds b
              JOIN build_input_locks l ON l.build_id = b.id
            WHERE l.output_dataset_rid = $1
            ORDER BY b.created_at DESC
            LIMIT 100"#,
    )
    .bind(&dataset_rid)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    Json(json!({"data": rows, "dataset_rid": dataset_rid})).into_response()
}
