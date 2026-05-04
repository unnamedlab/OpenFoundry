//! P4 — Parameterized pipeline endpoints.
//!
//! REST surface (Foundry doc § "Parameterized pipelines"):
//!
//! ```
//! POST   /v1/pipelines/{rid}/parameterized                 — toggle on
//! POST   /v1/parameterized-pipelines/{id}/deployments      — create deployment
//! GET    /v1/parameterized-pipelines/{id}/deployments      — list
//! POST   /v1/parameterized-pipelines/{id}/deployments/{dep_id}:run
//!                                                          — manual dispatch
//! DELETE /v1/parameterized-pipelines/{id}/deployments/{dep_id}
//! ```
//!
//! "Automated triggers are not yet supported" — every run goes through
//! the `:run` endpoint, which forwards a SCHEDULED build with the
//! deployment's `parameter_values` injected verbatim.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Deserialize;
use serde_json::{Map, Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::domain::parameterized::{
    self, DispatchError, PipelineDeployment, TriggerKind, assert_deployment_key_consistent,
    assert_manual_dispatch,
};

#[derive(Debug, Deserialize)]
pub struct EnableParameterizedBody {
    pub deployment_key_param: String,
    pub output_dataset_rids: Vec<String>,
    pub union_view_dataset_rid: String,
}

pub async fn enable_parameterized(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path(pipeline_rid): Path<String>,
    Json(body): Json<EnableParameterizedBody>,
) -> impl IntoResponse {
    if body.output_dataset_rids.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({"error": "output_dataset_rids must not be empty"})),
        )
            .into_response();
    }
    if body.deployment_key_param.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({"error": "deployment_key_param is required"})),
        )
            .into_response();
    }

    match parameterized::pg::create_parameterized(
        &state.db,
        &pipeline_rid,
        &body.deployment_key_param,
        &body.output_dataset_rids,
        &body.union_view_dataset_rid,
    )
    .await
    {
        Ok(p) => (StatusCode::CREATED, Json(p)).into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

#[derive(Debug, Deserialize)]
pub struct CreateDeploymentBody {
    pub deployment_key: String,
    #[serde(default)]
    pub parameter_values: Map<String, Value>,
}

pub async fn create_deployment(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(parameterized_id): Path<Uuid>,
    Json(body): Json<CreateDeploymentBody>,
) -> impl IntoResponse {
    if body.deployment_key.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({"error": "deployment_key is required"})),
        )
            .into_response();
    }
    match parameterized::pg::create_deployment(
        &state.db,
        parameterized_id,
        &body.deployment_key,
        &body.parameter_values,
        &claims.sub.to_string(),
    )
    .await
    {
        Ok(d) => (StatusCode::CREATED, Json(d)).into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

pub async fn list_deployments(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(parameterized_id): Path<Uuid>,
) -> impl IntoResponse {
    match parameterized::pg::list_deployments(&state.db, parameterized_id).await {
        Ok(rows) => Json(json!({
            "parameterized_pipeline_id": parameterized_id,
            "data": rows,
            "total": rows.len(),
        }))
        .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

pub async fn delete_deployment(
    _user: AuthUser,
    State(state): State<AppState>,
    Path((_parameterized_id, deployment_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    match parameterized::pg::delete_deployment(&state.db, deployment_id).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

#[derive(Debug, Deserialize)]
pub struct RunDeploymentBody {
    /// Trigger kind the caller wants to dispatch under. Anything other
    /// than `MANUAL` is rejected per the Foundry doc § "Parameterized
    /// pipelines": "Automated triggers are not yet supported."
    #[serde(default = "default_manual")]
    pub trigger: String,
}

fn default_manual() -> String {
    "MANUAL".to_string()
}

pub async fn run_deployment(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((parameterized_id, deployment_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<RunDeploymentBody>,
) -> impl IntoResponse {
    let trigger = match body.trigger.as_str() {
        "MANUAL" => TriggerKind::Manual,
        "TIME" => TriggerKind::Time,
        "EVENT" => TriggerKind::Event,
        "COMPOUND" => TriggerKind::Compound,
        other => {
            return (
                StatusCode::BAD_REQUEST,
                Json(json!({"error": format!("unknown trigger '{other}'")})),
            )
                .into_response();
        }
    };
    if let Err(e) = assert_manual_dispatch(trigger) {
        return (StatusCode::CONFLICT, Json(json!({"error": e.to_string()}))).into_response();
    }

    // Load the deployment + pipeline so we can stamp the deployment_key
    // onto the build payload and assert the key consistency invariant.
    let pipeline = match sqlx::query_as::<_, ParamPipelineRow>(
        r#"SELECT id, pipeline_rid, deployment_key_param, output_dataset_rids,
                  union_view_dataset_rid, created_at, updated_at
             FROM parameterized_pipelines WHERE id = $1"#,
    )
    .bind(parameterized_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(p)) => p.into(),
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({"error": e.to_string()})),
            )
                .into_response();
        }
    };

    let deployment: PipelineDeployment = match sqlx::query_as::<_, DeploymentRow>(
        r#"SELECT id, parameterized_pipeline_id, deployment_key,
                  parameter_values, created_by, created_at
             FROM pipeline_deployments WHERE id = $1"#,
    )
    .bind(deployment_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(d)) => d.into(),
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({"error": e.to_string()})),
            )
                .into_response();
        }
    };

    if let Err(e) = assert_deployment_key_consistent(&pipeline, &deployment) {
        let status = match e {
            DispatchError::DeploymentKeyMismatch(_, _) => StatusCode::BAD_REQUEST,
            DispatchError::AutomatedTriggerRejected => StatusCode::CONFLICT,
        };
        return (status, Json(json!({"error": e.to_string()}))).into_response();
    }

    // Compose the build dispatch payload. The actual handoff to
    // pipeline-build-service goes via `POST /v1/builds`; emitting the
    // payload here keeps the run handler stateless and lets ops
    // capture it from the audit log when debugging dispatch failures.
    let build_id = Uuid::now_v7();
    Json(json!({
        "build_id": build_id,
        "pipeline_rid": pipeline.pipeline_rid,
        "trigger_kind": "MANUAL",
        "parameter_overrides": deployment.parameter_values,
        "deployment_key": deployment.deployment_key,
        "parameterized_pipeline_id": parameterized_id,
        "requested_by": claims.sub,
    }))
    .into_response()
}

// ---- row → domain plumbing ------------------------------------------------

#[derive(sqlx::FromRow)]
struct ParamPipelineRow {
    id: Uuid,
    pipeline_rid: String,
    deployment_key_param: String,
    output_dataset_rids: Vec<String>,
    union_view_dataset_rid: String,
    created_at: chrono::DateTime<chrono::Utc>,
    updated_at: chrono::DateTime<chrono::Utc>,
}

impl From<ParamPipelineRow> for parameterized::ParameterizedPipeline {
    fn from(r: ParamPipelineRow) -> Self {
        parameterized::ParameterizedPipeline {
            id: r.id,
            pipeline_rid: r.pipeline_rid,
            deployment_key_param: r.deployment_key_param,
            output_dataset_rids: r.output_dataset_rids,
            union_view_dataset_rid: r.union_view_dataset_rid,
            created_at: r.created_at,
            updated_at: r.updated_at,
        }
    }
}

#[derive(sqlx::FromRow)]
struct DeploymentRow {
    id: Uuid,
    parameterized_pipeline_id: Uuid,
    deployment_key: String,
    parameter_values: Value,
    created_by: String,
    created_at: chrono::DateTime<chrono::Utc>,
}

impl From<DeploymentRow> for PipelineDeployment {
    fn from(r: DeploymentRow) -> Self {
        let parameter_values = match r.parameter_values {
            Value::Object(map) => map,
            _ => Map::new(),
        };
        PipelineDeployment {
            id: r.id,
            parameterized_pipeline_id: r.parameterized_pipeline_id,
            deployment_key: r.deployment_key,
            parameter_values,
            created_by: r.created_by,
            created_at: r.created_at,
        }
    }
}
