//! Iceberg catalog audit emissions.
//!
//! The closing-task spec § 10 lists the event vocabulary. We log to the
//! `audit` tracing target so the existing `audit-compliance-service`
//! pipeline drains them in the same way it drains other services'
//! events. The structured fields below are preserved verbatim by the
//! Foundry audit collector.

use serde_json::Value;
use uuid::Uuid;

pub fn namespace_created(actor: Uuid, project_rid: &str, namespace: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.namespace.created",
        actor = %actor,
        project_rid = %project_rid,
        namespace = %namespace,
        "iceberg namespace created"
    );
}

pub fn namespace_deleted(actor: Uuid, project_rid: &str, namespace: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.namespace.deleted",
        actor = %actor,
        project_rid = %project_rid,
        namespace = %namespace,
        "iceberg namespace deleted"
    );
}

pub fn table_created(actor: Uuid, table_rid: &str, namespace: &str, name: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.table.created",
        actor = %actor,
        table_rid = %table_rid,
        namespace = %namespace,
        name = %name,
        "iceberg table created"
    );
}

pub fn table_dropped(actor: Uuid, table_rid: &str, purge: bool) {
    tracing::info!(
        target: "audit",
        event = "iceberg.table.dropped",
        actor = %actor,
        table_rid = %table_rid,
        purge = purge,
        "iceberg table dropped"
    );
}

pub fn table_metadata_updated(
    actor: Uuid,
    table_rid: &str,
    metadata_location: &str,
    diff: &Value,
) {
    tracing::info!(
        target: "audit",
        event = "iceberg.table.metadata_updated",
        actor = %actor,
        table_rid = %table_rid,
        metadata_location = %metadata_location,
        diff = %diff,
        "iceberg table metadata updated"
    );
}

pub fn oauth_token_issued(actor: Option<Uuid>, grant_type: &str, scope: &str) {
    let actor_str = actor.map(|a| a.to_string()).unwrap_or_default();
    tracing::info!(
        target: "audit",
        event = "iceberg.oauth_token.issued",
        actor = %actor_str,
        grant_type = %grant_type,
        scope = %scope,
        "iceberg oauth token issued"
    );
}

pub fn api_token_created(actor: Uuid, token_id: Uuid, scopes: &[String]) {
    tracing::info!(
        target: "audit",
        event = "iceberg.api_token.created",
        actor = %actor,
        token_id = %token_id,
        scopes = ?scopes,
        "iceberg api token created"
    );
}

// ─── P2 Foundry-transaction events ────────────────────────────────────

pub fn transaction_begin(actor: Uuid, build_rid: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.transaction.begin",
        actor = %actor,
        build_rid = %build_rid,
        "foundry iceberg transaction begin"
    );
}

pub fn transaction_commit(actor: Uuid, build_rid: &str, table_count: usize) {
    tracing::info!(
        target: "audit",
        event = "iceberg.transaction.commit",
        actor = %actor,
        build_rid = %build_rid,
        table_count = table_count,
        "foundry iceberg transaction commit"
    );
}

pub fn transaction_abort(actor: Uuid, build_rid: &str, reason: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.transaction.abort",
        actor = %actor,
        build_rid = %build_rid,
        reason = %reason,
        "foundry iceberg transaction abort"
    );
}

pub fn transaction_conflict(
    actor: Uuid,
    build_rid: &str,
    table_rid: &str,
    conflicting_with: &str,
) {
    tracing::info!(
        target: "audit",
        event = "iceberg.transaction.conflict",
        actor = %actor,
        build_rid = %build_rid,
        table_rid = %table_rid,
        conflicting_with = %conflicting_with,
        "foundry iceberg transaction conflict"
    );
}

pub fn transaction_retry(actor: Uuid, build_rid: &str, table_rid: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.transaction.retry",
        actor = %actor,
        build_rid = %build_rid,
        table_rid = %table_rid,
        "foundry iceberg transaction retry signal"
    );
}

pub fn schema_altered(
    actor: Uuid,
    table_rid: &str,
    previous_schema_id: i64,
    new_schema_id: i64,
) {
    tracing::info!(
        target: "audit",
        event = "iceberg.schema.altered",
        actor = %actor,
        table_rid = %table_rid,
        previous_schema_id = previous_schema_id,
        new_schema_id = new_schema_id,
        "iceberg schema explicitly altered"
    );
}

pub fn schema_attempt_blocked(actor: Uuid, table_rid: &str, diff: &str) {
    tracing::warn!(
        target: "audit",
        event = "iceberg.schema.attempt_blocked_by_strict_mode",
        actor = %actor,
        table_rid = %table_rid,
        diff = %diff,
        "iceberg schema mutation blocked by strict-mode (call alter-schema first)"
    );
}

pub fn branch_alias_applied(actor: Option<Uuid>, requested: &str, resolved: &str) {
    let actor_str = actor.map(|a| a.to_string()).unwrap_or_default();
    tracing::info!(
        target: "audit",
        event = "iceberg.branch_alias.applied",
        actor = %actor_str,
        requested = %requested,
        resolved = %resolved,
        "iceberg branch alias applied"
    );
}

// ─── P3 markings + denial + diagnose events ────────────────────────────

pub fn markings_updated(actor: Uuid, target_rid: &str, scope: &str, before: &[String], after: &[String]) {
    tracing::info!(
        target: "audit",
        event = "iceberg.markings.updated",
        actor = %actor,
        target_rid = %target_rid,
        scope = %scope,
        before = ?before,
        after = ?after,
        "iceberg markings updated"
    );
}

pub fn markings_override_created(actor: Uuid, table_rid: &str, marking: &str) {
    tracing::info!(
        target: "audit",
        event = "iceberg.markings.override_created",
        actor = %actor,
        table_rid = %table_rid,
        marking = %marking,
        "iceberg markings override created"
    );
}

pub fn markings_inheritance_snapshot(actor: Uuid, table_rid: &str, namespace_rid: &str, markings: &[String]) {
    tracing::info!(
        target: "audit",
        event = "iceberg.markings.inheritance_snapshot",
        actor = %actor,
        table_rid = %table_rid,
        namespace_rid = %namespace_rid,
        markings = ?markings,
        "iceberg markings inherited at table creation"
    );
}

pub fn access_denied(actor: Uuid, target_rid: &str, attempted_action: &str, reason: &str) {
    tracing::warn!(
        target: "audit",
        event = "iceberg.access.denied",
        actor = %actor,
        target_rid = %target_rid,
        attempted_action = %attempted_action,
        reason = %reason,
        "iceberg access denied"
    );
}

pub fn diagnose_executed(actor: Uuid, client_kind: &str, latency_ms: u128, success: bool) {
    tracing::info!(
        target: "audit",
        event = "iceberg.diagnose.executed",
        actor = %actor,
        client_kind = %client_kind,
        latency_ms = latency_ms,
        success = success,
        "iceberg diagnose executed"
    );
}
