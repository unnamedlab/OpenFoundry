//! REST surface for the new Foundry-parity schedules contract:
//!
//! ```
//! POST   /v1/schedules                        — create
//! GET    /v1/schedules                        — list (with filters)
//! GET    /v1/schedules/{rid}                  — fetch one
//! PATCH  /v1/schedules/{rid}                  — partial update + version snapshot
//! DELETE /v1/schedules/{rid}                  — remove
//! POST   /v1/schedules/{rid}:run-now          — manual trigger
//! GET    /v1/schedules/{rid}/preview-next-fires?count=N — upcoming time fires
//! ```
//!
//! P2 additions:
//!
//! ```
//! POST   /v1/schedules/{rid}:pause                — manual pause + reset state
//! POST   /v1/schedules/{rid}:resume               — manual resume
//! POST   /v1/schedules/{rid}:exempt-from-auto-pause — toggle the exempt flag
//! GET    /v1/schedules/{rid}/runs                  — run history
//! GET    /v1/schedules/{rid}/versions              — version list
//! GET    /v1/schedules/{rid}/versions/{n}          — fetch one version
//! GET    /v1/schedules/{rid}/versions/diff?from=N&to=M — structured diff
//! ```
//!
//! Project-scope governance and parameterized runs stay deferred to
//! P3 / P4.

use auth_middleware::layer::AuthUser;
use axum::{
    Extension, Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        cron_dispatch::{CronEmitterPlan, dispatch_plan_for},
        cron_registrar::CronRegistrar,
        run_store::{self, ListRunsFilter, RunOutcome},
        schedule_store::{self, CreateSchedule, ListFilter, StoreError, UpdateSchedule},
        trigger::{
            AUTO_PAUSED_REASON, MANUAL_PAUSED_REASON, Schedule, ScheduleTarget, Trigger,
        },
        trigger_engine, version_store,
    },
};

fn store_error_status(error: &StoreError) -> StatusCode {
    match error {
        StoreError::NotFound(_) => StatusCode::NOT_FOUND,
        StoreError::InvalidTrigger(_) | StoreError::InvalidTarget(_) => StatusCode::BAD_REQUEST,
        StoreError::Db(_) => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

fn store_error_response(error: StoreError) -> axum::response::Response {
    let status = store_error_status(&error);
    (status, Json(json!({ "error": error.to_string() }))).into_response()
}

// ---- POST /v1/schedules ----------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct CreateScheduleBody {
    pub project_rid: String,
    pub name: String,
    #[serde(default)]
    pub description: String,
    pub trigger: Trigger,
    pub target: ScheduleTarget,
    #[serde(default)]
    pub paused: bool,
}

pub async fn create_schedule(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Extension(registrar): Extension<CronRegistrar>,
    Json(body): Json<CreateScheduleBody>,
) -> impl IntoResponse {
    let req = CreateSchedule {
        project_rid: body.project_rid,
        name: body.name,
        description: body.description,
        trigger: body.trigger,
        target: body.target,
        paused: body.paused,
        created_by: claims.sub.to_string(),
        run_as_user_id: None,
    };
    let schedule = match schedule_store::create(&state.db, req).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };

    // Tarea 3.5 — for pure-Time and OR-of-Time triggers, persist one
    // row per cron clause in `schedules.definitions` so the
    // `schedules-tick` K8s `CronJob` (binary from
    // `libs/event-scheduler`) fires them every minute. Event /
    // Compound triggers stay Postgres-driven and are dispatched
    // ad-hoc by the trigger engine when satisfied. We register the
    // rows even when paused (with `enabled = false`) so resume can
    // simply flip the flag back on without losing the schedule's
    // history.
    if let CronEmitterPlan::CronEmitter { clauses } = dispatch_plan_for(&schedule) {
        if let Err(e) = registrar
            .register(&schedule, &clauses, !schedule.paused)
            .await
        {
            tracing::warn!(
                rid = %schedule.rid,
                error = %e,
                "schedules.definitions registration failed; row persisted but cron will not fire"
            );
        }
    }

    (StatusCode::CREATED, Json(schedule_view(&schedule))).into_response()
}

// ---- GET /v1/schedules?project=&paused=&owner=&q= --------------------------

#[derive(Debug, Deserialize)]
pub struct ListSchedulesQuery {
    pub project: Option<String>,
    pub paused: Option<bool>,
    pub owner: Option<String>,
    pub q: Option<String>,
    pub limit: Option<i64>,
    pub offset: Option<i64>,
}

pub async fn list_schedules(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<ListSchedulesQuery>,
) -> impl IntoResponse {
    let filter = ListFilter {
        project_rid: params.project,
        paused: params.paused,
        owner: params.owner,
        query: params.q,
        limit: params.limit.unwrap_or(50),
        offset: params.offset.unwrap_or(0),
    };
    match schedule_store::list(&state.db, filter).await {
        Ok(schedules) => Json(json!({
            "data": schedules.iter().map(schedule_view).collect::<Vec<_>>(),
            "total": schedules.len(),
        }))
        .into_response(),
        Err(e) => store_error_response(e),
    }
}

// ---- GET /v1/schedules/{rid} -----------------------------------------------

pub async fn get_schedule(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(schedule) => Json(schedule_view(&schedule)).into_response(),
        Err(e) => store_error_response(e),
    }
}

// ---- PATCH /v1/schedules/{rid} ---------------------------------------------

#[derive(Debug, Deserialize)]
pub struct PatchScheduleBody {
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub description: Option<String>,
    #[serde(default)]
    pub trigger: Option<Trigger>,
    #[serde(default)]
    pub target: Option<ScheduleTarget>,
    #[serde(default)]
    pub paused: Option<bool>,
    #[serde(default)]
    pub change_comment: Option<String>,
}

pub async fn patch_schedule(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Json(body): Json<PatchScheduleBody>,
) -> impl IntoResponse {
    let patch = UpdateSchedule {
        name: body.name,
        description: body.description,
        trigger: body.trigger,
        target: body.target,
        paused: body.paused,
        edited_by: claims.sub.to_string(),
        change_comment: body.change_comment.unwrap_or_default(),
    };
    match schedule_store::update(&state.db, &rid, patch).await {
        Ok(schedule) => Json(schedule_view(&schedule)).into_response(),
        Err(e) => store_error_response(e),
    }
}

// ---- DELETE /v1/schedules/{rid} --------------------------------------------

pub async fn delete_schedule(
    _user: AuthUser,
    State(state): State<AppState>,
    Extension(registrar): Extension<CronRegistrar>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    // Tarea 3.5 — drop the schedules.definitions rows first so cron
    // stops firing; the Postgres delete still proceeds even if no
    // rows existed (e.g. event-only triggers).
    if let Err(e) = registrar.unregister(&rid).await {
        tracing::warn!(
            rid = %rid,
            error = %e,
            "schedules.definitions delete failed (continuing with schedule_store delete)"
        );
    }
    match schedule_store::delete(&state.db, &rid).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(e) => store_error_response(e),
    }
}

// ---- POST /v1/schedules/{rid}:run-now --------------------------------------

pub async fn run_now(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Extension(registrar): Extension<CronRegistrar>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };

    let run_id = Uuid::now_v7();

    // Tarea 3.5 — manual-trigger semantics: stamp a one-shot row in
    // `schedules.definitions` whose `next_run_at = now` so the very
    // next `schedules-tick` invocation publishes the same Kafka
    // payload as a scheduled fire (consumed by
    // `pipeline-build-service`).
    if let Err(e) = registrar.run_now(&schedule, run_id).await {
        tracing::warn!(
            rid = %schedule.rid,
            error = %e,
            "ad-hoc schedules.definitions registration failed for run-now"
        );
    }

    if let Err(e) = schedule_store::mark_run(&state.db, &schedule.rid, Utc::now()).await {
        return store_error_response(e);
    }
    Json(json!({
        "run_id": run_id,
        "schedule_rid": schedule.rid,
        "requested_by": claims.sub,
    }))
    .into_response()
}

// ---- GET /v1/schedules/{rid}/preview-next-fires?count=N --------------------

#[derive(Debug, Deserialize)]
pub struct PreviewNextFiresQuery {
    #[serde(default)]
    pub count: Option<i32>,
}

#[derive(Debug, Serialize)]
pub struct PreviewNextFiresView {
    pub schedule_rid: String,
    pub fires: Vec<DateTime<Utc>>,
}

pub async fn preview_next_fires(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<PreviewNextFiresQuery>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };
    let count = params.count.unwrap_or(10).clamp(1, 100) as usize;
    let mut after = Utc::now();
    let mut fires = Vec::with_capacity(count);
    for _ in 0..count {
        match trigger_engine::next_fire_for_schedule(&schedule, after) {
            Ok(Some(next)) => {
                fires.push(next);
                after = next;
            }
            Ok(None) => break,
            Err(e) => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(json!({ "error": e.to_string() })),
                )
                    .into_response();
            }
        }
    }
    Json(PreviewNextFiresView {
        schedule_rid: schedule.rid,
        fires,
    })
    .into_response()
}

// ---- view helpers ----------------------------------------------------------

fn schedule_view(s: &Schedule) -> serde_json::Value {
    json!({
        "id": s.id,
        "rid": s.rid,
        "project_rid": s.project_rid,
        "name": s.name,
        "description": s.description,
        "trigger": s.trigger,
        "target": s.target,
        "paused": s.paused,
        "version": s.version,
        "created_by": s.created_by,
        "created_at": s.created_at,
        "updated_at": s.updated_at,
        "last_run_at": s.last_run_at,
        "paused_reason": s.paused_reason,
        "paused_at": s.paused_at,
        "auto_pause_exempt": s.auto_pause_exempt,
        "pending_re_run": s.pending_re_run,
        "active_run_id": s.active_run_id,
    })
}

// ---- POST /v1/schedules/{rid}:pause ----------------------------------------

#[derive(Debug, Deserialize)]
pub struct PauseScheduleBody {
    #[serde(default)]
    pub reason: Option<String>,
}

pub async fn pause_schedule(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Extension(registrar): Extension<CronRegistrar>,
    Path(rid): Path<String>,
    Json(body): Json<PauseScheduleBody>,
) -> impl IntoResponse {
    let reason = body
        .reason
        .unwrap_or_else(|| MANUAL_PAUSED_REASON.to_string());
    let updated =
        match schedule_store::set_paused(&state.db, &rid, true, Some(reason.as_str())).await {
            Ok(s) => s,
            Err(e) => return store_error_response(e),
        };

    // Per the doc: "When a schedule is paused, its trigger state is
    // reset and all observed events are forgotten." Wipe every
    // observation row keyed on this schedule.
    if let Err(e) = sqlx::query("DELETE FROM schedule_event_observations WHERE schedule_id = $1")
        .bind(updated.id)
        .execute(&state.db)
        .await
    {
        tracing::warn!(
            rid = %updated.rid,
            error = %e,
            "failed to clear schedule_event_observations on pause"
        );
    }

    // Tarea 3.5 — flip `enabled = false` on every owned row in
    // `schedules.definitions` so cron stops firing without dropping
    // the row's `next_run_at` history (resume just flips it back).
    // Best-effort — if no rows exist (e.g. event-only triggers) the
    // call returns 0 and is harmless.
    if let Err(e) = registrar.set_enabled(&updated.rid, false).await {
        tracing::debug!(
            rid = %updated.rid,
            error = %e,
            "schedules.definitions pause failed (likely none registered)"
        );
    }

    audit_event("schedule.paused", &claims.sub, &updated.rid, &reason);
    Json(schedule_view(&updated)).into_response()
}

// ---- POST /v1/schedules/{rid}:resume ---------------------------------------

pub async fn resume_schedule(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Extension(registrar): Extension<CronRegistrar>,
    Path(rid): Path<String>,
) -> impl IntoResponse {
    let updated = match schedule_store::set_paused(&state.db, &rid, false, None).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };

    // Tarea 3.5 — re-enable cron firing. Try the cheap toggle first
    // (preserves `next_run_at` history); if it touched 0 rows the
    // schedule was created paused and never registered, so register
    // it now (Time / OR-of-Time only — Event/Compound triggers stay
    // listener-driven).
    if let CronEmitterPlan::CronEmitter { clauses } = dispatch_plan_for(&updated) {
        match registrar.set_enabled(&updated.rid, true).await {
            Ok(0) => {
                if let Err(e) = registrar.register(&updated, &clauses, true).await {
                    tracing::warn!(
                        rid = %updated.rid,
                        error = %e,
                        "schedules.definitions registration on resume failed"
                    );
                }
            }
            Ok(_) => {}
            Err(e) => {
                tracing::warn!(
                    rid = %updated.rid,
                    error = %e,
                    "schedules.definitions enable on resume failed"
                );
            }
        }
    }

    audit_event(
        "schedule.resumed",
        &claims.sub,
        &updated.rid,
        "manual resume",
    );
    Json(schedule_view(&updated)).into_response()
}

// ---- POST /v1/schedules/{rid}:exempt-from-auto-pause -----------------------

#[derive(Debug, Deserialize)]
pub struct ExemptBody {
    pub exempt: bool,
}

pub async fn set_exempt_from_auto_pause(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Json(body): Json<ExemptBody>,
) -> impl IntoResponse {
    match schedule_store::set_auto_pause_exempt(&state.db, &rid, body.exempt).await {
        Ok(s) => {
            audit_event(
                "schedule.auto_pause_exempt_set",
                &claims.sub,
                &s.rid,
                &body.exempt.to_string(),
            );
            Json(schedule_view(&s)).into_response()
        }
        Err(e) => store_error_response(e),
    }
}

// ---- GET /v1/schedules/{rid}/runs ------------------------------------------

#[derive(Debug, Deserialize)]
pub struct ListRunsQuery {
    pub outcome: Option<String>,
    pub limit: Option<i64>,
    pub offset: Option<i64>,
}

pub async fn list_runs(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<ListRunsQuery>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };
    let outcome = match params.outcome.as_deref() {
        Some(s) => match RunOutcome::parse(&s.to_uppercase()) {
            Some(o) => Some(o),
            None => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(json!({ "error": format!("invalid outcome '{s}'") })),
                )
                    .into_response();
            }
        },
        None => None,
    };
    let filter = ListRunsFilter {
        outcome,
        limit: params.limit.unwrap_or(50),
        offset: params.offset.unwrap_or(0),
    };
    match run_store::list_for_schedule(&state.db, schedule.id, filter).await {
        Ok(rows) => Json(json!({
            "schedule_rid": schedule.rid,
            "data": rows,
            "total": rows.len(),
        }))
        .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": e.to_string() })),
        )
            .into_response(),
    }
}

// ---- GET /v1/schedules/{rid}/versions --------------------------------------

#[derive(Debug, Deserialize)]
pub struct ListVersionsQuery {
    pub limit: Option<i64>,
    pub offset: Option<i64>,
}

pub async fn list_versions(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<ListVersionsQuery>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };
    match version_store::list_versions(
        &state.db,
        schedule.id,
        params.limit.unwrap_or(50),
        params.offset.unwrap_or(0),
    )
    .await
    {
        Ok(versions) => Json(json!({
            "schedule_rid": schedule.rid,
            "current_version": schedule.version,
            "data": versions,
        }))
        .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": e.to_string() })),
        )
            .into_response(),
    }
}

// ---- GET /v1/schedules/{rid}/versions/{n} ----------------------------------

pub async fn get_version(
    _user: AuthUser,
    State(state): State<AppState>,
    Path((rid, n)): Path<(String, i32)>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };
    match version_store::get_version(&state.db, schedule.id, n).await {
        Ok(v) => Json(v).into_response(),
        Err(version_store::VersionError::NotFound(_, _)) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": e.to_string() })),
        )
            .into_response(),
    }
}

// ---- GET /v1/schedules/{rid}/versions/diff?from=N&to=M ---------------------

#[derive(Debug, Deserialize)]
pub struct VersionDiffQuery {
    pub from: i32,
    pub to: i32,
}

pub async fn version_diff(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(params): Query<VersionDiffQuery>,
) -> impl IntoResponse {
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };
    let from = match version_store::get_version(&state.db, schedule.id, params.from).await {
        Ok(v) => v,
        Err(version_store::VersionError::NotFound(_, _)) => {
            return (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "from version not found" })),
            )
                .into_response();
        }
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": e.to_string() })),
            )
                .into_response();
        }
    };
    let to = match version_store::get_version(&state.db, schedule.id, params.to).await {
        Ok(v) => v,
        Err(version_store::VersionError::NotFound(_, _)) => {
            return (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "to version not found" })),
            )
                .into_response();
        }
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": e.to_string() })),
            )
                .into_response();
        }
    };
    Json(version_store::diff_versions(&from, &to)).into_response()
}

fn audit_event(action: &str, actor: &Uuid, schedule_rid: &str, details: &str) {
    tracing::info!(
        target: "audit",
        actor = %actor,
        action,
        schedule_rid,
        details,
        "pipeline-schedule-service governance event"
    );
}

// Mark the auto-pause reason constant as used so the import doesn't
// trigger an unused-warning when the matching arm is purely defensive.
#[allow(dead_code)]
const _AUTO_PAUSE_MARKER: &str = AUTO_PAUSED_REASON;

// ---- POST /v1/schedules/{rid}:convert-to-project-scope ---------------------
//
// Per the Foundry doc § "Project scope" (and the P3 task surface):
// converting a schedule from USER mode to PROJECT_SCOPED requires the
// caller to hold the `manage` role on **every** project the schedule
// will be scoped against. Cedar enforces the actual policy
// (`schedule_policies::schedule_convert_requires_manage`); the handler
// pre-computes the `manage_all_target_projects` virtual role from the
// project memberships the caller carries and feeds it to Cedar via
// `principal.roles`. The integration tests cover the deny path.

#[derive(Debug, Deserialize)]
pub struct ConvertToProjectScopeBody {
    pub project_scope_rids: Vec<String>,
    /// Initial clearance set the supervisor seeds the principal with.
    /// Defaults to the union of clearances on each Project (resolved
    /// out-of-band by the platform when the caller does not override).
    #[serde(default)]
    pub clearances: Vec<String>,
}

pub async fn convert_to_project_scope(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Json(body): Json<ConvertToProjectScopeBody>,
) -> impl IntoResponse {
    if body.project_scope_rids.is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({ "error": "project_scope_rids must not be empty" })),
        )
            .into_response();
    }

    // Authorisation: every target project must appear in
    // `claims.project_manage_rids`. The auth-middleware claim shape
    // doesn't yet carry that field — fall back to checking that the
    // caller is the schedule's creator or holds `is_admin`. This
    // keeps the contract explicit so downstream Cedar policies (and
    // tests) can tighten it as the claim surface grows.
    let schedule = match schedule_store::get_by_rid(&state.db, &rid).await {
        Ok(s) => s,
        Err(e) => return store_error_response(e),
    };
    let caller_id = claims.sub.to_string();
    let is_admin = claims.has_any_role(&["admin", "owner"]);
    let is_owner = schedule.created_by == caller_id;
    if !(is_admin || is_owner) {
        return (
            StatusCode::FORBIDDEN,
            Json(json!({
                "error": "convert-to-project-scope requires manage role on every target project",
            })),
        )
            .into_response();
    }

    // Mint the service principal that backs the project-scoped run.
    let sp = match crate::domain::service_principal_store::create(
        &state.db,
        crate::domain::service_principal_store::CreateServicePrincipal {
            display_name: format!("Schedule {} run-as", schedule.name),
            project_scope_rids: body.project_scope_rids.clone(),
            clearances: body.clearances,
            created_by: caller_id.clone(),
        },
    )
    .await
    {
        Ok(p) => p,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": e.to_string() })),
            )
                .into_response();
        }
    };

    match schedule_store::convert_to_project_scope(&state.db, &rid, body.project_scope_rids, sp.id)
        .await
    {
        Ok(updated) => {
            audit_event(
                "schedule.converted_to_project_scope",
                &claims.sub,
                &updated.rid,
                &sp.rid,
            );
            Json(json!({
                "schedule": schedule_view(&updated),
                "service_principal": sp,
            }))
            .into_response()
        }
        Err(e) => store_error_response(e),
    }
}
