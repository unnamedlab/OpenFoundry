//! Service-local audit recorder for virtual-table mutations.
//!
//! Each call writes a row into `virtual_table_audit` and emits a
//! `tracing::info!(target = "audit", action = …)` event so the
//! `audit-compliance-service` collector picks it up under the same
//! contract as the request-level Tower middleware.
//!
//! The shared `audit-trail` lib's `AuditEvent` enum is closed over the
//! media-set vocabulary (libs/audit-trail/src/events.rs:103). Adding
//! virtual-table variants to that enum is intentionally deferred to
//! a follow-up task — for P1 we use a structured tracing event so the
//! SIEM can already filter by `kind="virtual_table.*"` and we do not
//! couple the closed-enum migration to this binary's first cut.

use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

/// Persist a virtual-table audit row and mirror it to the audit
/// tracing target.
pub async fn record(
    pool: &PgPool,
    source_rid: Option<&str>,
    virtual_table_id: Option<Uuid>,
    action: &str,
    actor_id: Option<&str>,
    details: Value,
) {
    if let Err(error) = sqlx::query(
        "INSERT INTO virtual_table_audit
            (virtual_table_id, source_rid, action, actor_id, details)
            VALUES ($1, $2, $3, $4, $5::jsonb)",
    )
    .bind(virtual_table_id)
    .bind(source_rid)
    .bind(action)
    .bind(actor_id)
    .bind(&details)
    .execute(pool)
    .await
    {
        // We never want a failed audit insert to break the user
        // request — log loudly and let the request succeed.
        tracing::warn!(
            target: "audit",
            ?error,
            action,
            "failed to persist virtual_table_audit row"
        );
    }

    tracing::info!(
        target: "audit",
        kind = action,
        source_rid = source_rid.unwrap_or(""),
        virtual_table_id = virtual_table_id.map(|id| id.to_string()).unwrap_or_default(),
        actor_id = actor_id.unwrap_or(""),
        details = %details,
        "virtual_table audit event"
    );
}
