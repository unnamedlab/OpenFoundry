use serde_json::{Value, json};
use uuid::Uuid;

use crate::{AppState, models::workflow::WorkflowDefinition};
use event_bus_control::{
    Publisher, connect, subscriber,
    topics::{streams, subjects},
    workflows::WorkflowRunRequested,
};

pub async fn load_workflow(
    state: &AppState,
    workflow_id: Uuid,
) -> Result<Option<WorkflowDefinition>, String> {
    sqlx::query_as::<_, WorkflowDefinition>("SELECT * FROM workflows WHERE id = $1")
        .bind(workflow_id)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| error.to_string())
}

pub fn workflow_schedule_expression(workflow: &WorkflowDefinition) -> Option<String> {
    if workflow.trigger_type != "cron" {
        return None;
    }

    workflow
        .trigger_config
        .get("cron")
        .and_then(Value::as_str)
        .map(str::to_string)
}

pub async fn trigger_internal_workflow_run(
    state: &AppState,
    workflow_id: Uuid,
    trigger_type: &str,
    started_by: Option<Uuid>,
    context: Value,
) -> Result<WorkflowRunRequested, String> {
    let js = connect(&state.nats_url)
        .await
        .map_err(|error| format!("failed to connect to NATS: {error}"))?;
    subscriber::ensure_stream(&js, streams::EVENTS, &[subjects::WORKFLOWS])
        .await
        .map_err(|error| format!("failed to ensure workflow event stream: {error}"))?;

    let request = WorkflowRunRequested {
        workflow_id,
        trigger_type: trigger_type.to_string(),
        started_by,
        context,
        correlation_id: Uuid::now_v7(),
    };
    let subject = format!("{}.run.requested", subjects::WORKFLOWS);

    Publisher::new(js, "pipeline-schedule-service")
        .publish(&subject, "workflow.run.requested", &request)
        .await
        .map_err(|error| format!("failed to publish workflow.run.requested: {error}"))?;

    Ok(request)
}

pub async fn trigger_event_workflows(
    state: &AppState,
    event_name: &str,
    actor_id: Uuid,
    event_context: Value,
) -> Result<Vec<WorkflowRunRequested>, String> {
    let workflows = sqlx::query_as::<_, WorkflowDefinition>(
        r#"SELECT * FROM workflows
           WHERE status = 'active'
             AND trigger_type = 'event'
             AND trigger_config ->> 'event_name' = $1
           ORDER BY updated_at DESC"#,
    )
    .bind(event_name)
    .fetch_all(&state.db)
    .await
    .map_err(|error| error.to_string())?;

    let mut runs = Vec::new();
    for workflow in workflows {
        let context = json!({
            "trigger": {
                "type": "event",
                "name": event_name,
                "actor_id": actor_id,
            },
            "event": event_context.clone(),
        });

        match trigger_internal_workflow_run(state, workflow.id, "event", Some(actor_id), context)
            .await
        {
            Ok(run) => runs.push(run),
            Err(error) => {
                tracing::warn!(workflow_id = %workflow.id, "event trigger failed: {error}");
            }
        }
    }

    Ok(runs)
}

