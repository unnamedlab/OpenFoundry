//! OpenLineage emission for `lineage.events.v1` from pipeline builds.
//!
//! Wraps [`outbox::lineage_event`] with the build-service's job
//! identity (namespace `of://pipelines/{pipeline_rid}`, job
//! `pipeline.build`). The build_id is reused as the OpenLineage run id
//! so the lineage stream joins back to `foundry.build.events.v1` via
//! the `ol-run-id` header.
//!
//! Topic literal pinned here for `rg "lineage.events.v1"` discovery
//! (matches [`outbox::lineage_event::TOPIC`]).

use chrono::{DateTime, Utc};
use outbox::lineage_event::{LineageDataset, LineageEvent, LineageEventType};
use sqlx::{Postgres, Transaction};
use uuid::Uuid;

/// Mirror of [`outbox::lineage_event::TOPIC`] for this producer.
pub const LINEAGE_TOPIC: &str = "lineage.events.v1";

/// Stable namespace for jobs emitted by `pipeline-build-service`.
const JOB_NAMESPACE_PREFIX: &str = "of://pipelines";

/// OpenLineage job name for the pipeline-build-service producer.
const JOB_NAME: &str = "pipeline.build";

/// Enqueue an OpenLineage event for the given build transition.
///
/// `pipeline_rid` is folded into the job namespace so consumers can
/// segregate per-pipeline run histories without parsing the payload.
/// `output_dataset_rids` lands in the OL `outputs[]` array — empty
/// vec is fine (e.g. on START before outputs are known).
///
/// Failures are logged and swallowed: lineage hiccups must not abort
/// the build lifecycle, mirroring [`crate::domain::build_events::enqueue`].
pub async fn enqueue(
    tx: &mut Transaction<'_, Postgres>,
    event_type: LineageEventType,
    build_id: Uuid,
    pipeline_rid: &str,
    output_dataset_rids: &[String],
    event_time: DateTime<Utc>,
) {
    let mut event = LineageEvent::new(
        event_type,
        build_id,
        format!("{JOB_NAMESPACE_PREFIX}/{pipeline_rid}"),
        JOB_NAME,
    )
    .at(event_time);
    for rid in output_dataset_rids {
        event = event.with_output(LineageDataset::new("of://datasets", rid.clone()));
    }
    if let Err(error) = outbox::lineage_event::enqueue(tx, event).await {
        tracing::warn!(
            event = event_type.as_str(),
            build_id = %build_id,
            pipeline_rid = %pipeline_rid,
            %error,
            "lineage outbox enqueue failed"
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn topic_constant_matches_outbox_helper() {
        assert_eq!(LINEAGE_TOPIC, outbox::lineage_event::TOPIC);
    }
}
