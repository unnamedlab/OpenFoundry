//! Pinned Kafka topic constants for the FASE 7 / Tarea 7.3 state-
//! machine runtime.
//!
//! Topics are part of the wire contract with every consumer of
//! approval lifecycle events. Pinning them as `&'static str`
//! constants makes a typo a compile error.
//!
//! Topic provisioning lives in `infra/helm/infra/kafka-cluster/`
//! values (FASE 7 / Tarea 7.2). The
//! `topic_constants_match_helm_provisioning` test below is the
//! lockstep guard.

/// Outbound. Emitted by the HTTP `POST /api/v1/approvals` handler
/// when a new approval is created.
///
/// Replaces the implicit "Temporal workflow started" signal.
pub const APPROVAL_REQUESTED_V1: &str = "approval.requested.v1";

/// Inbound. Reserved for the future "manager decided externally"
/// path (FASE 7 inventory §11). No in-tree producer today; the
/// dedup table (`audit_compliance.processed_events`) is provisioned
/// so the consumer wires without a schema migration when a
/// producer exists.
pub const APPROVAL_DECIDED_V1: &str = "approval.decided.v1";

/// Outbound. Emitted on every terminal `pending → approved /
/// rejected` transition driven by `POST /api/v1/approvals/{id}/decide`.
pub const APPROVAL_COMPLETED_V1: &str = "approval.completed.v1";

/// Outbound. Subset of `approval.completed.v1` reserved for
/// `pending → expired` transitions driven by the timeout sweep
/// CronJob (FASE 7 / Tarea 7.4). Kept on a separate topic so SLO
/// alerts can fire on the expired feed without filtering the
/// completed feed.
pub const APPROVAL_EXPIRED_V1: &str = "approval.expired.v1";

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn topic_constants_match_helm_provisioning() {
        assert_eq!(APPROVAL_REQUESTED_V1, "approval.requested.v1");
        assert_eq!(APPROVAL_DECIDED_V1, "approval.decided.v1");
        assert_eq!(APPROVAL_COMPLETED_V1, "approval.completed.v1");
        assert_eq!(APPROVAL_EXPIRED_V1, "approval.expired.v1");
    }

    #[test]
    fn every_topic_uses_v1_suffix() {
        for topic in [
            APPROVAL_REQUESTED_V1,
            APPROVAL_DECIDED_V1,
            APPROVAL_COMPLETED_V1,
            APPROVAL_EXPIRED_V1,
        ] {
            assert!(topic.ends_with(".v1"), "topic {topic} must end with .v1");
        }
    }
}
