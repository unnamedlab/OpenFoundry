use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    ActionRuleKind, CreateActionRuleRequest, CreatePrimaryRequest, CreateSecondaryRequest,
    PrimaryItem, SecondaryItem,
};

pub async fn list_items(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, PrimaryItem>(
        "SELECT * FROM monitoring_rules ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_item(
    State(state): State<AppState>,
    Json(body): Json<CreatePrimaryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, PrimaryItem>(
        "INSERT INTO monitoring_rules (id, payload) VALUES ($1, $2) RETURNING *",
    )
    .bind(id)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_item(State(state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    match sqlx::query_as::<_, PrimaryItem>("SELECT * FROM monitoring_rules WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, SecondaryItem>(
        "SELECT * FROM monitoring_subscribers WHERE parent_id = $1 ORDER BY created_at DESC LIMIT 200",
    )
    .bind(parent_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
    Json(body): Json<CreateSecondaryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, SecondaryItem>(
        "INSERT INTO monitoring_subscribers (id, parent_id, payload) VALUES ($1, $2, $3) RETURNING *",
    )
    .bind(id)
    .bind(parent_id)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

/// `POST /rules` — TASK F. Accepts a typed [`CreateActionRuleRequest`] and
/// persists it as a JSONB row in `monitoring_rules`. The full rule engine
/// will be wired in a follow-up task; this handler nails down the contract
/// so `ontology-actions-service` and the UI agree on the rule shape.
pub async fn create_action_rule(
    State(state): State<AppState>,
    Json(body): Json<CreateActionRuleRequest>,
) -> impl IntoResponse {
    if let Err(error) = body.validate() {
        return (StatusCode::BAD_REQUEST, error).into_response();
    }
    let payload = serialize_rule(&body);
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, PrimaryItem>(
        "INSERT INTO monitoring_rules (id, payload) VALUES ($1, $2) RETURNING *",
    )
    .bind(id)
    .bind(&payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

fn serialize_rule(req: &CreateActionRuleRequest) -> serde_json::Value {
    let kind = match req.kind {
        ActionRuleKind::ActionDurationP95 => "action_duration_p95",
        ActionRuleKind::ActionFailuresInWindow => "action_failures_in_window",
    };
    serde_json::json!({
        "kind": kind,
        "action_id": req.action_id,
        "window": req.window,
        "threshold_ms": req.threshold_ms,
        "threshold_count": req.threshold_count,
        "failure_type": req.failure_type,
        "severity": req.severity,
    })
}
