//! Pinned Kafka topic constants for the FASE 5 / Tarea 5.3 Foundry-
//! pattern runtime of `workflow-automation-service`.
//!
//! Topics are part of the wire contract with every producer of
//! automation conditions (HTTP handlers in this service, future
//! `pipeline-schedule-service` cron CronJobs, future Kafka-driven
//! event matchers). Pinning them as `&'static str` constants makes
//! a typo a compile error.
//!
//! Topic provisioning lives in `infra/helm/infra/kafka-cluster/`
//! values (`automate.condition.v1`, `automate.outcome.v1` and the
//! matching DLQs are already declared).

/// Input topic. The condition consumer subscribes here. Payload is
/// JSON-serialised [`crate::event::AutomateConditionV1`].
///
/// Replaces the Temporal task queue `openfoundry.workflow-automation`
/// (see `docs/architecture/refactor/workflow-automation-worker-inventory.md`
/// §6, row 9).
pub const AUTOMATE_CONDITION_V1: &str = "automate.condition.v1";

/// Output (control plane) topic. One record per terminal AutomationRun
/// transition (`completed` / `failed`). Payload is JSON-serialised
/// [`crate::event::AutomateOutcomeV1`].
///
/// Downstream consumers: notification-alerting-service (UI live
/// feed), audit-compliance-service (audit trail), lineage-service
/// (end-to-end lineage stitching).
pub const AUTOMATE_OUTCOME_V1: &str = "automate.outcome.v1";

#[cfg(test)]
mod tests {
    use super::*;

    /// Ensure the topic names match the helm-provisioned values
    /// exactly. Drift in either direction is a runtime failure
    /// (the broker rejects publishes / subscribes to topics that
    /// do not exist), so keep the strings stable.
    #[test]
    fn topic_names_match_helm_provisioning() {
        assert_eq!(AUTOMATE_CONDITION_V1, "automate.condition.v1");
        assert_eq!(AUTOMATE_OUTCOME_V1, "automate.outcome.v1");
    }
}
