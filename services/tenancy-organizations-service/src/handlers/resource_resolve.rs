//! Cross-resource label resolver.
//!
//! `POST /workspace/resources/resolve` accepts a batch of `(kind, id)`
//! pairs and returns a single map of human-friendly labels. The
//! frontend uses this so the workspace surface (favorites, recents,
//! trash, search results) doesn't fall back to `kind · id-prefix`
//! placeholders for every non-ontology row.
//!
//! In Phase 1 this service can resolve resources whose authority lives
//! in databases it has direct access to:
//!
//! - `ontology_project`     → `ontology_projects.display_name`
//! - `ontology_folder`      → `ontology_project_folders.name`
//!
//! For other kinds (datasets, pipelines, notebooks, …) the response
//! reports `resolved: false` so the caller keeps using its placeholder.
//! Adding HTTP clients to fan out to those services is intentionally
//! deferred — the contract is shaped to absorb that later without
//! breaking callers.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::State,
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use uuid::Uuid;

use crate::AppState;

use super::workspace::{ResourceKind, db_err};

#[derive(Debug, Deserialize)]
pub struct ResolveRequestEntry {
    pub resource_kind: String,
    pub resource_id: Uuid,
}

#[derive(Debug, Deserialize)]
pub struct ResolveRequest {
    pub items: Vec<ResolveRequestEntry>,
}

#[derive(Debug, Serialize)]
pub struct ResolvedLabel {
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub resolved: bool,
    pub label: Option<String>,
    pub description: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ResolveResponse {
    pub data: Vec<ResolvedLabel>,
}

/// Hard cap so a misbehaving caller can't issue an unbounded batch.
const MAX_BATCH: usize = 200;

pub async fn resolve_resources(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<ResolveRequest>,
) -> Response {
    if body.items.is_empty() {
        return Json(ResolveResponse { data: vec![] }).into_response();
    }
    if body.items.len() > MAX_BATCH {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({ "error": format!("at most {MAX_BATCH} items per request") })),
        )
            .into_response();
    }

    // Bucket request entries by kind so we can fire one query per
    // database table instead of one per item.
    let mut project_ids: Vec<Uuid> = Vec::new();
    let mut folder_ids: Vec<Uuid> = Vec::new();
    // Preserve original (kind, id) ordering so the response mirrors the
    // request. Unsupported entries are emitted as `resolved: false`.
    let mut order: Vec<(String, Uuid)> = Vec::with_capacity(body.items.len());

    for entry in &body.items {
        order.push((entry.resource_kind.clone(), entry.resource_id));
        match ResourceKind::parse(&entry.resource_kind) {
            Ok(ResourceKind::OntologyProject) => project_ids.push(entry.resource_id),
            Ok(ResourceKind::OntologyFolder) => folder_ids.push(entry.resource_id),
            // Other kinds are intentionally skipped; we report
            // `resolved: false` for them below so the frontend keeps
            // its placeholder.
            _ => {}
        }
    }

    // -- ontology_projects -------------------------------------------------
    let mut project_labels: std::collections::HashMap<Uuid, (String, Option<String>)> =
        std::collections::HashMap::new();
    if !project_ids.is_empty() {
        let rows = match sqlx::query_as::<_, (Uuid, String, Option<String>)>(
            r#"SELECT id, COALESCE(NULLIF(display_name, ''), slug) AS label, description
               FROM ontology_projects
               WHERE id = ANY($1)"#,
        )
        .bind(&project_ids)
        .fetch_all(&state.ontology_db)
        .await
        {
            Ok(rows) => rows,
            Err(error) => return db_err("resolve.projects", error),
        };
        for (id, label, description) in rows {
            project_labels.insert(id, (label, description));
        }
    }

    // -- ontology_project_folders -----------------------------------------
    let mut folder_labels: std::collections::HashMap<Uuid, (String, Option<String>)> =
        std::collections::HashMap::new();
    if !folder_ids.is_empty() {
        let rows = match sqlx::query_as::<_, (Uuid, String, Option<String>)>(
            r#"SELECT id, name, description
               FROM ontology_project_folders
               WHERE id = ANY($1)"#,
        )
        .bind(&folder_ids)
        .fetch_all(&state.ontology_db)
        .await
        {
            Ok(rows) => rows,
            Err(error) => return db_err("resolve.folders", error),
        };
        for (id, label, description) in rows {
            folder_labels.insert(id, (label, description));
        }
    }

    let data: Vec<ResolvedLabel> = order
        .into_iter()
        .map(|(kind, id)| {
            let (label, description) = match ResourceKind::parse(&kind) {
                Ok(ResourceKind::OntologyProject) => project_labels
                    .get(&id)
                    .cloned()
                    .map(|(l, d)| (Some(l), d))
                    .unwrap_or((None, None)),
                Ok(ResourceKind::OntologyFolder) => folder_labels
                    .get(&id)
                    .cloned()
                    .map(|(l, d)| (Some(l), d))
                    .unwrap_or((None, None)),
                _ => (None, None),
            };
            ResolvedLabel {
                resource_kind: kind,
                resource_id: id,
                resolved: label.is_some(),
                label,
                description,
            }
        })
        .collect();

    Json(ResolveResponse { data }).into_response()
}
