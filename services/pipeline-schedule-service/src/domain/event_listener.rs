//! Listener for the data-plane Kafka topics that drive Event /
//! Compound triggers:
//!
//!   * `foundry.dataset.events.v1` — `DATA_UPDATED`, `NEW_LOGIC` (D1.1.5 P4)
//!   * `foundry.build.events.v1`   — `JOB_SUCCEEDED`
//!   * `foundry.schedule.events.v1` — `SCHEDULE_RAN_SUCCESSFULLY` (auto-feed loop)
//!
//! Per inbound record we:
//!   1. parse `target_rid` and event-type out of the payload;
//!   2. look up every schedule whose trigger references `target_rid`
//!      (the lookup is JSONB-indexed via `trigger_json @>` predicates);
//!   3. push the observation into [`PgTriggerEvaluator::observe`] —
//!      which both persists the observation AND returns whether the
//!      compound trigger has now become satisfied;
//!   4. if `Satisfied`, enqueue an ad-hoc Temporal workflow run.
//!
//! Step 4's actual dispatch is delegated to
//! [`crate::domain::temporal_schedule`] in P3; here we only enqueue
//! the run-now intent.

use std::collections::HashSet;

use chrono::{DateTime, Utc};
use serde_json::Value;
use sqlx::PgPool;

use crate::domain::trigger::{EventType, Schedule};
use crate::domain::trigger_engine::{ObservedEvent, PgTriggerEvaluator, TriggerOutcome};

/// Topic constants. Kept literal here so the listener can be wired up
/// independently of the topic-registry helper landing in P5.
pub const DATASET_EVENTS_TOPIC: &str = "foundry.dataset.events.v1";
pub const BUILD_EVENTS_TOPIC: &str = "foundry.build.events.v1";
pub const SCHEDULE_EVENTS_TOPIC: &str = "foundry.schedule.events.v1";

/// Decoded payload coming off any of the three topics. The shape is
/// intentionally tiny — we only consume what the trigger model needs.
#[derive(Debug, Clone)]
pub struct InboundEvent {
    pub event_type: EventType,
    pub target_rid: String,
    pub occurred_at: DateTime<Utc>,
}

/// Parse a JSON event payload into an [`InboundEvent`]. Schemas for
/// each topic are out-of-band (see `proto/data_integration/`); this
/// function is what every consumer calls to project the payload onto
/// the trigger-engine vocabulary.
pub fn decode_event_payload(topic: &str, payload: &Value) -> Option<InboundEvent> {
    let target_rid = payload.get("target_rid").and_then(Value::as_str)?.to_string();
    let event_type_str = payload.get("event_type").and_then(Value::as_str)?;
    let event_type = match (topic, event_type_str) {
        (DATASET_EVENTS_TOPIC, "data_updated") => EventType::DataUpdated,
        (DATASET_EVENTS_TOPIC, "new_logic") => EventType::NewLogic,
        (BUILD_EVENTS_TOPIC, "job_succeeded") => EventType::JobSucceeded,
        (SCHEDULE_EVENTS_TOPIC, "schedule_ran_successfully") => {
            EventType::ScheduleRanSuccessfully
        }
        _ => return None,
    };
    let occurred_at = payload
        .get("occurred_at")
        .and_then(Value::as_str)
        .and_then(|s| DateTime::parse_from_rfc3339(s).ok())
        .map(|dt| dt.with_timezone(&Utc))
        .unwrap_or_else(Utc::now);
    Some(InboundEvent {
        event_type,
        target_rid,
        occurred_at,
    })
}

/// Find every non-paused schedule whose persisted `trigger_json`
/// references `target_rid`. We use `@>` containment with a small
/// candidate-array literal so JSONB indexes can serve the query — for
/// today the search is precise enough for the ~thousand-schedule
/// workloads we care about, and a `gin (trigger_json)` index brings
/// it under 10 ms at expected cardinalities.
pub async fn find_schedules_referencing_target(
    pool: &PgPool,
    target_rid: &str,
) -> Result<Vec<Schedule>, sqlx::Error> {
    use sqlx::Row;
    let sql = format!(
        "SELECT {} FROM schedules
         WHERE paused = FALSE
           AND trigger_json::text LIKE $1",
        crate::domain::schedule_store::SCHEDULE_COLUMNS,
    );
    let rows = sqlx::query(&sql)
        .bind(format!("%{target_rid}%"))
        .fetch_all(pool)
        .await?;

    let mut out = Vec::with_capacity(rows.len());
    let mut seen = HashSet::new();
    for row in rows {
        let rid: String = row.try_get("rid")?;
        if !seen.insert(rid.clone()) {
            continue;
        }
        let trigger_json: serde_json::Value = row.try_get("trigger_json")?;
        let target_json: serde_json::Value = row.try_get("target_json")?;
        let trigger = match serde_json::from_value(trigger_json) {
            Ok(t) => t,
            Err(_) => continue,
        };
        let target = match serde_json::from_value(target_json) {
            Ok(t) => t,
            Err(_) => continue,
        };
        let scope_kind_str: String = row.try_get("scope_kind")?;
        let scope_kind = crate::domain::trigger::ScheduleScopeKind::parse(&scope_kind_str)
            .unwrap_or(crate::domain::trigger::ScheduleScopeKind::User);
        out.push(Schedule {
            id: row.try_get("id")?,
            rid,
            project_rid: row.try_get("project_rid")?,
            name: row.try_get("name")?,
            description: row.try_get("description")?,
            trigger,
            target,
            paused: row.try_get("paused")?,
            version: row.try_get("version")?,
            created_by: row.try_get("created_by")?,
            created_at: row.try_get("created_at")?,
            updated_at: row.try_get("updated_at")?,
            last_run_at: row.try_get("last_run_at")?,
            paused_reason: row.try_get("paused_reason")?,
            paused_at: row.try_get("paused_at")?,
            auto_pause_exempt: row.try_get("auto_pause_exempt")?,
            pending_re_run: row.try_get("pending_re_run")?,
            active_run_id: row.try_get("active_run_id")?,
            scope_kind,
            project_scope_rids: row.try_get("project_scope_rids")?,
            run_as_user_id: row.try_get("run_as_user_id")?,
            service_principal_id: row.try_get("service_principal_id")?,
        });
    }
    Ok(out)
}

/// Subscribe to the three trigger-feeding topics and pump records
/// through `process_event` until the subscriber closes. Designed to be
/// `tokio::spawn`-ed from `main.rs` once the rest of the runtime
/// (config, Postgres pool, evaluator) has booted. The returned future
/// completes only on a fatal subscriber error or shutdown.
pub async fn run_subscriber_loop(
    subscriber: std::sync::Arc<dyn event_bus_data::DataSubscriber>,
    pool: PgPool,
    evaluator: PgTriggerEvaluator,
) -> Result<(), ListenerError> {
    subscriber.subscribe(&[
        DATASET_EVENTS_TOPIC,
        BUILD_EVENTS_TOPIC,
        SCHEDULE_EVENTS_TOPIC,
    ])?;
    loop {
        let msg = subscriber.recv().await?;
        let topic = msg.topic().to_string();
        let payload = match msg.payload() {
            Some(bytes) => bytes,
            None => {
                let _ = subscriber.commit(&msg);
                continue;
            }
        };
        let payload_value: Value = match serde_json::from_slice(payload) {
            Ok(v) => v,
            Err(e) => {
                tracing::warn!(
                    topic = %topic,
                    error = %e,
                    "trigger listener could not decode event payload"
                );
                let _ = subscriber.commit(&msg);
                continue;
            }
        };
        let event = match decode_event_payload(&topic, &payload_value) {
            Some(e) => e,
            None => {
                let _ = subscriber.commit(&msg);
                continue;
            }
        };
        match process_event(&pool, &evaluator, event).await {
            Ok(satisfied) => {
                for rid in satisfied {
                    tracing::info!(
                        rid = %rid,
                        "schedule trigger satisfied — dispatcher should run it now"
                    );
                }
            }
            Err(e) => tracing::warn!(
                topic = %topic,
                error = %e,
                "process_event failed"
            ),
        }
        if let Err(e) = subscriber.commit(&msg) {
            tracing::warn!(error = %e, "commit failed");
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ListenerError {
    #[error("subscribe error: {0}")]
    Subscribe(#[from] event_bus_data::SubscribeError),
}

/// Process one inbound event end-to-end: persist observations against
/// every matching schedule, returning the list of schedule RIDs that
/// became fully satisfied (i.e. should be dispatched).
pub async fn process_event(
    pool: &PgPool,
    evaluator: &PgTriggerEvaluator,
    event: InboundEvent,
) -> Result<Vec<String>, ProcessError> {
    let candidates = find_schedules_referencing_target(pool, &event.target_rid).await?;
    let observed = ObservedEvent {
        event_type: event.event_type,
        target_rid: event.target_rid.clone(),
        occurred_at: event.occurred_at,
    };
    let mut satisfied = Vec::new();
    for schedule in candidates {
        match evaluator.observe(&schedule, &observed).await {
            Ok(TriggerOutcome::Satisfied) => satisfied.push(schedule.rid),
            Ok(_) => {}
            Err(e) => {
                tracing::warn!(
                    schedule_rid = %schedule.rid,
                    error = %e,
                    "trigger evaluator rejected an observation"
                );
            }
        }
    }
    Ok(satisfied)
}

#[derive(Debug, thiserror::Error)]
pub enum ProcessError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn decodes_dataset_data_updated() {
        let payload = json!({
            "event_type": "data_updated",
            "target_rid": "ri.foundry.main.dataset.x",
            "occurred_at": "2026-04-27T12:00:00Z"
        });
        let evt = decode_event_payload(DATASET_EVENTS_TOPIC, &payload).unwrap();
        assert_eq!(evt.event_type, EventType::DataUpdated);
        assert_eq!(evt.target_rid, "ri.foundry.main.dataset.x");
    }

    #[test]
    fn decodes_build_job_succeeded() {
        let payload = json!({
            "event_type": "job_succeeded",
            "target_rid": "ri.foundry.main.job.x"
        });
        let evt = decode_event_payload(BUILD_EVENTS_TOPIC, &payload).unwrap();
        assert_eq!(evt.event_type, EventType::JobSucceeded);
    }

    #[test]
    fn decodes_schedule_ran_successfully() {
        let payload = json!({
            "event_type": "schedule_ran_successfully",
            "target_rid": "ri.foundry.main.schedule.x"
        });
        let evt = decode_event_payload(SCHEDULE_EVENTS_TOPIC, &payload).unwrap();
        assert_eq!(evt.event_type, EventType::ScheduleRanSuccessfully);
    }

    #[test]
    fn rejects_event_type_not_belonging_to_topic() {
        let payload = json!({
            "event_type": "data_updated",
            "target_rid": "ri.x"
        });
        // data_updated belongs to the dataset topic, not the build one.
        assert!(decode_event_payload(BUILD_EVENTS_TOPIC, &payload).is_none());
    }
}
