//! Wire format of the Kafka events the FASE 5 / Tarea 5.3 runtime
//! consumes (`automate.condition.v1`) and produces
//! (`automate.outcome.v1`), plus the deterministic UUIDv5 helpers
//! that make producer redeliveries idempotent.
//!
//! ## Idempotency derivation
//!
//! `derive_run_id(definition_id, correlation_id)` is the canonical
//! aggregate id for an [`crate::domain::automation_run::AutomationRun`].
//! The HTTP handler INSERTs the row at this id atomically with the
//! outbox publish, so a producer redelivery (retry, replay) collapses
//! onto the same `automation_runs` row instead of starting a new run.
//!
//! `derive_condition_event_id(definition_id, correlation_id)` is the
//! `event_id` used by the Postgres idempotency store
//! (`workflow_automation.processed_events`). Mirrors `derive_run_id`
//! one-to-one — a redelivery of the same condition collapses both at
//! the row level (PK conflict on `automation_runs.id`) and at the
//! consumer level (PK conflict on `processed_events.event_id`).
//!
//! Both ids are UUIDv5 (SHA-1 namespaced UUIDs, RFC 4122) so they
//! depend purely on inputs. The namespace is hard-coded as a
//! `Uuid::from_bytes` constant (generated once with `uuidgen`) so a
//! future namespace migration is a single-line change.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

/// UUIDv5 namespace for everything emitted by `workflow-automation-
/// service`. Pinned forever — generated with `uuidgen` and never
/// rotated.
pub const WORKFLOW_AUTOMATION_NAMESPACE: Uuid = Uuid::from_bytes([
    0x4e, 0x21, 0x9b, 0x1a, 0x57, 0x9c, 0x4b, 0x37, 0xb6, 0x29, 0x6c, 0xfe, 0x6e, 0x47, 0xd1, 0x40,
]);

/// Payload of `automate.condition.v1`.
///
/// Shape preserves the legacy `AutomationRunInput` (`workers-go/
/// workflow-automation/internal/contract/contract.go`) one-to-one
/// so existing producers — manual UI button, webhook, lineage
/// service-to-service call, `pipeline-schedule-service` cron / event
/// fan-out — all continue to work without payload changes. The
/// `trigger_payload` field carries the same free-form map the legacy
/// Go workflow inspected via `triggerHasOntologyAction`.
///
/// The producer MUST set `correlation_id` to a stable identifier
/// (typically the inbound HTTP request's audit correlation id or
/// the upstream event's id) so consumer-side dedup via
/// `derive_condition_event_id` works correctly.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AutomateConditionV1 {
    /// Workflow definition (catalog row) the run belongs to.
    pub definition_id: Uuid,
    /// Owning tenant. Free-form string so existing producers (which
    /// pass either a workspace UUID or an opaque tenant slug) keep
    /// working; the consumer normalises to UUID via
    /// [`tenant_uuid_from_str`] when persisting to the
    /// `automation_runs.tenant_id UUID` column.
    pub tenant_id: String,
    /// Stable correlation id. Used (a) as the audit correlation id
    /// on the downstream effect call's `x-audit-correlation-id`
    /// header, (b) as one of the inputs to `derive_run_id`, and
    /// (c) as the outcome event's correlation id.
    pub correlation_id: Uuid,
    /// Free-form actor identifier — user UUID, service principal,
    /// or sentinel like `system` / `manual`. Mirrors
    /// `AutomationRunInput::triggered_by` from the Go contract.
    pub triggered_by: String,
    /// One of `manual` / `webhook` / `lineage_build` / `event` /
    /// `cron`. Carried for audit and metric labelling; the consumer
    /// does not branch on this value.
    pub trigger_type: String,
    /// Free-form trigger payload. Worker today branches on whether
    /// it carries `action_id` (root) or `ontology_action.action_id`
    /// (nested) — the consumer preserves that semantics.
    #[serde(default)]
    pub trigger_payload: Value,
}

/// Payload of `automate.outcome.v1`.
///
/// Mirrors the [`crate::domain::automation_run::AutomationRun`]
/// terminal-state projection. Downstream observability dashboards
/// and notification fan-out should pattern-match on `status` only;
/// `effect_response` is best-effort (only present when the effect
/// returned 2xx).
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AutomateOutcomeV1 {
    pub run_id: Uuid,
    pub definition_id: Uuid,
    pub tenant_id: String,
    pub correlation_id: Uuid,
    /// `completed` or `failed`. `cancelled` is reserved for future
    /// control-plane HTTP cancel routes (Tarea 5.3 follow-up;
    /// mirrors FASE 7 / Tarea 7.4 cron timeout sweep).
    pub status: String,
    /// Decoded JSON body of the upstream effect call. `None` on
    /// failure paths.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub effect_response: Option<Value>,
    /// Operator-facing error message. `None` on success paths.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
    /// Number of effect-dispatch attempts the consumer made before
    /// reaching this terminal state. `1` for first-try success.
    pub attempts: u32,
}

/// Derive the canonical `automation_runs.id` for a `(definition_id,
/// correlation_id)` pair. Producer retries that re-publish the same
/// condition collapse onto the same row.
pub fn derive_run_id(definition_id: Uuid, correlation_id: Uuid) -> Uuid {
    let mut buf = [0_u8; 32];
    buf[..16].copy_from_slice(definition_id.as_bytes());
    buf[16..].copy_from_slice(correlation_id.as_bytes());
    Uuid::new_v5(&WORKFLOW_AUTOMATION_NAMESPACE, &buf)
}

/// Derive the per-condition `event_id` for the `processed_events`
/// idempotency table. Defined separately from [`derive_run_id`] so
/// the two namespaces do not collide if the run id ever needs to
/// be addressable as a Kafka event id by another consumer.
pub fn derive_condition_event_id(definition_id: Uuid, correlation_id: Uuid) -> Uuid {
    let mut buf = [0_u8; 33];
    buf[..16].copy_from_slice(definition_id.as_bytes());
    buf[16..32].copy_from_slice(correlation_id.as_bytes());
    buf[32] = b'C'; // distinguish from the run-id namespace
    Uuid::new_v5(&WORKFLOW_AUTOMATION_NAMESPACE, &buf)
}

/// Best-effort coercion from the wire-format `tenant_id String` to
/// the column-format `tenant_id Uuid`.
///
/// Producers today pass either a UUID (workspace id) or an opaque
/// slug. UUID inputs round-trip; non-UUID inputs are mapped via
/// UUIDv5 to a stable derivative so the consumer never crashes on
/// malformed inputs and the row is always insertable.
///
/// Once every producer is normalised onto a UUID payload (FASE 9),
/// this helper can be replaced by a strict `Uuid::parse_str`.
pub fn tenant_uuid_from_str(tenant_id: &str) -> Uuid {
    if let Ok(parsed) = Uuid::parse_str(tenant_id) {
        return parsed;
    }
    Uuid::new_v5(&WORKFLOW_AUTOMATION_NAMESPACE, tenant_id.as_bytes())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn derive_run_id_is_stable_across_calls() {
        let definition = Uuid::now_v7();
        let correlation = Uuid::now_v7();
        let a = derive_run_id(definition, correlation);
        let b = derive_run_id(definition, correlation);
        assert_eq!(a, b);
        assert_eq!(a.get_version_num(), 5);
    }

    #[test]
    fn derive_run_id_differs_per_input_pair() {
        let definition_a = Uuid::now_v7();
        let definition_b = Uuid::now_v7();
        let correlation = Uuid::now_v7();
        assert_ne!(
            derive_run_id(definition_a, correlation),
            derive_run_id(definition_b, correlation)
        );
        let other_correlation = Uuid::now_v7();
        assert_ne!(
            derive_run_id(definition_a, correlation),
            derive_run_id(definition_a, other_correlation)
        );
    }

    #[test]
    fn condition_event_id_is_distinct_from_run_id() {
        let definition = Uuid::now_v7();
        let correlation = Uuid::now_v7();
        let run = derive_run_id(definition, correlation);
        let condition = derive_condition_event_id(definition, correlation);
        assert_ne!(run, condition);
        assert_eq!(condition.get_version_num(), 5);
    }

    #[test]
    fn condition_event_id_is_stable_across_calls() {
        let definition = Uuid::now_v7();
        let correlation = Uuid::now_v7();
        assert_eq!(
            derive_condition_event_id(definition, correlation),
            derive_condition_event_id(definition, correlation)
        );
    }

    #[test]
    fn tenant_uuid_from_str_round_trips_uuids() {
        let original = Uuid::now_v7();
        assert_eq!(tenant_uuid_from_str(&original.to_string()), original);
    }

    #[test]
    fn tenant_uuid_from_str_is_stable_for_arbitrary_strings() {
        let a = tenant_uuid_from_str("acme-corp");
        let b = tenant_uuid_from_str("acme-corp");
        let c = tenant_uuid_from_str("acme-corp-2");
        assert_eq!(a, b);
        assert_ne!(a, c);
        assert_eq!(a.get_version_num(), 5);
    }

    #[test]
    fn condition_v1_round_trip() {
        let event = AutomateConditionV1 {
            definition_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            correlation_id: Uuid::now_v7(),
            triggered_by: "user-7".into(),
            trigger_type: "manual".into(),
            trigger_payload: json!({"action_id": "promote-customer"}),
        };
        let json = serde_json::to_string(&event).unwrap();
        let back: AutomateConditionV1 = serde_json::from_str(&json).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn outcome_v1_round_trip_success() {
        let event = AutomateOutcomeV1 {
            run_id: Uuid::now_v7(),
            definition_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            correlation_id: Uuid::now_v7(),
            status: "completed".into(),
            effect_response: Some(json!({"object_id": "obj-1"})),
            error: None,
            attempts: 1,
        };
        let json = serde_json::to_string(&event).unwrap();
        let back: AutomateOutcomeV1 = serde_json::from_str(&json).unwrap();
        assert_eq!(event, back);
        // skip_serializing_if must omit the error key entirely
        assert!(!json.contains("\"error\""));
    }

    #[test]
    fn outcome_v1_round_trip_failure_omits_response() {
        let event = AutomateOutcomeV1 {
            run_id: Uuid::now_v7(),
            definition_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            correlation_id: Uuid::now_v7(),
            status: "failed".into(),
            effect_response: None,
            error: Some("ontology action returned 500".into()),
            attempts: 5,
        };
        let json = serde_json::to_string(&event).unwrap();
        assert!(!json.contains("\"effect_response\""));
        assert!(json.contains("\"error\""));
        let back: AutomateOutcomeV1 = serde_json::from_str(&json).unwrap();
        assert_eq!(event, back);
    }
}
