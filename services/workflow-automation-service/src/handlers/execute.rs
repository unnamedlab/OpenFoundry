use axum::{
    Json,
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    domain::executor,
    handlers::crud::load_workflow,
    models::{
        execution::InternalLineageRunRequest, execution::InternalTriggeredRunRequest,
        execution::StartRunRequest, execution::TriggerEventRequest,
    },
};

pub async fn start_manual_run(
    State(state): State<AppState>,
    Path(workflow_id): Path<Uuid>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Json(body): Json<StartRunRequest>,
) -> impl IntoResponse {
    let Some(workflow) = (match load_workflow(&state, workflow_id).await {
        Ok(workflow) => workflow,
        Err(error) => {
            tracing::error!("manual run lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    match executor::execute_workflow_run(
        &state,
        &workflow,
        "manual",
        Some(claims.sub),
        body.context,
    )
    .await
    {
        Ok(run) => (StatusCode::CREATED, Json(run)).into_response(),
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

pub async fn trigger_webhook(
    State(state): State<AppState>,
    Path(workflow_id): Path<Uuid>,
    headers: HeaderMap,
    Json(body): Json<TriggerEventRequest>,
) -> impl IntoResponse {
    let Some(workflow) = (match load_workflow(&state, workflow_id).await {
        Ok(workflow) => workflow,
        Err(error) => {
            tracing::error!("webhook lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    if workflow.trigger_type != "webhook" {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({ "error": "workflow is not configured for webhook triggers" })),
        )
            .into_response();
    }

    if let Some(expected_secret) = workflow.webhook_secret.as_deref() {
        let actual = headers
            .get("x-openfoundry-webhook-secret")
            .and_then(|value| value.to_str().ok())
            .unwrap_or_default();
        if actual != expected_secret {
            return StatusCode::UNAUTHORIZED.into_response();
        }
    }

    match executor::execute_workflow_run(
        &state,
        &workflow,
        "webhook",
        None,
        json!({
            "trigger": {
                "type": "webhook",
                "workflow_id": workflow_id,
            },
            "payload": body.context,
        }),
    )
    .await
    {
        Ok(run) => (StatusCode::CREATED, Json(run)).into_response(),
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

pub async fn start_internal_lineage_run(
    State(state): State<AppState>,
    Path(workflow_id): Path<Uuid>,
    Json(body): Json<InternalLineageRunRequest>,
) -> impl IntoResponse {
    let Some(workflow) = (match load_workflow(&state, workflow_id).await {
        Ok(workflow) => workflow,
        Err(error) => {
            tracing::error!("internal lineage run lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    if workflow.status != "active" {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({ "error": "workflow must be active to run from lineage" })),
        )
            .into_response();
    }

    match executor::execute_workflow_run(&state, &workflow, "lineage_build", None, body.context)
        .await
    {
        Ok(run) => (StatusCode::CREATED, Json(run)).into_response(),
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

pub async fn start_internal_triggered_run(
    State(state): State<AppState>,
    Path(workflow_id): Path<Uuid>,
    Json(body): Json<InternalTriggeredRunRequest>,
) -> impl IntoResponse {
    match execute_internal_triggered_run(&state, workflow_id, body).await {
        Ok(run) => (StatusCode::CREATED, Json(run)).into_response(),
        Err(error) if error.contains("not found") => {
            (StatusCode::NOT_FOUND, Json(json!({ "error": error }))).into_response()
        }
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

pub async fn execute_internal_triggered_run(
    state: &AppState,
    workflow_id: Uuid,
    body: InternalTriggeredRunRequest,
) -> Result<crate::models::execution::WorkflowRun, String> {
    let Some(workflow) = (match load_workflow(state, workflow_id).await {
        Ok(workflow) => workflow,
        Err(error) => {
            tracing::error!("internal triggered run lookup failed: {error}");
            return Err(error.to_string());
        }
    }) else {
        return Err(format!("workflow {workflow_id} not found"));
    };

    executor::execute_workflow_run(
        state,
        &workflow,
        &body.trigger_type,
        body.started_by,
        body.context,
    )
    .await
}
