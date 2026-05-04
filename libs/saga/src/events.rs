//! Wire-format payloads for the `saga.*.v1` Kafka topics emitted /
//! consumed by `libs/saga`'s [`crate::SagaRunner`] and the consumer-
//! driven Foundry-pattern services that wrap it
//! (`automation-operations-service`, `workflow-automation-service`,
//! …).
//!
//! Locked-in shapes: every field is documented and round-tripped by
//! a unit test in this module so a downstream consumer can rely on
//! the wire contract without reading runner source.
//!
//! ## Topic catalog (FASE 6 / Tarea 6.2)
//!
//! | Topic                          | Payload type                  | Direction |
//! | ------------------------------ | ----------------------------- | --------- |
//! | `saga.step.requested.v1`       | [`SagaStepRequestedV1`]       | Inbound   |
//! | `saga.step.completed.v1`       | [`SagaStepCompletedV1`]       | Outbound  |
//! | `saga.step.failed.v1`          | [`SagaStepFailedV1`]          | Outbound  |
//! | `saga.step.compensated.v1`     | [`SagaStepCompensatedV1`]     | Outbound  |
//! | `saga.compensate.v1`           | [`SagaCompensateRequestedV1`] | Inbound   |
//! | `saga.completed.v1`            | [`SagaCompletedV1`]           | Outbound  |
//! | `saga.aborted.v1`              | [`SagaAbortedV1`]             | Outbound  |
//!
//! "Inbound" topics are consumed by the saga runtime
//! (handler / cron / upstream event published into them); "outbound"
//! topics are published BY the runtime via the transactional outbox
//! and surfaced by Debezium's EventRouter SMT.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

// ─── Topic name constants ────────────────────────────────────────────

/// Topic the saga runtime subscribes to. Producers (HTTP handler in
/// the owning service, k8s CronJob, upstream domain event) publish
/// here to invite the runtime to start (or resume) a saga.
pub const SAGA_STEP_REQUESTED_V1: &str = "saga.step.requested.v1";

/// Topic the runner emits per successful step.
pub const SAGA_STEP_COMPLETED_V1: &str = "saga.step.completed.v1";

/// Topic the runner emits per failed step.
pub const SAGA_STEP_FAILED_V1: &str = "saga.step.failed.v1";

/// Topic the runner emits per successful LIFO compensation.
pub const SAGA_STEP_COMPENSATED_V1: &str = "saga.step.compensated.v1";

/// Control-plane signal that asks an upstream saga to roll back. Out
/// of scope for the FASE 6 single-saga path; declared here so the
/// topic exists in helm and the wire-type is locked in.
pub const SAGA_COMPENSATE_V1: &str = "saga.compensate.v1";

/// Terminal: every step succeeded and `SagaRunner::finish` was
/// called.
pub const SAGA_COMPLETED_V1: &str = "saga.completed.v1";

/// Terminal: caller invoked `SagaRunner::abort`. LIFO compensations
/// have already run (and emitted their own `saga.step.compensated.v1`
/// events) before this final terminal event.
pub const SAGA_ABORTED_V1: &str = "saga.aborted.v1";

// ─── Outbound payloads (emitted by the runner) ───────────────────────

/// Payload of `saga.step.completed.v1`. Replaces the ad-hoc
/// `serde_json::json!({...})` shape the runner used in pre-Tarea-6.2
/// code.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaStepCompletedV1 {
    pub saga_id: Uuid,
    /// Saga type, e.g. `retention.sweep`. Same value as
    /// `saga.state.name`.
    pub saga: String,
    /// Step identifier within this saga type (matches the
    /// `SagaStep::step_name()` constant of the Rust impl).
    pub step: String,
    /// Decoded JSON body of the step's `Output`. Round-trips through
    /// `serde_json` per the [`crate::SagaStep`] contract.
    pub output: Value,
}

/// Payload of `saga.step.failed.v1`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaStepFailedV1 {
    pub saga_id: Uuid,
    pub saga: String,
    pub step: String,
    /// Operator-facing error string (the [`crate::SagaError::Step`]
    /// `message`). Free-form on purpose — operators read it; no
    /// downstream pattern-matching.
    pub error: String,
}

/// Payload of `saga.step.compensated.v1`. Emitted once per
/// previously-completed step that was successfully reversed.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaStepCompensatedV1 {
    pub saga_id: Uuid,
    pub saga: String,
    pub step: String,
}

/// Payload of `saga.completed.v1`. Emitted once when every step
/// succeeded and the caller invoked [`crate::SagaRunner::finish`].
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaCompletedV1 {
    pub saga_id: Uuid,
    pub saga: String,
    /// Names of every step that succeeded, in execution order.
    /// Mirrors `saga.state.completed_steps`.
    pub completed_steps: Vec<String>,
}

/// Payload of `saga.aborted.v1`. Emitted once when the caller
/// invoked [`crate::SagaRunner::abort`]. LIFO compensations have
/// already emitted their own `saga.step.compensated.v1` events
/// before this terminal event.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaAbortedV1 {
    pub saga_id: Uuid,
    pub saga: String,
}

// ─── Inbound payloads (consumed by the runner) ───────────────────────

/// Payload of `saga.step.requested.v1`. The saga runtime subscribes
/// to this topic; one record ⇒ one saga start (or resume).
///
/// `saga` and `input` together are enough to drive the entire saga;
/// the runtime's per-`saga` registry decides which `SagaStep` graph
/// to dispatch.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaStepRequestedV1 {
    /// Caller-chosen aggregate id. Producer retries that re-publish
    /// the same `saga_id` are idempotent at three layers: the
    /// `processed_events` dedup row, `INSERT … ON CONFLICT DO
    /// NOTHING` on `saga.state`, and the runner's
    /// `completed_steps`-aware short-circuit on already-finished
    /// steps. Recommended: deterministic UUIDv5 derived from
    /// `(saga, correlation_id)` so the same producer trigger always
    /// resolves to the same saga.
    pub saga_id: Uuid,
    /// Saga type, used by the runtime as the dispatch key in its
    /// step-graph registry. Free-form string at the schema level;
    /// the consumer rejects unknown values into the saga's
    /// `failed` state.
    pub saga: String,
    /// Owning tenant. String to match the rest of the platform (some
    /// producers pass UUIDs, others slugs); the consumer normalises.
    pub tenant_id: String,
    /// End-to-end correlation id propagated to every effect call as
    /// `x-audit-correlation-id`. Producer SHOULD set this to the id
    /// already attached to the inbound HTTP request span; if absent
    /// the consumer generates a fresh UUIDv7.
    pub correlation_id: Uuid,
    /// Free-form actor identifier (user UUID, service principal,
    /// `system`).
    pub triggered_by: String,
    /// Free-form input payload — the saga's first step is invoked
    /// with this value, the runtime is responsible for parsing it
    /// into the step's typed `Input`.
    #[serde(default)]
    pub input: Value,
}

/// Payload of `saga.compensate.v1`. Out-of-band compensation request
/// (cross-saga rollback signal). Out of scope for FASE 6 single-saga
/// path; the wire-type is locked in here so the topic + payload
/// shape are stable for the FASE 6.4+ chaos / multi-saga work.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SagaCompensateRequestedV1 {
    pub saga_id: Uuid,
    pub saga: String,
    /// Caller-supplied reason for the compensation request — surfaced
    /// in the `saga.aborted.v1` event the runtime emits after
    /// running the compensations.
    pub reason: String,
    /// End-to-end correlation id, same semantics as
    /// `SagaStepRequestedV1::correlation_id`.
    pub correlation_id: Uuid,
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn fake_id() -> Uuid {
        Uuid::nil()
    }

    #[test]
    fn topic_constants_use_v1_suffix() {
        for topic in [
            SAGA_STEP_REQUESTED_V1,
            SAGA_STEP_COMPLETED_V1,
            SAGA_STEP_FAILED_V1,
            SAGA_STEP_COMPENSATED_V1,
            SAGA_COMPENSATE_V1,
            SAGA_COMPLETED_V1,
            SAGA_ABORTED_V1,
        ] {
            assert!(topic.ends_with(".v1"), "topic {topic} must end with .v1");
        }
    }

    #[test]
    fn topic_constants_match_runner_emit_topics() {
        // Defence in depth: SagaEventKind::topic() must match the
        // constants here exactly. The runner uses the enum, the
        // helm chart uses the constants — they must agree.
        assert_eq!(crate::SagaEventKind::StepCompleted.topic(), SAGA_STEP_COMPLETED_V1);
        assert_eq!(crate::SagaEventKind::StepFailed.topic(), SAGA_STEP_FAILED_V1);
        assert_eq!(
            crate::SagaEventKind::StepCompensated.topic(),
            SAGA_STEP_COMPENSATED_V1
        );
        assert_eq!(crate::SagaEventKind::SagaCompleted.topic(), SAGA_COMPLETED_V1);
        assert_eq!(crate::SagaEventKind::SagaAborted.topic(), SAGA_ABORTED_V1);
    }

    #[test]
    fn step_completed_v1_round_trip() {
        let event = SagaStepCompletedV1 {
            saga_id: fake_id(),
            saga: "retention.sweep".into(),
            step: "evict_old_objects".into(),
            output: json!({"evicted": 42}),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaStepCompletedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn step_failed_v1_round_trip() {
        let event = SagaStepFailedV1 {
            saga_id: fake_id(),
            saga: "cleanup.workspace".into(),
            step: "drop_blobs".into(),
            error: "S3 returned 503".into(),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaStepFailedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn step_compensated_v1_round_trip() {
        let event = SagaStepCompensatedV1 {
            saga_id: fake_id(),
            saga: "retention.sweep".into(),
            step: "evict_old_objects".into(),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaStepCompensatedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn step_requested_v1_round_trip_with_input() {
        let event = SagaStepRequestedV1 {
            saga_id: fake_id(),
            saga: "retention.sweep".into(),
            tenant_id: "acme".into(),
            correlation_id: fake_id(),
            triggered_by: "system".into(),
            input: json!({"older_than_days": 90}),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaStepRequestedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn step_requested_v1_round_trip_with_default_input() {
        // input defaults to Value::Null when omitted on the wire.
        let raw = r#"{
            "saga_id": "00000000-0000-0000-0000-000000000000",
            "saga": "retention.sweep",
            "tenant_id": "acme",
            "correlation_id": "00000000-0000-0000-0000-000000000000",
            "triggered_by": "system"
        }"#;
        let parsed: SagaStepRequestedV1 = serde_json::from_str(raw).unwrap();
        assert_eq!(parsed.input, Value::Null);
    }

    #[test]
    fn compensate_v1_round_trip() {
        let event = SagaCompensateRequestedV1 {
            saga_id: fake_id(),
            saga: "create_order".into(),
            reason: "downstream payment provider rejected".into(),
            correlation_id: fake_id(),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaCompensateRequestedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn completed_v1_round_trip() {
        let event = SagaCompletedV1 {
            saga_id: fake_id(),
            saga: "create_order".into(),
            completed_steps: vec![
                "reserve_inventory".into(),
                "charge_card".into(),
                "ship_order".into(),
            ],
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaCompletedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }

    #[test]
    fn aborted_v1_round_trip() {
        let event = SagaAbortedV1 {
            saga_id: fake_id(),
            saga: "create_order".into(),
        };
        let s = serde_json::to_string(&event).unwrap();
        let back: SagaAbortedV1 = serde_json::from_str(&s).unwrap();
        assert_eq!(event, back);
    }
}
