//! HTTP surface for the global-branching plane.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
};
use outbox::{OutboxEvent, enqueue};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::router::AppState;

use super::model::{
    CreateGlobalBranchLinkRequest, CreateGlobalBranchRequest, GlobalBranch, GlobalBranchLink,
    GlobalBranchSummary, PromoteResponse,
};
use super::store;

/// Topic used by `:promote` events. Kept distinct from the per-plane
/// `foundry.branch.events.v1` so consumers can subscribe selectively.
pub const PROMOTE_TOPIC: &str = "foundry.global.branch.promote.requested.v1";

pub async fn create_global_branch(
    State(state): State<AppState>,
    Json(request): Json<CreateGlobalBranchRequest>,
) -> Result<(StatusCode, Json<GlobalBranch>), (StatusCode, Json<Value>)> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    let row = store::create_branch(&state.db, &request, &state.actor)
        .await
        .map_err(|e| match e {
            sqlx::Error::Database(db) if db.is_unique_violation() => (
                StatusCode::CONFLICT,
                Json(json!({ "error": "name already in use", "name": request.name })),
            ),
            other => internal(other),
        })?;
    audit(
        "global_branch.created",
        &state.actor,
        &row.rid,
        json!({"name": row.name}),
    );
    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn list_global_branches(
    State(state): State<AppState>,
) -> Result<Json<Vec<GlobalBranch>>, (StatusCode, Json<Value>)> {
    store::list_branches(&state.db)
        .await
        .map(Json)
        .map_err(internal)
}

pub async fn get_global_branch(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> Result<Json<GlobalBranchSummary>, (StatusCode, Json<Value>)> {
    let branch = store::get_branch(&state.db, id)
        .await
        .map_err(internal)?
        .ok_or_else(|| not_found("global branch not found"))?;
    let summary = store::summary(&state.db, branch).await.map_err(internal)?;
    Ok(Json(summary))
}

pub async fn add_link(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(request): Json<CreateGlobalBranchLinkRequest>,
) -> Result<(StatusCode, Json<GlobalBranchLink>), (StatusCode, Json<Value>)> {
    if request.resource_type.trim().is_empty()
        || request.resource_rid.trim().is_empty()
        || request.branch_rid.trim().is_empty()
    {
        return Err(bad_request(
            "resource_type, resource_rid and branch_rid are required",
        ));
    }
    // Make sure the global branch exists so we never insert orphan
    // links pointing at a non-existent FK target.
    if store::get_branch(&state.db, id)
        .await
        .map_err(internal)?
        .is_none()
    {
        return Err(not_found("global branch not found"));
    }
    let link = store::add_link(&state.db, id, &request)
        .await
        .map_err(internal)?;
    audit(
        "global_branch.link.added",
        &state.actor,
        &id.to_string(),
        json!({
            "resource_type": request.resource_type,
            "resource_rid": request.resource_rid,
            "branch_rid": request.branch_rid,
        }),
    );
    Ok((StatusCode::CREATED, Json(link)))
}

pub async fn list_resources(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> Result<Json<Vec<GlobalBranchLink>>, (StatusCode, Json<Value>)> {
    store::list_links(&state.db, id)
        .await
        .map(Json)
        .map_err(internal)
}

pub async fn promote(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> Result<Json<PromoteResponse>, (StatusCode, Json<Value>)> {
    let branch = store::get_branch(&state.db, id)
        .await
        .map_err(internal)?
        .ok_or_else(|| not_found("global branch not found"))?;
    let payload = store::promote_payload(branch.id, &branch.name, &state.actor);

    let event_id = Uuid::now_v7();
    let mut tx = state.db.begin().await.map_err(internal)?;
    let event = OutboxEvent::new(
        event_id,
        "global_branch",
        branch.id.to_string(),
        PROMOTE_TOPIC,
        payload,
    )
    .with_header("event_type", "global.branch.promote.requested.v1")
    .with_header("global_branch_rid", branch.rid.clone());
    enqueue(&mut tx, event)
        .await
        .map_err(|e| internal(e.to_string()))?;
    tx.commit().await.map_err(internal)?;

    audit(
        "global_branch.promote.requested",
        &state.actor,
        &branch.rid,
        json!({ "event_id": event_id }),
    );
    Ok(Json(PromoteResponse {
        event_id,
        global_branch_id: branch.id,
        topic: PROMOTE_TOPIC.to_string(),
    }))
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
}

fn not_found(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::NOT_FOUND, Json(json!({ "error": msg })))
}

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "global-branch-service handler error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}

fn audit(action: &str, actor: &str, target: &str, details: Value) {
    tracing::info!(
        target: "audit",
        actor = actor,
        action = action,
        target = target,
        details = %details,
        "global-branch-service audit",
    );
}
