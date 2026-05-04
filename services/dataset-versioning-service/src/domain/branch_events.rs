//! Branch lifecycle events emitted via the transactional outbox.
//!
//! Topic: `foundry.branch.events.v1`. Schema is JSON (Avro contract
//! lives in Apicurio; the wire representation here is the canonical
//! JSON envelope). Every event carries the same envelope so consumers
//! can route by `event_type` without parsing the payload.
//!
//! ```json
//! {
//!   "event_type": "dataset.branch.created.v1",
//!   "event_id":   "f3b1…",
//!   "occurred_at": "2026-05-04T12:00:00Z",
//!   "actor":      "user:42",
//!   "branch_rid":  "ri.foundry.main.branch.…",
//!   "dataset_rid": "ri.foundry.main.dataset.…",
//!   "parent_rid":  "ri.foundry.main.branch.…" | null,
//!   "is_root":     false,
//!   "head_transaction_rid": "ri.foundry.main.transaction.…" | null,
//!   "fallback_chain": ["develop", "master"],
//!   "labels": {"persona": "data-eng"},
//!   "markings": ["pii"],
//!   "extras": { … per-event extras … }
//! }
//! ```
//!
//! `extras` is event-type specific:
//!   * `.reparented.v1`  → `{ from_parent_rid, to_parent_rid }`
//!   * `.deleted.v1`     → `{ reparented_children: […] }`
//!   * `.archived.v1`    → `{ reason: "ttl"|"manual" }`
//!   * `.restored.v1`    → `{ archived_at }`
//!   * `.markings.updated.v1` → `{ added: [...], removed: [...] }`
//!   * `.retention.updated.v1` → `{ policy, ttl_days }`

use std::collections::HashMap;

use chrono::{DateTime, Utc};
use outbox::{OutboxEvent, OutboxResult, enqueue};
use serde::Serialize;
use serde_json::{Value, json};
use sqlx::{Postgres, Transaction};
use uuid::Uuid;

pub const TOPIC: &str = "foundry.branch.events.v1";

/// Every branch-lifecycle event uses one of these `event_type` strings.
/// Kept as `&'static str` so callers cannot forge ad-hoc names.
pub const EVT_CREATED: &str = "dataset.branch.created.v1";
pub const EVT_REPARENTED: &str = "dataset.branch.reparented.v1";
pub const EVT_DELETED: &str = "dataset.branch.deleted.v1";
pub const EVT_ARCHIVED: &str = "dataset.branch.archived.v1";
pub const EVT_RESTORED: &str = "dataset.branch.restored.v1";
pub const EVT_MARKINGS_UPDATED: &str = "dataset.branch.markings.updated.v1";
pub const EVT_RETENTION_UPDATED: &str = "dataset.branch.retention.updated.v1";

#[derive(Debug, Clone, Serialize)]
pub struct BranchEnvelope {
    pub event_type: &'static str,
    pub event_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub actor: String,
    pub branch_rid: String,
    pub dataset_rid: String,
    pub parent_rid: Option<String>,
    pub is_root: bool,
    pub head_transaction_rid: Option<String>,
    #[serde(default)]
    pub fallback_chain: Vec<String>,
    #[serde(default)]
    pub labels: HashMap<String, String>,
    #[serde(default)]
    pub markings: Vec<Uuid>,
    pub extras: Value,
}

impl BranchEnvelope {
    pub fn new(event_type: &'static str, branch_rid: &str, dataset_rid: &str, actor: &str) -> Self {
        Self {
            event_type,
            event_id: Uuid::now_v7(),
            occurred_at: Utc::now(),
            actor: actor.to_string(),
            branch_rid: branch_rid.to_string(),
            dataset_rid: dataset_rid.to_string(),
            parent_rid: None,
            // `is_root` mirrors `parent_rid IS NULL`. The default
            // constructor leaves the parent unset, so the envelope is
            // born as a root event; `with_parent_rid(Some(...))`
            // flips this back to false.
            is_root: true,
            head_transaction_rid: None,
            fallback_chain: Vec::new(),
            labels: HashMap::new(),
            markings: Vec::new(),
            extras: json!({}),
        }
    }

    pub fn with_parent_rid(mut self, parent_rid: Option<String>) -> Self {
        self.is_root = parent_rid.is_none();
        self.parent_rid = parent_rid;
        self
    }

    pub fn with_head(mut self, head_transaction_rid: Option<String>) -> Self {
        self.head_transaction_rid = head_transaction_rid;
        self
    }

    pub fn with_fallback(mut self, fallback_chain: Vec<String>) -> Self {
        self.fallback_chain = fallback_chain;
        self
    }

    pub fn with_labels(mut self, labels: HashMap<String, String>) -> Self {
        self.labels = labels;
        self
    }

    pub fn with_markings(mut self, markings: Vec<Uuid>) -> Self {
        self.markings = markings;
        self
    }

    pub fn with_extras(mut self, extras: Value) -> Self {
        self.extras = extras;
        self
    }

    pub fn into_payload(&self) -> Value {
        serde_json::to_value(self).unwrap_or_else(|_| json!({}))
    }
}

/// Enqueue a branch event onto the outbox inside the caller's
/// transaction. Caller commits.
pub async fn emit(
    tx: &mut Transaction<'_, Postgres>,
    envelope: &BranchEnvelope,
) -> OutboxResult<()> {
    let event = OutboxEvent::new(
        envelope.event_id,
        "dataset_branch",
        envelope.branch_rid.clone(),
        TOPIC,
        envelope.into_payload(),
    )
    .with_header("event_type", envelope.event_type)
    .with_header("dataset_rid", envelope.dataset_rid.clone())
    .with_header("actor", envelope.actor.clone());
    enqueue(tx, event).await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn envelope_serialises_with_required_fields() {
        let env = BranchEnvelope::new(
            EVT_CREATED,
            "ri.foundry.main.branch.x",
            "ri.foundry.main.dataset.y",
            "user:1",
        )
        .with_parent_rid(Some("ri.foundry.main.branch.parent".into()))
        .with_extras(json!({"source_kind":"child_from_branch"}));
        let payload = env.into_payload();
        assert_eq!(payload["event_type"], "dataset.branch.created.v1");
        assert_eq!(payload["dataset_rid"], "ri.foundry.main.dataset.y");
        assert_eq!(payload["parent_rid"], "ri.foundry.main.branch.parent");
        assert_eq!(payload["is_root"], false);
        assert_eq!(payload["extras"]["source_kind"], "child_from_branch");
    }

    #[test]
    fn missing_parent_marks_event_as_root() {
        let env = BranchEnvelope::new(EVT_CREATED, "br", "ds", "system");
        let payload = env.into_payload();
        assert_eq!(payload["is_root"], true);
        assert!(payload["parent_rid"].is_null());
    }
}
