use event_bus::{
    connect,
    schemas::Event,
    subscriber,
    topics::{streams, subjects},
    workflows::WorkflowRunRequested,
};
use futures::StreamExt;

use crate::{
    AppState,
    handlers::execute::execute_internal_triggered_run,
    models::execution::InternalTriggeredRunRequest,
};

pub async fn consume(state: AppState) -> Result<(), String> {
    let js = connect(&state.nats_url)
        .await
        .map_err(|error| format!("failed to connect to NATS: {error}"))?;
    let stream = subscriber::ensure_stream(&js, streams::EVENTS, &[subjects::WORKFLOWS])
        .await
        .map_err(|error| format!("failed to ensure workflow event stream: {error}"))?;
    let subject = format!("{}.run.requested", subjects::WORKFLOWS);
    let consumer =
        subscriber::create_consumer(&stream, "workflow-automation-run-requested", Some(&subject))
            .await
            .map_err(|error| format!("failed to create workflow run consumer: {error}"))?;
    let mut messages = consumer
        .messages()
        .await
        .map_err(|error| format!("failed to open workflow run message stream: {error}"))?;

    while let Some(message) = messages.next().await {
        let message = match message {
            Ok(message) => message,
            Err(error) => {
                tracing::warn!("workflow run consumer message read failed: {error}");
                continue;
            }
        };

        let event = match serde_json::from_slice::<Event<WorkflowRunRequested>>(&message.payload) {
            Ok(event) => event,
            Err(error) => {
                tracing::warn!("workflow run consumer payload decode failed: {error}");
                continue;
            }
        };

        let request = InternalTriggeredRunRequest {
            trigger_type: event.payload.trigger_type.clone(),
            started_by: event.payload.started_by,
            context: event.payload.context.clone(),
        };

        match execute_internal_triggered_run(&state, event.payload.workflow_id, request).await {
            Ok(run) => {
                message
                    .ack()
                    .await
                    .map_err(|error| format!("failed to ack workflow.run.requested: {error}"))?;
                tracing::info!(
                    workflow_id = %run.workflow_id,
                    run_id = %run.id,
                    correlation_id = %event.payload.correlation_id,
                    "workflow.run.requested accepted"
                );
            }
            Err(error) => {
                tracing::warn!(
                    workflow_id = %event.payload.workflow_id,
                    correlation_id = %event.payload.correlation_id,
                    "workflow.run.requested failed: {error}"
                );
            }
        }
    }

    Ok(())
}
