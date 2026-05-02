//! T9.2 / T9.3 — RBAC + audit emission for dataset-versioning
//! mutations. Mirrors `data-asset-catalog-service::security`; kept as a
//! peer module rather than shared so the two services can evolve their
//! audit payloads independently and the cross-crate dependency surface
//! stays tiny.
//!
//! Required scope is `dataset.write` (admin role bypasses). The
//! `operation` label fed to [`DATASET_RBAC_DENIALS_TOTAL`] mirrors the
//! audit `action` field so dashboards can join them 1-to-1.

use std::fmt::Display;

use auth_middleware::claims::Claims;
use axum::{Json, http::StatusCode};
use serde_json::{Value, json};

use crate::metrics::DATASET_RBAC_DENIALS_TOTAL;

pub const SCOPE_DATASET_WRITE: &str = "dataset.write";

/// Vertical RBAC gate. `operation` is recorded as the metric label and
/// the audit action so failed attempts are visible in both surfaces.
pub fn require_dataset_write(
    claims: &Claims,
    dataset_rid: &str,
    operation: &'static str,
) -> Result<(), (StatusCode, Json<Value>)> {
    if claims.has_role(auth_middleware::rbac::roles::ADMIN) {
        return Ok(());
    }
    if claims.has_permission_key(SCOPE_DATASET_WRITE) || claims.has_permission("dataset", "write") {
        return Ok(());
    }
    DATASET_RBAC_DENIALS_TOTAL
        .with_label_values(&[operation])
        .inc();
    tracing::warn!(
        target: "audit",
        action = format!("{operation}.denied").as_str(),
        actor = %claims.sub,
        dataset_rid = dataset_rid,
        reason = "missing scope dataset.write",
        "RBAC denied dataset mutation"
    );
    Err((
        StatusCode::FORBIDDEN,
        Json(json!({
            "error": "forbidden",
            "required_scope": SCOPE_DATASET_WRITE,
            "operation": operation,
            "dataset_rid": dataset_rid,
        })),
    ))
}

/// Synchronous structured audit event. Matches the convention used by
/// `lineage-deletion-service` (target = "audit", action = "…").
pub fn emit_audit<A: Display>(actor: A, action: &str, dataset_rid: &str, extra: Value) {
    tracing::info!(
        target: "audit",
        actor = %actor,
        action = action,
        dataset_rid = dataset_rid,
        details = %extra,
        "dataset versioning mutation"
    );
}
