use serde_json::{Value, json};
use uuid::Uuid;

use crate::{
    AppState,
    domain::schedule,
    models::{
        schedule::{BackfillRunResult, DueRunRecord, ScheduleTargetKind, ScheduleWindow},
        workflow::WorkflowDefinition,
        workflow_execution::{InternalTriggeredRunRequest, WorkflowRun},
    },
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

pub async fn list_due_workflow_runs(
    state: &AppState,
    limit: usize,
) -> Result<Vec<DueRunRecord>, String> {
    let workflows = sqlx::query_as::<_, WorkflowDefinition>(
        r#"SELECT * FROM workflows
           WHERE status = 'active'
             AND trigger_type = 'cron'
             AND next_run_at IS NOT NULL
             AND next_run_at <= NOW()
           ORDER BY next_run_at ASC
           LIMIT $1"#,
    )
    .bind(limit as i64)
    .fetch_all(&state.db)
    .await
    .map_err(|error| error.to_string())?;

    Ok(workflows
        .into_iter()
        .filter_map(|workflow| {
            let schedule_expression = workflow_schedule_expression(&workflow)?;
            Some(DueRunRecord {
                target_kind: ScheduleTargetKind::Workflow,
                target_id: workflow.id,
                name: workflow.name,
                due_at: workflow.next_run_at?,
                schedule_expression,
                trigger_type: workflow.trigger_type,
            })
        })
        .collect())
}

pub async fn trigger_internal_workflow_run(
    state: &AppState,
    workflow_id: Uuid,
    trigger_type: &str,
    started_by: Option<Uuid>,
    context: Value,
) -> Result<WorkflowRun, String> {
    let endpoint = format!(
        "{}/internal/workflows/{workflow_id}/runs/trigger",
        state.workflow_service_url.trim_end_matches('/'),
    );

    let response = state
        .http_client
        .post(endpoint)
        .json(&InternalTriggeredRunRequest {
            trigger_type: trigger_type.to_string(),
            started_by,
            context,
        })
        .send()
        .await
        .map_err(|error| error.to_string())?;

    if !response.status().is_success() {
        let status = response.status();
        let body = response.text().await.unwrap_or_default();
        return Err(format!(
            "workflow-service trigger failed with status {status}: {body}"
        ));
    }

    response.json::<WorkflowRun>().await.map_err(|error| error.to_string())
}

pub async fn run_due_cron_workflows(state: &AppState) -> Result<usize, String> {
    let workflows = sqlx::query_as::<_, WorkflowDefinition>(
        r#"SELECT * FROM workflows
           WHERE status = 'active'
             AND trigger_type = 'cron'
             AND next_run_at IS NOT NULL
             AND next_run_at <= NOW()
           ORDER BY next_run_at ASC"#,
    )
    .fetch_all(&state.db)
    .await
    .map_err(|error| error.to_string())?;

    let mut triggered = 0usize;
    for workflow in workflows {
        let context = json!({
            "trigger": {
                "type": "cron",
                "scheduled_at": chrono::Utc::now(),
            }
        });

        match trigger_internal_workflow_run(state, workflow.id, "cron", None, context).await {
            Ok(_) => triggered += 1,
            Err(error) => {
                tracing::warn!(workflow_id = %workflow.id, "cron workflow trigger failed: {error}");
            }
        }
    }

    Ok(triggered)
}

pub async fn trigger_event_workflows(
    state: &AppState,
    event_name: &str,
    actor_id: Uuid,
    event_context: Value,
) -> Result<Vec<WorkflowRun>, String> {
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

pub async fn backfill_workflow_runs(
    state: &AppState,
    workflow: &WorkflowDefinition,
    windows: &[ScheduleWindow],
    started_by: Option<Uuid>,
    context: Option<Value>,
) -> Result<Vec<BackfillRunResult>, String> {
    let mut results = Vec::new();

    for window in windows {
        let payload = schedule::merge_json(
            context.clone(),
            json!({
                "trigger": {
                    "type": "backfill",
                    "scheduled_for": window.scheduled_for,
                    "window": {
                        "start": window.window_start,
                        "end": window.window_end,
                    }
                }
            }),
        );
        let run = trigger_internal_workflow_run(
            state,
            workflow.id,
            "backfill",
            started_by,
            payload,
        )
        .await?;

        results.push(BackfillRunResult {
            target_kind: ScheduleTargetKind::Workflow,
            target_id: workflow.id,
            scheduled_for: window.scheduled_for,
            window_start: window.window_start,
            window_end: window.window_end,
            run_id: Some(run.id),
            status: "triggered".to_string(),
        });
    }

    Ok(results)
}