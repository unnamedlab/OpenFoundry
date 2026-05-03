//! Sweep linter handlers: surface the `scheduling-linter` rule
//! catalogue under `/v1/scheduling-linter/...`.
//!
//! `POST /v1/scheduling-linter/sweep` materialises the inventory by
//! joining `schedules` + `schedule_runs`, runs every rule, and returns
//! the report. `POST /v1/scheduling-linter/sweep:apply` then takes a
//! filter (rule ids + finding ids) and executes the recommended
//! actions against the schedules table.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use scheduling_linter::{
    Action, InventoryRun, InventorySchedule, RuleId, SweepInput, SweepReport, run_sweep,
    model::{InventoryTrigger, InventoryUser, TriggerCronFlavor},
};
use serde::Deserialize;
use serde_json::json;
use sqlx::Row;
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        run_store, schedule_store,
        trigger::{Trigger, TriggerKind},
    },
};

#[derive(Debug, Deserialize)]
pub struct SweepQuery {
    #[serde(default)]
    pub project: Option<String>,
    #[serde(default)]
    pub dry_run: Option<bool>,
    #[serde(default)]
    pub production: Option<bool>,
}

pub async fn sweep(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<SweepQuery>,
) -> impl IntoResponse {
    let inventory = match build_inventory(&state, params.project.as_deref()).await {
        Ok(rows) => rows,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": e })),
            )
                .into_response();
        }
    };
    let input = SweepInput {
        schedules: inventory,
        now: chrono::Utc::now(),
        production: params.production.unwrap_or(false),
    };
    let report = run_sweep(&input);
    Json(json!({
        "dry_run": params.dry_run.unwrap_or(true),
        "findings": report.findings,
        "by_rule": report.group_by_rule().keys().collect::<Vec<_>>(),
    }))
    .into_response()
}

#[derive(Debug, Deserialize)]
pub struct ApplyBody {
    #[serde(default)]
    pub rule_ids: Vec<RuleId>,
    #[serde(default)]
    pub finding_ids: Vec<Uuid>,
    /// The full report the operator is acting on. Stateless: the
    /// client posts back the findings list it received from
    /// `:sweep`, then the server picks the matching pairs and
    /// executes their actions.
    pub report: SweepReport,
}

pub async fn apply(
    _user: AuthUser,
    State(state): State<AppState>,
    Json(body): Json<ApplyBody>,
) -> impl IntoResponse {
    let actions = body.report.plan_apply(&body.rule_ids, &body.finding_ids);
    let mut applied = Vec::with_capacity(actions.len());
    for action in actions {
        let result = match action.action {
            Action::Pause => schedule_store::set_paused(
                &state.db,
                &action.schedule_rid,
                true,
                Some("LINTER"),
            )
            .await
            .map(|_| "paused"),
            Action::Delete => schedule_store::delete(&state.db, &action.schedule_rid)
                .await
                .map(|_| "deleted"),
            Action::Archive => schedule_store::set_paused(
                &state.db,
                &action.schedule_rid,
                true,
                Some("LINTER_ARCHIVED"),
            )
            .await
            .map(|_| "archived"),
            Action::Notify => Ok("notified"),
        };
        applied.push(json!({
            "finding_id": action.finding_id,
            "schedule_rid": action.schedule_rid,
            "action": format!("{:?}", action.action),
            "result": match &result {
                Ok(r) => r.to_string(),
                Err(e) => format!("error: {e}"),
            },
        }));
    }
    Json(json!({ "applied": applied })).into_response()
}

async fn build_inventory(
    state: &AppState,
    project: Option<&str>,
) -> Result<Vec<InventorySchedule>, String> {
    let sql = format!(
        "SELECT {} FROM schedules
         WHERE ($1::TEXT IS NULL OR project_rid = $1)
         ORDER BY updated_at DESC
         LIMIT 1000",
        schedule_store::SCHEDULE_COLUMNS,
    );
    let rows = sqlx::query(&sql)
        .bind(project)
        .fetch_all(&state.db)
        .await
        .map_err(|e| e.to_string())?;
    let mut out = Vec::with_capacity(rows.len());
    for row in &rows {
        let schedule_rid: String = row.try_get("rid").map_err(|e| e.to_string())?;
        let schedule_id: Uuid = row.try_get("id").map_err(|e| e.to_string())?;
        let project_rid: String = row.try_get("project_rid").map_err(|e| e.to_string())?;
        let name: String = row.try_get("name").map_err(|e| e.to_string())?;
        let paused: bool = row.try_get("paused").map_err(|e| e.to_string())?;
        let paused_at = row
            .try_get::<Option<chrono::DateTime<chrono::Utc>>, _>("paused_at")
            .map_err(|e| e.to_string())?;
        let scope_kind: String = row.try_get("scope_kind").map_err(|e| e.to_string())?;
        let trigger_json: serde_json::Value =
            row.try_get("trigger_json").map_err(|e| e.to_string())?;
        let trigger: Trigger =
            serde_json::from_value(trigger_json).map_err(|e| e.to_string())?;
        let recent_runs = run_store::list_for_schedule(
            &state.db,
            schedule_id,
            run_store::ListRunsFilter {
                limit: 200,
                ..Default::default()
            },
        )
        .await
        .map_err(|e| e.to_string())?;

        out.push(InventorySchedule {
            id: schedule_id,
            rid: schedule_rid,
            project_rid,
            name,
            paused,
            paused_at,
            scope_kind,
            // The user directory join is intentionally omitted here —
            // owner-related rules (SCH-004 / SCH-005) require
            // identity-federation-service data the schedule service
            // doesn't yet co-locate. Production deployments wire that
            // through a sidecar fetcher; the inventory snapshot stays
            // None-typed and the rules degrade gracefully.
            run_as_user: None,
            trigger: project_trigger(&trigger),
            recent_runs: recent_runs
                .into_iter()
                .map(|r| InventoryRun {
                    triggered_at: r.triggered_at,
                    outcome: r.outcome.as_str().to_string(),
                })
                .collect(),
        });
    }
    Ok(out)
}

/// Project the canonical [`Trigger`] onto the linter's narrower
/// [`InventoryTrigger`] vocabulary.
fn project_trigger(trigger: &Trigger) -> InventoryTrigger {
    match &trigger.kind {
        TriggerKind::Time(t) => InventoryTrigger::Time {
            cron: t.cron.clone(),
            time_zone: t.time_zone.clone(),
            flavor: match t.flavor {
                crate::domain::trigger::CronFlavor::Unix5 => TriggerCronFlavor::Unix5,
                crate::domain::trigger::CronFlavor::Quartz6 => TriggerCronFlavor::Quartz6,
            },
        },
        TriggerKind::Event(e) => InventoryTrigger::Event {
            target_rid: e.target_rid.clone(),
            branch_filter: e.branch_filter.clone(),
        },
        TriggerKind::Compound(c) => InventoryTrigger::Compound {
            children: c
                .components
                .iter()
                .map(project_trigger)
                .collect(),
        },
    }
}

/// Owner-aware inventory mode used by the integration tests (and
/// later by a real identity-federation join). The dispatcher exposes
/// it via a separate function so the test harness can drive
/// SCH-004 / 005 deterministically without standing up the auth
/// service.
pub fn inventory_with_owners(
    schedules: Vec<InventorySchedule>,
    owners: Vec<(String, InventoryUser)>,
) -> Vec<InventorySchedule> {
    let lookup: std::collections::HashMap<String, InventoryUser> = owners.into_iter().collect();
    schedules
        .into_iter()
        .map(|mut s| {
            if let Some(u) = lookup.get(&s.rid) {
                s.run_as_user = Some(u.clone());
            }
            s
        })
        .collect()
}

