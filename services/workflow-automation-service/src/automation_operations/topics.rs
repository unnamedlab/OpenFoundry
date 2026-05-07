//! Topic constants the service publishes to / subscribes from.
//!
//! Re-exports the canonical strings from `libs/saga::events` so the
//! service binary never builds a topic name from a literal — every
//! reference flows through the pinned constant. A drift between
//! the constants and the helm-provisioned topics in
//! `infra/helm/infra/kafka-cluster/values.yaml` is locked in by the
//! `topic_constants_match_helm_provisioning` test below.

pub use saga::events::{
    SAGA_ABORTED_V1, SAGA_COMPENSATE_V1, SAGA_COMPLETED_V1, SAGA_STEP_COMPENSATED_V1,
    SAGA_STEP_COMPLETED_V1, SAGA_STEP_FAILED_V1, SAGA_STEP_REQUESTED_V1,
};

#[cfg(test)]
mod tests {
    use super::*;

    /// Defence in depth — the helm chart provisions these exact
    /// strings (FASE 6 / Tarea 6.2). Keep this test in lockstep with
    /// the kafka-cluster topic catalog.
    #[test]
    fn topic_constants_match_helm_provisioning() {
        assert_eq!(SAGA_STEP_REQUESTED_V1, "saga.step.requested.v1");
        assert_eq!(SAGA_STEP_COMPLETED_V1, "saga.step.completed.v1");
        assert_eq!(SAGA_STEP_FAILED_V1, "saga.step.failed.v1");
        assert_eq!(SAGA_STEP_COMPENSATED_V1, "saga.step.compensated.v1");
        assert_eq!(SAGA_COMPENSATE_V1, "saga.compensate.v1");
        assert_eq!(SAGA_COMPLETED_V1, "saga.completed.v1");
        assert_eq!(SAGA_ABORTED_V1, "saga.aborted.v1");
    }
}
