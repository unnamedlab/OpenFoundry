//! Outbox helpers for `foundry.build.events.v1`.
//!
//! Eight event names per the prompt — every transition the builds
//! lifecycle exposes externally. The `aggregate_id` is always the
//! build's UUID so consumers can join the events back into a single
//! timeline.

use serde::Serialize;
use sqlx::{Postgres, Transaction};
use uuid::Uuid;

pub const TOPIC: &str = "foundry.build.events.v1";

#[derive(Debug, Clone, Copy)]
pub enum BuildEvent {
    Created,
    Queued,
    ResolutionFailed,
    Started,
    JobStateChanged,
    Completed,
    Failed,
    Aborted,
}

impl BuildEvent {
    pub fn as_str(&self) -> &'static str {
        match self {
            BuildEvent::Created => "build.created",
            BuildEvent::Queued => "build.queued",
            BuildEvent::ResolutionFailed => "build.resolution_failed",
            BuildEvent::Started => "build.started",
            BuildEvent::JobStateChanged => "build.job_state_changed",
            BuildEvent::Completed => "build.completed",
            BuildEvent::Failed => "build.failed",
            BuildEvent::Aborted => "build.aborted",
        }
    }
}

/// Append a build event to the transactional outbox.
///
/// The caller's transaction commits the event with whatever other
/// rows the lifecycle change touches (`builds.state` update,
/// `job_state_transitions` row, ...) so consumers never see a
/// partial transition. Failures are logged and swallowed — outbox
/// hiccups must not abort the lifecycle.
pub async fn enqueue<P: Serialize>(
    tx: &mut Transaction<'_, Postgres>,
    event: BuildEvent,
    build_id: Uuid,
    payload: P,
) {
    let body = match serde_json::to_value(&payload) {
        Ok(v) => v,
        Err(err) => {
            tracing::warn!(error = %err, "build event payload serialise failed");
            return;
        }
    };
    let event_id = Uuid::new_v5(&uuid::Uuid::NAMESPACE_OID, format!("{}|{event:?}|{build_id}|{}",
        TOPIC,
        chrono::Utc::now().timestamp_nanos_opt().unwrap_or_default()).as_bytes());
    let oe = outbox::OutboxEvent::new(
        event_id,
        "build",
        build_id.to_string(),
        TOPIC.to_string(),
        serde_json::json!({
            "event": event.as_str(),
            "build_id": build_id,
            "payload": body,
        }),
    );
    if let Err(err) = outbox::enqueue(tx, oe).await {
        tracing::warn!(
            event = event.as_str(),
            build_id = %build_id,
            error = %err,
            "outbox enqueue failed"
        );
    }
}
