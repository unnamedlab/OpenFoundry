//! T9.2 / T9.3 — Vertical RBAC + audit emission for catalog mutations.
//!
//! Two pieces, kept together because every mutation site uses both:
//!
//!   * [`require_dataset_write`] — the synchronous policy gate. Today
//!     it is enforced in-process via [`Claims::has_permission`]
//!     (`dataset.write`); the same shape will accept an HTTP round-trip
//!     to `authorization-policy-service` once the policy bundle for
//!     dataset operations lands there. The signature already takes the
//!     dataset RID so a future remote check has the resource context.
//!
//!   * [`emit_audit`] — synchronous audit emission. Mirrors the
//!     `tracing::info!(target = "audit", …)` convention used by
//!     `lineage-deletion-service::audit_emitter`, which the
//!     audit-compliance collector subscribes to.
//!
//! The intent is that **every** mutation handler reaches both helpers
//! before returning success: the guard short-circuits unauthorised
//! callers with a 403 and the audit call records the action for the
//! compliance pipeline. Read-only handlers skip both.

use std::fmt::Display;

use auth_middleware::claims::Claims;
use axum::{Json, http::StatusCode};
use serde_json::{Value, json};

/// Permission key required for any mutation that writes to a dataset.
/// Matches the convention shared by the rest of the platform — see
/// `auth_middleware::rbac` and the dataset write scopes documented in
/// the platform security guide.
pub const SCOPE_DATASET_WRITE: &str = "dataset.write";

/// Permission key required to administer dataset retention / markings.
pub const SCOPE_DATASET_ADMIN: &str = "dataset.admin";

/// Vertical RBAC gate for any dataset-mutating endpoint.
///
/// Returns `Err((403, …))` if the caller does not hold
/// `dataset.write`. The `dataset_rid` is unused today but threaded
/// through so a future call into `authorization-policy-service` can
/// scope the decision (project membership, branch ACLs, etc.).
pub fn require_dataset_write(
    claims: &Claims,
    dataset_rid: &str,
) -> Result<(), (StatusCode, Json<Value>)> {
    // Admin role bypasses scope checks (consistent with the rest of
    // the platform's auth model).
    if claims.has_role(auth_middleware::rbac::roles::ADMIN) {
        return Ok(());
    }
    if claims.has_permission_key(SCOPE_DATASET_WRITE) || claims.has_permission("dataset", "write") {
        return Ok(());
    }
    crate::metrics::DATASET_MARKING_ENFORCEMENT_DENIALS_TOTAL.inc();
    tracing::warn!(
        target: "audit",
        action = "dataset.write.denied",
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
            "dataset_rid": dataset_rid,
        })),
    ))
}

/// Same gate as [`require_dataset_write`] but for retention / marking
/// administration. Kept distinct so an operator with `dataset.write`
/// cannot silently mutate marking attachments.
pub fn require_dataset_admin(
    claims: &Claims,
    dataset_rid: &str,
) -> Result<(), (StatusCode, Json<Value>)> {
    if claims.has_role(auth_middleware::rbac::roles::ADMIN) {
        return Ok(());
    }
    if claims.has_permission_key(SCOPE_DATASET_ADMIN) || claims.has_permission("dataset", "admin") {
        return Ok(());
    }
    crate::metrics::DATASET_MARKING_ENFORCEMENT_DENIALS_TOTAL.inc();
    tracing::warn!(
        target: "audit",
        action = "dataset.admin.denied",
        actor = %claims.sub,
        dataset_rid = dataset_rid,
        reason = "missing scope dataset.admin",
        "RBAC denied dataset admin operation"
    );
    Err((
        StatusCode::FORBIDDEN,
        Json(json!({
            "error": "forbidden",
            "required_scope": SCOPE_DATASET_ADMIN,
            "dataset_rid": dataset_rid,
        })),
    ))
}

/// Synchronous audit emission. Drops one structured tracing event at
/// `info` level under the `audit` target; the audit-compliance
/// collector translates these into rows in the audit warehouse.
///
/// `extra` is splatted into the event as a single JSON object so the
/// collector keeps a stable schema regardless of which handler called
/// in.
pub fn emit_audit<A: Display>(actor: A, action: &str, dataset_rid: &str, extra: Value) {
    tracing::info!(
        target: "audit",
        actor = %actor,
        action = action,
        dataset_rid = dataset_rid,
        details = %extra,
        "dataset mutation"
    );
}
