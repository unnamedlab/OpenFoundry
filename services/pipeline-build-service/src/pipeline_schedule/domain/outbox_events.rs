//! Outbox event types emitted by the schedule plane.
//!
//! Topics align with the rest of the platform's
//! `<aggregate>.<verb>.<state>` convention. Every constant here MUST
//! be matched by a row in `services/pipeline-schedule-service/migrations/`
//! once the topic registry is updated; the constants are used both by
//! the publisher and by every consumer's subscriber filter so the
//! literal names stay in lockstep.

use serde::{Deserialize, Serialize};

// ---- Lifecycle events ------------------------------------------------------

pub const EVENT_SCHEDULE_CREATED: &str = "schedule.created";
pub const EVENT_SCHEDULE_UPDATED: &str = "schedule.updated";
pub const EVENT_SCHEDULE_PAUSED: &str = "schedule.paused";
pub const EVENT_SCHEDULE_RESUMED: &str = "schedule.resumed";
pub const EVENT_SCHEDULE_AUTO_PAUSED: &str = "schedule.auto_paused";
pub const EVENT_SCHEDULE_AUTO_RESUMED: &str = "schedule.auto_resumed";
pub const EVENT_SCHEDULE_DELETED: &str = "schedule.deleted";

// ---- Run events ------------------------------------------------------------

pub const EVENT_SCHEDULE_RUN_STARTED: &str = "schedule.run.started";
pub const EVENT_SCHEDULE_RUN_SUCCEEDED: &str = "schedule.run.succeeded";
pub const EVENT_SCHEDULE_RUN_IGNORED: &str = "schedule.run.ignored";
pub const EVENT_SCHEDULE_RUN_FAILED: &str = "schedule.run.failed";

// ---- Parameterized pipelines (P4) -----------------------------------------

pub const EVENT_PARAMETERIZED_DEPLOYMENT_CREATED: &str =
    "parameterized_pipeline.deployment.created";
pub const EVENT_PARAMETERIZED_DEPLOYMENT_RUN_DISPATCHED: &str =
    "parameterized_pipeline.deployment.run.dispatched";

/// Every event type the schedule plane writes to the outbox. Used by
/// the topic-registry CI gate that validates new emitters never drift
/// outside this allow-list.
pub const ALL_EVENT_TYPES: &[&str] = &[
    EVENT_SCHEDULE_CREATED,
    EVENT_SCHEDULE_UPDATED,
    EVENT_SCHEDULE_PAUSED,
    EVENT_SCHEDULE_RESUMED,
    EVENT_SCHEDULE_AUTO_PAUSED,
    EVENT_SCHEDULE_AUTO_RESUMED,
    EVENT_SCHEDULE_DELETED,
    EVENT_SCHEDULE_RUN_STARTED,
    EVENT_SCHEDULE_RUN_SUCCEEDED,
    EVENT_SCHEDULE_RUN_IGNORED,
    EVENT_SCHEDULE_RUN_FAILED,
    EVENT_PARAMETERIZED_DEPLOYMENT_CREATED,
    EVENT_PARAMETERIZED_DEPLOYMENT_RUN_DISPATCHED,
];

/// Strongly-typed shape every payload follows. The actual payload
/// fields vary per event type; the wrapper standardises the envelope
/// so consumers can route on `event_type` without reflection.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleEventEnvelope {
    pub event_type: String,
    pub schedule_rid: Option<String>,
    pub schedule_run_rid: Option<String>,
    pub deployment_id: Option<String>,
    pub payload: serde_json::Value,
    pub emitted_at: chrono::DateTime<chrono::Utc>,
    pub actor: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn all_event_types_are_unique_and_use_dot_separator() {
        let mut seen = std::collections::HashSet::new();
        for t in ALL_EVENT_TYPES {
            assert!(seen.insert(*t), "duplicate event type: {t}");
            assert!(t.contains('.'), "event type must contain '.': {t}");
        }
    }

    #[test]
    fn doc_required_lifecycle_events_present() {
        for required in [
            "schedule.created",
            "schedule.updated",
            "schedule.paused",
            "schedule.resumed",
            "schedule.auto_paused",
            "schedule.auto_resumed",
            "schedule.deleted",
            "schedule.run.started",
            "schedule.run.succeeded",
            "schedule.run.ignored",
            "schedule.run.failed",
            "parameterized_pipeline.deployment.created",
            "parameterized_pipeline.deployment.run.dispatched",
        ] {
            assert!(
                ALL_EVENT_TYPES.contains(&required),
                "missing event type: {required}"
            );
        }
    }
}
