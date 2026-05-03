//! HTTP handlers for the streaming-monitor surface (Bloque P4).
//!
//! Exposes:
//!   * `GET/POST /v1/monitoring-views`
//!   * `GET /v1/monitoring-views/{id}`
//!   * `GET/POST /v1/monitoring-views/{id}/rules`
//!   * `PATCH/DELETE /v1/monitor-rules/{id}`
//!   * `GET  /v1/monitor-rules/{id}/evaluations?limit=N`
//!   * `GET  /v1/monitor-rules?resource_type=..&resource_rid=..` —
//!     used by the streaming-service Job Details tab to render the
//!     "Active monitors" card.

use auth_middleware::claims::Claims;
use axum::{
    Extension, Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Deserialize;
use uuid::Uuid;

use crate::AppState;
use crate::streaming_monitors::{
    Comparator, CreateMonitorRuleRequest, CreateMonitoringViewRequest, MonitorEvaluation,
    MonitorKind, MonitorRule, MonitorRuleRow, MonitoringView, ResourceType, Severity,
};

const PERM_MONITOR_WRITE: &str = "monitoring:write";

fn can_write(claims: &Claims) -> bool {
    claims.has_any_role(&["admin", "monitoring_admin", "data_engineer"])
        || claims.has_permission_key(PERM_MONITOR_WRITE)
}

fn db_err<E: std::fmt::Display>(err: E) -> (StatusCode, String) {
    tracing::error!(error = %err, "monitor handler db error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        "database operation failed".to_string(),
    )
}

// ---------------------------------------------------------------------
// Monitoring views
// ---------------------------------------------------------------------

pub async fn list_views(State(state): State<AppState>) -> impl IntoResponse {
    let rows: Vec<MonitoringView> = match sqlx::query_as::<_, MonitoringView>(
        "SELECT id, name, description, project_rid, created_by, created_at, updated_at
           FROM monitoring_views
          ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => rows,
        Err(e) => return db_err(e).into_response(),
    };
    Json(serde_json::json!({ "data": rows })).into_response()
}

pub async fn create_view(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Json(body): Json<CreateMonitoringViewRequest>,
) -> impl IntoResponse {
    if !can_write(&claims) {
        return (StatusCode::FORBIDDEN, "monitoring:write required").into_response();
    }
    if body.name.trim().is_empty() || body.project_rid.trim().is_empty() {
        return (StatusCode::BAD_REQUEST, "name and project_rid required").into_response();
    }
    let id = Uuid::now_v7();
    let res = sqlx::query_as::<_, MonitoringView>(
        "INSERT INTO monitoring_views (id, name, description, project_rid, created_by)
         VALUES ($1, $2, $3, $4, $5)
         RETURNING id, name, description, project_rid, created_by, created_at, updated_at",
    )
    .bind(id)
    .bind(body.name.trim())
    .bind(body.description.unwrap_or_default())
    .bind(body.project_rid.trim())
    .bind(claims.sub.to_string())
    .fetch_one(&state.db)
    .await;
    match res {
        Ok(view) => {
            tracing::info!(
                target: "audit",
                event = "monitoring.view.created",
                actor.sub = %claims.sub,
                resource.id = %view.id,
                "monitor audit"
            );
            (StatusCode::CREATED, Json(view)).into_response()
        }
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_view(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, MonitoringView>(
        "SELECT id, name, description, project_rid, created_by, created_at, updated_at
           FROM monitoring_views WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(v)) => Json(v).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "view not found").into_response(),
        Err(e) => db_err(e).into_response(),
    }
}

// ---------------------------------------------------------------------
// Monitor rules
// ---------------------------------------------------------------------

pub async fn list_rules_for_view(
    State(state): State<AppState>,
    Path(view_id): Path<Uuid>,
) -> impl IntoResponse {
    let rows: Vec<MonitorRule> = match sqlx::query_as::<_, MonitorRuleRow>(
        "SELECT id, view_id, name, resource_type, resource_rid, monitor_kind,
                window_seconds, comparator, threshold, severity, enabled,
                created_by, created_at, updated_at
           FROM monitor_rules
          WHERE view_id = $1
          ORDER BY created_at DESC",
    )
    .bind(view_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => rows.into_iter().map(MonitorRule::from).collect(),
        Err(e) => return db_err(e).into_response(),
    };
    Json(serde_json::json!({ "data": rows })).into_response()
}

pub async fn create_rule(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Json(body): Json<CreateMonitorRuleRequest>,
) -> impl IntoResponse {
    if !can_write(&claims) {
        return (StatusCode::FORBIDDEN, "monitoring:write required").into_response();
    }
    if let Err(err) = body.validate() {
        return (StatusCode::BAD_REQUEST, err).into_response();
    }
    let id = Uuid::now_v7();
    let res = sqlx::query_as::<_, MonitorRuleRow>(
        "INSERT INTO monitor_rules (
             id, view_id, name, resource_type, resource_rid, monitor_kind,
             window_seconds, comparator, threshold, severity, created_by
         ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
         RETURNING id, view_id, name, resource_type, resource_rid, monitor_kind,
                   window_seconds, comparator, threshold, severity, enabled,
                   created_by, created_at, updated_at",
    )
    .bind(id)
    .bind(body.view_id)
    .bind(body.name.unwrap_or_default())
    .bind(body.resource_type.as_str())
    .bind(&body.resource_rid)
    .bind(body.monitor_kind.as_str())
    .bind(body.window_seconds)
    .bind(body.comparator.as_str())
    .bind(body.threshold)
    .bind(body.severity.as_str())
    .bind(claims.sub.to_string())
    .fetch_one(&state.db)
    .await;
    match res {
        Ok(row) => {
            let rule = MonitorRule::from(row);
            tracing::info!(
                target: "audit",
                event = "monitoring.rule.created",
                actor.sub = %claims.sub,
                resource.id = %rule.id,
                resource_rid = %rule.resource_rid,
                monitor_kind = rule.monitor_kind.as_str(),
                "monitor audit"
            );
            (StatusCode::CREATED, Json(rule)).into_response()
        }
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

#[derive(Debug, Default, Deserialize)]
pub struct PatchRuleBody {
    pub name: Option<String>,
    pub window_seconds: Option<i32>,
    pub comparator: Option<Comparator>,
    pub threshold: Option<f64>,
    pub severity: Option<Severity>,
    pub enabled: Option<bool>,
}

pub async fn patch_rule(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
    Json(body): Json<PatchRuleBody>,
) -> impl IntoResponse {
    if !can_write(&claims) {
        return (StatusCode::FORBIDDEN, "monitoring:write required").into_response();
    }
    let existing = match sqlx::query_as::<_, MonitorRuleRow>(
        "SELECT id, view_id, name, resource_type, resource_rid, monitor_kind,
                window_seconds, comparator, threshold, severity, enabled,
                created_by, created_at, updated_at
           FROM monitor_rules WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(r)) => r,
        Ok(None) => return (StatusCode::NOT_FOUND, "rule not found").into_response(),
        Err(e) => return db_err(e).into_response(),
    };

    let updated = sqlx::query_as::<_, MonitorRuleRow>(
        "UPDATE monitor_rules SET
            name           = $2,
            window_seconds = $3,
            comparator     = $4,
            threshold      = $5,
            severity       = $6,
            enabled        = $7,
            updated_at     = now()
          WHERE id = $1
          RETURNING id, view_id, name, resource_type, resource_rid, monitor_kind,
                    window_seconds, comparator, threshold, severity, enabled,
                    created_by, created_at, updated_at",
    )
    .bind(id)
    .bind(body.name.unwrap_or(existing.name))
    .bind(body.window_seconds.unwrap_or(existing.window_seconds))
    .bind(
        body.comparator
            .map(|c| c.as_str().to_string())
            .unwrap_or(existing.comparator),
    )
    .bind(body.threshold.unwrap_or(existing.threshold))
    .bind(
        body.severity
            .map(|s| s.as_str().to_string())
            .unwrap_or(existing.severity),
    )
    .bind(body.enabled.unwrap_or(existing.enabled))
    .fetch_one(&state.db)
    .await;

    match updated {
        Ok(row) => {
            tracing::info!(
                target: "audit",
                event = "monitoring.rule.updated",
                actor.sub = %claims.sub,
                resource.id = %id,
                "monitor audit"
            );
            Json(MonitorRule::from(row)).into_response()
        }
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn delete_rule(
    State(state): State<AppState>,
    Extension(claims): Extension<Claims>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    if !can_write(&claims) {
        return (StatusCode::FORBIDDEN, "monitoring:write required").into_response();
    }
    match sqlx::query("DELETE FROM monitor_rules WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await
    {
        Ok(res) if res.rows_affected() == 0 => {
            (StatusCode::NOT_FOUND, "rule not found").into_response()
        }
        Ok(_) => {
            tracing::info!(
                target: "audit",
                event = "monitoring.rule.deleted",
                actor.sub = %claims.sub,
                resource.id = %id,
                "monitor audit"
            );
            (StatusCode::NO_CONTENT, "").into_response()
        }
        Err(e) => db_err(e).into_response(),
    }
}

#[derive(Debug, Default, Deserialize)]
pub struct ListEvaluationsQuery {
    #[serde(default = "default_eval_limit")]
    pub limit: i64,
}
fn default_eval_limit() -> i64 {
    50
}

pub async fn list_evaluations(
    State(state): State<AppState>,
    Path(rule_id): Path<Uuid>,
    Query(q): Query<ListEvaluationsQuery>,
) -> impl IntoResponse {
    let limit = q.limit.clamp(1, 1000);
    let rows: Vec<MonitorEvaluation> = match sqlx::query_as::<_, MonitorEvaluation>(
        "SELECT id, rule_id, evaluated_at, observed_value, fired, alert_id
           FROM monitor_evaluations
          WHERE rule_id = $1
          ORDER BY evaluated_at DESC
          LIMIT $2",
    )
    .bind(rule_id)
    .bind(limit)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => rows,
        Err(e) => return db_err(e).into_response(),
    };
    Json(serde_json::json!({ "data": rows })).into_response()
}

#[derive(Debug, Default, Deserialize)]
pub struct ListRulesQuery {
    pub resource_type: Option<ResourceType>,
    pub resource_rid: Option<String>,
    pub monitor_kind: Option<MonitorKind>,
}

pub async fn list_rules(
    State(state): State<AppState>,
    Query(q): Query<ListRulesQuery>,
) -> impl IntoResponse {
    let mut sql = "SELECT id, view_id, name, resource_type, resource_rid, monitor_kind,
                          window_seconds, comparator, threshold, severity, enabled,
                          created_by, created_at, updated_at
                     FROM monitor_rules WHERE 1=1"
        .to_string();
    let mut binds: Vec<String> = Vec::new();
    if let Some(rt) = q.resource_type {
        sql.push_str(&format!(" AND resource_type = ${}", binds.len() + 1));
        binds.push(rt.as_str().to_string());
    }
    if let Some(rid) = q.resource_rid.as_deref() {
        sql.push_str(&format!(" AND resource_rid = ${}", binds.len() + 1));
        binds.push(rid.to_string());
    }
    if let Some(mk) = q.monitor_kind {
        sql.push_str(&format!(" AND monitor_kind = ${}", binds.len() + 1));
        binds.push(mk.as_str().to_string());
    }
    sql.push_str(" ORDER BY created_at DESC");
    let mut query = sqlx::query_as::<_, MonitorRuleRow>(&sql);
    for b in &binds {
        query = query.bind(b);
    }
    let rows = match query.fetch_all(&state.db).await {
        Ok(rows) => rows.into_iter().map(MonitorRule::from).collect::<Vec<_>>(),
        Err(e) => return db_err(e).into_response(),
    };
    Json(serde_json::json!({ "data": rows })).into_response()
}
