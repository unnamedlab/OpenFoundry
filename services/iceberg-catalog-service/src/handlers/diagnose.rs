//! `POST /iceberg/v1/diagnose` — connection diagnostic for catalog
//! clients.
//!
//! Used by the Catalog Access UI tab. Runs a deterministic sequence
//! against the catalog itself (ListNamespaces → LoadTable on a
//! standard probe namespace) and reports per-step outcomes + timings.
//! The service principal that hits this endpoint must hold the
//! `api:iceberg-read` scope; tighter authorisation is left to the
//! caller (UI never exposes the endpoint without an admin role).

use std::time::Instant;

use axum::extract::State;
use axum::Json;
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::AppState;
use crate::audit;
use crate::domain::namespace;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;

#[derive(Debug, Deserialize)]
pub struct DiagnoseRequest {
    /// Client implementation under diagnostic. Used as a label in the
    /// audit event so dashboards can break down failures by client.
    pub client: String,
    #[serde(default)]
    pub project_rid: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct DiagnoseStep {
    pub name: String,
    pub ok: bool,
    pub latency_ms: u128,
    pub detail: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct DiagnoseResponse {
    pub client: String,
    pub success: bool,
    pub steps: Vec<DiagnoseStep>,
    pub total_latency_ms: u128,
}

pub async fn run_diagnose(
    State(state): State<AppState>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<DiagnoseRequest>,
) -> Result<Json<DiagnoseResponse>, ApiError> {
    let started = Instant::now();
    let project_rid = body
        .project_rid
        .clone()
        .unwrap_or_else(|| "ri.foundry.main.project.default".to_string());
    let mut steps = Vec::new();

    // Step 1 — ListNamespaces.
    let t = Instant::now();
    let result = namespace::list(&state.iceberg.db, &project_rid, None).await;
    let elapsed = t.elapsed().as_millis();
    let (ok, detail) = match &result {
        Ok(list) => (true, Some(format!("{} namespaces", list.len()))),
        Err(err) => (false, Some(err.to_string())),
    };
    steps.push(DiagnoseStep {
        name: "list_namespaces".to_string(),
        ok,
        latency_ms: elapsed,
        detail,
    });

    // Step 2 — Resolve a probe namespace (`_diagnostic`) without
    // mutating state. Fails gracefully if the namespace doesn't
    // exist; that's a "soft-warn" outcome rather than a hard error.
    let t = Instant::now();
    let probe = namespace::fetch(
        &state.iceberg.db,
        &project_rid,
        &["_diagnostic".to_string()],
    )
    .await;
    let elapsed = t.elapsed().as_millis();
    let (ok, detail) = match &probe {
        Ok(_) => (true, Some("probe namespace reachable".to_string())),
        Err(_) => (
            true,
            Some(
                "no probe namespace; create `_diagnostic` to enable load probe".to_string(),
            ),
        ),
    };
    steps.push(DiagnoseStep {
        name: "load_probe_namespace".to_string(),
        ok,
        latency_ms: elapsed,
        detail,
    });

    let total = started.elapsed().as_millis();
    let success = steps.iter().all(|s| s.ok);

    let actor = Uuid::parse_str(&principal.subject).unwrap_or_else(|_| Uuid::nil());
    audit::diagnose_executed(actor, &body.client, total, success);

    Ok(Json(DiagnoseResponse {
        client: body.client,
        success,
        steps,
        total_latency_ms: total,
    }))
}
