//! Wire-format payloads for the four `approval.*.v1` Kafka topics
//! the FASE 7 / Tarea 7.3 runtime publishes / consumes.
//!
//! Locked-in shapes: every field is documented and round-tripped by
//! a unit test below so a downstream consumer can rely on the wire
//! contract without reading runtime source.
//!
//! ## Topic catalog
//!
//! | Topic                       | Payload                          | Direction |
//! | --------------------------- | -------------------------------- | --------- |
//! | `approval.requested.v1`     | [`ApprovalRequestedV1`]          | Outbound  |
//! | `approval.completed.v1`     | [`ApprovalCompletedV1`]          | Outbound  |
//! | `approval.expired.v1`       | [`ApprovalExpiredV1`]            | Outbound  |
//! | `approval.decided.v1`       | [`ApprovalDecidedV1`]            | Inbound   |
//!
//! Plus deterministic-id helpers for the per-row idempotency the
//! HTTP handler and the timeout sweep both rely on.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

/// Hard-coded UUIDv5 namespace for everything emitted by this
/// service. Generated once with `uuidgen` and pinned forever.
pub const APPROVALS_NAMESPACE: Uuid = Uuid::from_bytes([
    0xc4, 0x18, 0x73, 0x59, 0x96, 0xc7, 0x4c, 0x84, 0xa6, 0x3d, 0x42, 0xab, 0x91, 0x6e, 0x36, 0xa3,
]);

// ─────────────────────────── Outbound payloads ──────────────────────────

/// Payload of `approval.requested.v1`. Emitted once when the HTTP
/// handler accepts a new approval.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ApprovalRequestedV1 {
    pub approval_id: Uuid,
    pub tenant_id: String,
    pub subject: String,
    pub approver_set: Vec<String>,
    /// Free-form JSON the caller passed alongside the approval —
    /// used by downstream consumers (UI, audit) to render context.
    /// Mirrors the Temporal input's `action_payload` field.
    #[serde(default)]
    pub action_payload: Value,
    /// End-to-end correlation id propagated from the inbound HTTP
    /// request span. Same UUID flows on every subsequent
    /// `approval.*.v1` event header for this approval.
    pub correlation_id: Uuid,
    /// Free-form actor identifier — user UUID, service principal,
    /// or sentinel like `system`.
    pub triggered_by: String,
    /// Wall-clock deadline. `None` means "no deadline" — the
    /// timeout sweep CronJob skips these rows.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<DateTime<Utc>>,
}

/// Payload of `approval.completed.v1`. Emitted on every terminal
/// `pending → approved / rejected` transition.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ApprovalCompletedV1 {
    pub approval_id: Uuid,
    pub tenant_id: String,
    pub correlation_id: Uuid,
    /// `approved` or `rejected`. The `expired` outcome lives on its
    /// own topic (see [`ApprovalExpiredV1`]).
    pub decision: String,
    pub decided_by: String,
    /// Optional decision comment. `None` when the decider chose
    /// not to leave one.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub comment: Option<String>,
    pub decided_at: DateTime<Utc>,
}

/// Payload of `approval.expired.v1`. Emitted by the timeout sweep
/// CronJob (Tarea 7.4) on every `pending → expired` transition.
/// Kept on its own topic so SLO alerts can fire on the expired
/// feed without filtering [`ApprovalCompletedV1`].
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ApprovalExpiredV1 {
    pub approval_id: Uuid,
    pub tenant_id: String,
    pub correlation_id: Uuid,
    /// The deadline the row exceeded. Stamped by the sweep so
    /// downstream SLO consumers can compute "how late were we?".
    pub expired_at: DateTime<Utc>,
    /// The original `expires_at` for forensic comparison.
    pub deadline: DateTime<Utc>,
}

// ─────────────────────────── Inbound payloads ───────────────────────────

/// Payload of `approval.decided.v1`. Future inbound topic for
/// "manager decided externally" — out-of-band decision events from
/// other systems. Out of scope for FASE 7 strict; the wire-type is
/// locked in here so the topic + payload shape are stable when a
/// producer eventually wires in.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ApprovalDecidedV1 {
    pub approval_id: Uuid,
    pub tenant_id: String,
    pub correlation_id: Uuid,
    /// `approved` | `rejected`.
    pub decision: String,
    pub decided_by: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub comment: Option<String>,
}

// ─────────────────────────── Derivation helpers ─────────────────────────

/// Derive a deterministic `event_id` for outbox events bound to
/// `(approval_id, kind)`. Re-publishing the same id collapses via
/// the outbox's `ON CONFLICT DO NOTHING`.
pub fn derive_outbox_event_id(approval_id: Uuid, kind: &str) -> Uuid {
    let mut buf = Vec::with_capacity(17 + kind.len());
    buf.extend_from_slice(approval_id.as_bytes());
    buf.push(b'|');
    buf.extend_from_slice(kind.as_bytes());
    Uuid::new_v5(&APPROVALS_NAMESPACE, &buf)
}

/// Derive the per-decision `event_id` for the
/// `audit_compliance.processed_events` idempotency store. Reserved
/// for the future inbound `approval.decided.v1` consumer.
pub fn derive_decided_event_id(approval_id: Uuid, decided_by: &str, decision: &str) -> Uuid {
    let mut buf = Vec::with_capacity(17 + decided_by.len() + 1 + decision.len());
    buf.extend_from_slice(approval_id.as_bytes());
    buf.push(b'|');
    buf.extend_from_slice(decided_by.as_bytes());
    buf.push(b'|');
    buf.extend_from_slice(decision.as_bytes());
    Uuid::new_v5(&APPROVALS_NAMESPACE, &buf)
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn fake_id() -> Uuid {
        Uuid::nil()
    }

    #[test]
    fn requested_v1_round_trip_with_all_fields() {
        let now = Utc::now();
        let event = ApprovalRequestedV1 {
            approval_id: fake_id(),
            tenant_id: "acme".into(),
            subject: "Promote customer".into(),
            approver_set: vec!["alice".into()],
            action_payload: json!({"customer_id": "c-1"}),
            correlation_id: fake_id(),
            triggered_by: "user-7".into(),
            expires_at: Some(now),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: ApprovalRequestedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn requested_v1_omits_expires_at_when_absent() {
        let event = ApprovalRequestedV1 {
            approval_id: fake_id(),
            tenant_id: "acme".into(),
            subject: "x".into(),
            approver_set: vec![],
            action_payload: Value::Null,
            correlation_id: fake_id(),
            triggered_by: "system".into(),
            expires_at: None,
        };
        let s = serde_json::to_string(&event).unwrap();
        assert!(!s.contains("expires_at"));
    }

    #[test]
    fn completed_v1_round_trip_omits_comment_when_absent() {
        let now = Utc::now();
        let event = ApprovalCompletedV1 {
            approval_id: fake_id(),
            tenant_id: "acme".into(),
            correlation_id: fake_id(),
            decision: "approved".into(),
            decided_by: "alice".into(),
            comment: None,
            decided_at: now,
        };
        let s = serde_json::to_string(&event).unwrap();
        assert!(!s.contains("comment"));
        let back: ApprovalCompletedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn expired_v1_round_trip() {
        let now = Utc::now();
        let event = ApprovalExpiredV1 {
            approval_id: fake_id(),
            tenant_id: "acme".into(),
            correlation_id: fake_id(),
            expired_at: now,
            deadline: now,
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: ApprovalExpiredV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn decided_v1_round_trip() {
        let event = ApprovalDecidedV1 {
            approval_id: fake_id(),
            tenant_id: "acme".into(),
            correlation_id: fake_id(),
            decision: "rejected".into(),
            decided_by: "bob".into(),
            comment: Some("stop".into()),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: ApprovalDecidedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn outbox_event_id_is_deterministic_v5() {
        let approval_id = Uuid::now_v7();
        let a = derive_outbox_event_id(approval_id, "requested");
        let b = derive_outbox_event_id(approval_id, "requested");
        assert_eq!(a, b);
        assert_eq!(a.get_version_num(), 5);
        assert_ne!(a, derive_outbox_event_id(approval_id, "completed"));
    }

    #[test]
    fn decided_event_id_distinguishes_inputs() {
        let approval_id = Uuid::now_v7();
        let a = derive_decided_event_id(approval_id, "alice", "approved");
        let b = derive_decided_event_id(approval_id, "alice", "approved");
        let c = derive_decided_event_id(approval_id, "alice", "rejected");
        let d = derive_decided_event_id(approval_id, "bob", "approved");
        assert_eq!(a, b);
        assert_ne!(a, c);
        assert_ne!(a, d);
        assert_eq!(a.get_version_num(), 5);
    }
}
