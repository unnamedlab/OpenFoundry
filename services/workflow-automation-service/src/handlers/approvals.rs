use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::executor,
    models::{
        approval::InternalApprovalContinuationRequest,
        execution::WorkflowRun,
        workflow::{WorkflowDefinition, WorkflowStep},
    },
};

pub async fn continue_after_approval(
    State(state): State<AppState>,
    Path(_approval_id): Path<Uuid>,
    Json(body): Json<InternalApprovalContinuationRequest>,
) -> impl IntoResponse {
    let workflow =
        match sqlx::query_as::<_, WorkflowDefinition>(r#"SELECT * FROM workflows WHERE id = $1"#)
            .bind(body.workflow_id)
            .fetch_one(&state.db)
            .await
        {
            Ok(workflow) => workflow,
            Err(error) => {
                tracing::error!("workflow lookup for approval continuation failed: {error}");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
        };

    let mut run =
        match sqlx::query_as::<_, WorkflowRun>(r#"SELECT * FROM workflow_runs WHERE id = $1"#)
            .bind(body.workflow_run_id)
            .fetch_one(&state.db)
            .await
        {
            Ok(run) => run,
            Err(error) => {
                tracing::error!("run lookup for approval continuation failed: {error}");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
        };

    let steps = match workflow.parsed_steps() {
        Ok(steps) => steps,
        Err(error) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({ "error": error })),
            )
                .into_response();
        }
    };
    let Some(step): Option<&WorkflowStep> = steps.iter().find(|step| step.id == body.step_id)
    else {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "approval step not found in workflow" })),
        )
            .into_response();
    };

    run.context = body.context;

    match executor::continue_after_approval(&state, &workflow, run, &body.decision, step).await {
        Ok(run) => Json(run).into_response(),
        Err(error) => {
            tracing::error!("approval continuation failed: {error}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": error })),
            )
                .into_response()
        }
    }
}
