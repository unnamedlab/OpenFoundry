use axum::{
    Json,
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
};
use chrono::Utc;
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    domain::temporal_adapter::{StartRunRequest as TemporalStartRunRequest, TemporalAdapter},
    handlers::crud::load_workflow,
    models::{
        execution::InternalLineageRunRequest, execution::InternalTriggeredRunRequest,
        execution::StartRunRequest, execution::TriggerEventRequest, workflow::WorkflowDefinition,
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

    match dispatch_run(&state, &workflow, "manual", Some(claims.sub), body.context).await {
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

    match dispatch_run(
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

    match dispatch_run(&state, &workflow, "lineage_build", None, body.context).await {
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

    dispatch_run(
        state,
        &workflow,
        &body.trigger_type,
        body.started_by,
        body.context,
    )
    .await
}

async fn dispatch_run(
    state: &AppState,
    workflow: &WorkflowDefinition,
    trigger_type: &str,
    started_by: Option<Uuid>,
    context: serde_json::Value,
) -> Result<crate::models::execution::WorkflowRun, String> {
    if workflow.status != "active" && trigger_type != "manual" {
        return Err("workflow must be active for automatic execution".to_string());
    }

    let run_id = Uuid::now_v7();
    let adapter = TemporalAdapter::new(
        state.workflow_client.clone(),
        state.temporal_namespace.clone(),
    );
    let start_request = TemporalStartRunRequest {
        run_id,
        definition_id: workflow.id,
        tenant_id: workflow_tenant_id(workflow),
        triggered_by: started_by
            .map(|value| value.to_string())
            .unwrap_or_else(|| "system".to_string()),
        trigger_payload: context.clone(),
    };

    let handle = adapter
        .start_run(&start_request, run_id)
        .await
        .map_err(|error| error.to_string())?;

    let _ = sqlx::query(
        r#"UPDATE workflows
           SET last_triggered_at = NOW(), updated_at = NOW()
           WHERE id = $1"#,
    )
    .bind(workflow.id)
    .execute(&state.db)
    .await;

    Ok(accepted_run(
        workflow.id,
        run_id,
        trigger_type,
        started_by,
        context,
        handle.run_id.0,
    ))
}

fn workflow_tenant_id(workflow: &WorkflowDefinition) -> String {
    workflow
        .trigger_config
        .get("tenant_id")
        .and_then(serde_json::Value::as_str)
        .map(str::to_string)
        .unwrap_or_else(|| workflow.owner_id.to_string())
}

fn accepted_run(
    workflow_id: Uuid,
    run_id: Uuid,
    trigger_type: &str,
    started_by: Option<Uuid>,
    context: serde_json::Value,
    temporal_run_id: String,
) -> crate::models::execution::WorkflowRun {
    crate::models::execution::WorkflowRun {
        id: run_id,
        workflow_id,
        trigger_type: trigger_type.to_string(),
        status: "running".to_string(),
        started_by,
        current_step_id: None,
        context: json!({
            "input": context,
            "temporal": {
                "workflow_id": format!("workflow-automation:{run_id}"),
                "run_id": temporal_run_id,
                "authoritative": true,
            }
        }),
        error_message: None,
        started_at: Utc::now(),
        finished_at: None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn accepted_run_returns_api_compatible_temporal_state() {
        let workflow_id = Uuid::now_v7();
        let run_id = Uuid::now_v7();
        let started_by = Uuid::now_v7();

        let run = accepted_run(
            workflow_id,
            run_id,
            "manual",
            Some(started_by),
            json!({"customer_id": "c-1"}),
            "temporal-run-1".into(),
        );

        assert_eq!(run.id, run_id);
        assert_eq!(run.workflow_id, workflow_id);
        assert_eq!(run.status, "running");
        assert_eq!(run.started_by, Some(started_by));
        assert_eq!(run.context["input"]["customer_id"], "c-1");
        assert_eq!(
            run.context["temporal"]["workflow_id"],
            format!("workflow-automation:{run_id}")
        );
        assert_eq!(run.context["temporal"]["run_id"], "temporal-run-1");
        assert_eq!(run.context["temporal"]["authoritative"], true);
    }
}
