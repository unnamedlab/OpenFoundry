use std::str::FromStr;

use chrono::{DateTime, Duration, Utc};
use cron::Schedule;
use serde_json::{Value, json};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{executor, workflow},
    models::{
        pipeline::Pipeline,
        schedule::{BackfillRunResult, DueRunRecord, ScheduleTargetKind, ScheduleWindow},
    },
};

const DEFAULT_LIMIT: usize = 50;
const MAX_LIMIT: usize = 200;

pub fn clamp_limit(limit: Option<usize>) -> usize {
    limit.unwrap_or(DEFAULT_LIMIT).clamp(1, MAX_LIMIT)
}

pub fn build_schedule_windows(
    expression: &str,
    start_at: DateTime<Utc>,
    end_at: DateTime<Utc>,
    limit: usize,
) -> Result<Vec<ScheduleWindow>, String> {
    if end_at < start_at {
        return Err("end_at must be greater than or equal to start_at".to_string());
    }

    let schedule = Schedule::from_str(expression)
        .map_err(|error| format!("invalid cron expression '{expression}': {error}"))?;
    let seed = start_at - Duration::seconds(1);
    let occurrences: Vec<_> = schedule
        .after(&seed)
        .take_while(|scheduled_for| *scheduled_for <= end_at)
        .take(limit.saturating_add(1))
        .collect();

    let mut windows = Vec::new();
    for (index, scheduled_for) in occurrences.iter().take(limit).enumerate() {
        let mut window_end = occurrences.get(index + 1).copied().unwrap_or(end_at);
        if window_end < *scheduled_for {
            window_end = *scheduled_for;
        }
        if window_end > end_at {
            window_end = end_at;
        }

        windows.push(ScheduleWindow {
            scheduled_for: *scheduled_for,
            window_start: *scheduled_for,
            window_end,
        });
    }

    Ok(windows)
}

pub async fn load_pipeline(state: &AppState, pipeline_id: Uuid) -> Result<Option<Pipeline>, String> {
    sqlx::query_as::<_, Pipeline>("SELECT * FROM pipelines WHERE id = $1")
        .bind(pipeline_id)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| error.to_string())
}

pub fn pipeline_schedule_expression(pipeline: &Pipeline) -> Option<String> {
    let schedule = pipeline.schedule();
    if pipeline.status != "active" || !schedule.enabled {
        return None;
    }

    schedule.cron
}

pub async fn list_due_pipeline_runs(
    state: &AppState,
    limit: usize,
) -> Result<Vec<DueRunRecord>, String> {
    let pipelines = sqlx::query_as::<_, Pipeline>(
        r#"SELECT * FROM pipelines
           WHERE status = 'active'
             AND next_run_at IS NOT NULL
             AND next_run_at <= NOW()
           ORDER BY next_run_at ASC
           LIMIT $1"#,
    )
    .bind(limit as i64)
    .fetch_all(&state.db)
    .await
    .map_err(|error| error.to_string())?;

    Ok(pipelines
        .into_iter()
        .filter_map(|pipeline| {
            let schedule_expression = pipeline_schedule_expression(&pipeline)?;
            Some(DueRunRecord {
                target_kind: ScheduleTargetKind::Pipeline,
                target_id: pipeline.id,
                name: pipeline.name,
                due_at: pipeline.next_run_at?,
                schedule_expression,
                trigger_type: "scheduled".to_string(),
            })
        })
        .collect())
}

pub async fn list_due_runs(
    state: &AppState,
    kind: Option<ScheduleTargetKind>,
    limit: usize,
) -> Result<Vec<DueRunRecord>, String> {
    let mut due_runs = Vec::new();

    match kind {
        Some(ScheduleTargetKind::Pipeline) => {
            due_runs.extend(list_due_pipeline_runs(state, limit).await?);
        }
        Some(ScheduleTargetKind::Workflow) => {
            due_runs.extend(workflow::list_due_workflow_runs(state, limit).await?);
        }
        None => {
            due_runs.extend(list_due_pipeline_runs(state, limit).await?);
            due_runs.extend(workflow::list_due_workflow_runs(state, limit).await?);
        }
    }

    due_runs.sort_by(|left, right| left.due_at.cmp(&right.due_at));
    due_runs.truncate(limit);
    Ok(due_runs)
}

pub fn merge_json(base: Option<Value>, patch: Value) -> Value {
    fn merge_values(target: &mut Value, patch: &Value) {
        match (target, patch) {
            (Value::Object(target_obj), Value::Object(patch_obj)) => {
                for (key, value) in patch_obj {
                    match target_obj.get_mut(key) {
                        Some(existing) => merge_values(existing, value),
                        None => {
                            target_obj.insert(key.clone(), value.clone());
                        }
                    }
                }
            }
            (target, patch) => {
                *target = patch.clone();
            }
        }
    }

    let mut merged = base.unwrap_or_else(|| json!({}));
    merge_values(&mut merged, &patch);
    merged
}

pub async fn preview_windows(
    state: &AppState,
    target_kind: ScheduleTargetKind,
    target_id: Uuid,
    start_at: DateTime<Utc>,
    end_at: DateTime<Utc>,
    limit: usize,
) -> Result<Vec<ScheduleWindow>, String> {
    let expression = match target_kind {
        ScheduleTargetKind::Pipeline => {
            let pipeline = load_pipeline(state, target_id)
                .await?
                .ok_or_else(|| format!("pipeline {target_id} not found"))?;
            pipeline_schedule_expression(&pipeline)
                .ok_or_else(|| format!("pipeline {target_id} does not have an active cron schedule"))?
        }
        ScheduleTargetKind::Workflow => {
            let workflow = workflow::load_workflow(state, target_id)
                .await?
                .ok_or_else(|| format!("workflow {target_id} not found"))?;
            workflow::workflow_schedule_expression(&workflow)
                .ok_or_else(|| format!("workflow {target_id} does not have a cron trigger"))?
        }
    };

    build_schedule_windows(&expression, start_at, end_at, limit)
}

pub async fn backfill_runs(
    state: &AppState,
    target_kind: ScheduleTargetKind,
    target_id: Uuid,
    start_at: DateTime<Utc>,
    end_at: DateTime<Utc>,
    limit: usize,
    dry_run: bool,
    context: Option<Value>,
    skip_unchanged: bool,
    started_by: Option<Uuid>,
) -> Result<Vec<BackfillRunResult>, String> {
    let windows = preview_windows(state, target_kind, target_id, start_at, end_at, limit).await?;

    if dry_run {
        return Ok(windows
            .into_iter()
            .map(|window| BackfillRunResult {
                target_kind,
                target_id,
                scheduled_for: window.scheduled_for,
                window_start: window.window_start,
                window_end: window.window_end,
                run_id: None,
                status: "planned".to_string(),
            })
            .collect());
    }

    match target_kind {
        ScheduleTargetKind::Pipeline => {
            let pipeline = load_pipeline(state, target_id)
                .await?
                .ok_or_else(|| format!("pipeline {target_id} not found"))?;
            let mut results = Vec::new();

            for window in windows {
                let payload = merge_json(
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
                let run = executor::start_pipeline_run(
                    state,
                    &pipeline,
                    started_by,
                    "backfill",
                    None,
                    None,
                    1,
                    state.distributed_pipeline_workers.max(1),
                    skip_unchanged,
                    payload,
                )
                .await?;

                results.push(BackfillRunResult {
                    target_kind,
                    target_id,
                    scheduled_for: window.scheduled_for,
                    window_start: window.window_start,
                    window_end: window.window_end,
                    run_id: Some(run.id),
                    status: "triggered".to_string(),
                });
            }

            Ok(results)
        }
        ScheduleTargetKind::Workflow => {
            let workflow = workflow::load_workflow(state, target_id)
                .await?
                .ok_or_else(|| format!("workflow {target_id} not found"))?;
            workflow::backfill_workflow_runs(state, &workflow, &windows, started_by, context).await
        }
    }
}

#[cfg(test)]
mod tests {
    use chrono::Utc;
    use chrono::TimeZone;

    use super::build_schedule_windows;

    #[test]
    fn builds_windows_for_hourly_schedule() {
        let start_at = Utc.with_ymd_and_hms(2026, 4, 27, 8, 0, 0).unwrap();
        let end_at = Utc.with_ymd_and_hms(2026, 4, 27, 11, 0, 0).unwrap();

        let windows = build_schedule_windows("0 0 * * * *", start_at, end_at, 10).unwrap();

        assert_eq!(windows.len(), 4);
        assert_eq!(windows[0].window_start, start_at);
        assert_eq!(windows[0].window_end, Utc.with_ymd_and_hms(2026, 4, 27, 9, 0, 0).unwrap());
        assert_eq!(windows[3].window_end, end_at);
    }

    #[test]
    fn rejects_inverted_window_ranges() {
        let start_at = Utc.with_ymd_and_hms(2026, 4, 27, 11, 0, 0).unwrap();
        let end_at = Utc.with_ymd_and_hms(2026, 4, 27, 8, 0, 0).unwrap();

        let error = build_schedule_windows("0 0 * * * *", start_at, end_at, 10).unwrap_err();
        assert!(error.contains("end_at"));
    }
}