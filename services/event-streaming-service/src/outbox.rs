use chrono::{DateTime, Utc};
use outbox::{OutboxEvent, OutboxResult, enqueue};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{Postgres, Transaction};
use uuid::Uuid;

use crate::models::{
    branch::StreamBranch, profile::StreamingProfile, stream::StreamDefinition,
    stream_view::StreamView, topology::TopologyDefinition, window::WindowDefinition,
};

pub const STREAMING_CHANGED_TOPIC: &str = "dataset.streaming.changed.v1";
const EVENT_NAMESPACE: Uuid = Uuid::from_bytes([
    0x13, 0x4e, 0x0c, 0x9b, 0x83, 0x61, 0x45, 0x53, 0xa3, 0xce, 0x21, 0x7a, 0xf1, 0x8c, 0x54, 0x40,
]);

pub const STREAMING_CHANGED_V1_JSON_SCHEMA: &str = r#"{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "DatasetStreamingChangedV1",
  "type": "object",
  "required": [
    "event_type",
    "aggregate",
    "aggregate_id",
    "version",
    "occurred_at",
    "payload"
  ],
  "properties": {
    "event_type": { "type": "string" },
    "aggregate": {
      "type": "string",
      "enum": ["stream", "stream_branch", "stream_window", "stream_topology", "stream_profile"]
    },
    "aggregate_id": { "type": "string", "minLength": 1 },
    "stream_id": { "type": ["string", "null"], "format": "uuid" },
    "version": { "type": "string", "minLength": 1 },
    "occurred_at": { "type": "string", "format": "date-time" },
    "payload": { "type": "object" }
  },
  "additionalProperties": false
}"#;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamingChangedEvent {
    pub event_type: String,
    pub aggregate: String,
    pub aggregate_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub stream_id: Option<Uuid>,
    pub version: String,
    pub occurred_at: DateTime<Utc>,
    pub payload: Value,
}

impl StreamingChangedEvent {
    pub fn to_outbox_event(&self) -> OutboxEvent {
        OutboxEvent::new(
            derive_event_id(&self.aggregate, &self.aggregate_id, &self.version),
            self.aggregate.clone(),
            self.aggregate_id.clone(),
            STREAMING_CHANGED_TOPIC,
            serde_json::to_value(self).expect("streaming outbox payload must serialize"),
        )
    }
}

pub fn derive_event_id(aggregate: &str, aggregate_id: &str, version: &str) -> Uuid {
    Uuid::new_v5(
        &EVENT_NAMESPACE,
        format!("event-streaming/{aggregate}/{aggregate_id}@{version}").as_bytes(),
    )
}

pub async fn emit(
    tx: &mut Transaction<'_, Postgres>,
    event: &StreamingChangedEvent,
) -> OutboxResult<()> {
    enqueue(tx, event.to_outbox_event()).await
}

pub fn stream_created(stream: &StreamDefinition) -> StreamingChangedEvent {
    stream_event("stream.created", stream, stream.created_at)
}

pub fn stream_updated(stream: &StreamDefinition) -> StreamingChangedEvent {
    stream_event("stream.updated", stream, stream.updated_at)
}

/// Build the `stream.reset.v1` outbox payload. The reset emits the
/// stable `stream_rid` plus old/new view RIDs so consumers (push
/// agents, downstream pipelines) can rotate their POST URL or replay.
pub fn stream_reset(
    stream: &StreamDefinition,
    old_view_rid: &str,
    new_view: &StreamView,
    requested_by: &str,
    forced: bool,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: "stream.reset.v1".to_string(),
        aggregate: "stream".to_string(),
        aggregate_id: stream.id.to_string(),
        stream_id: Some(stream.id),
        // Use `reset:<gen>:<unix_ms>` so two resets in the same
        // millisecond still get distinct event ids.
        version: format!(
            "reset:{gen}:{ms}",
            gen = new_view.generation,
            ms = new_view.created_at.timestamp_millis(),
        ),
        occurred_at: new_view.created_at,
        payload: serde_json::json!({
            "stream_rid":   new_view.stream_rid,
            "old_view_rid": old_view_rid,
            "new_view_rid": new_view.view_rid,
            "generation":   new_view.generation,
            "requested_by": requested_by,
            "forced":       forced,
        }),
    }
}

fn stream_event(
    event_type: &str,
    stream: &StreamDefinition,
    occurred_at: DateTime<Utc>,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: event_type.to_string(),
        aggregate: "stream".to_string(),
        aggregate_id: stream.id.to_string(),
        stream_id: Some(stream.id),
        version: occurred_at.timestamp_millis().to_string(),
        occurred_at,
        payload: serde_json::to_value(stream).expect("stream definition must serialize"),
    }
}

/// Streaming profile lifecycle outbox events. The audit trail mirrors
/// these via a `tracing::info!(target = "audit", ...)` emission so the
/// `audit-compliance-service` collector can correlate with the
/// canonical change-data-capture stream.
pub fn profile_created(profile: &StreamingProfile) -> StreamingChangedEvent {
    profile_event(
        "streaming.profile.created",
        profile,
        profile.created_at,
        profile.created_at.timestamp_millis().to_string(),
    )
}

pub fn profile_updated(profile: &StreamingProfile) -> StreamingChangedEvent {
    profile_event(
        "streaming.profile.updated",
        profile,
        profile.updated_at,
        format!(
            "v{version}:{ms}",
            version = profile.version,
            ms = profile.updated_at.timestamp_millis()
        ),
    )
}

pub fn profile_imported(
    profile: &StreamingProfile,
    project_rid: &str,
    imported_by: &str,
    occurred_at: DateTime<Utc>,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: "streaming.profile.imported".to_string(),
        aggregate: "stream_profile".to_string(),
        aggregate_id: profile.id.to_string(),
        stream_id: None,
        version: format!(
            "imported:{project_rid}:{ms}",
            ms = occurred_at.timestamp_millis()
        ),
        occurred_at,
        payload: serde_json::json!({
            "profile_id":   profile.id,
            "profile_name": profile.name,
            "project_rid":  project_rid,
            "imported_by":  imported_by,
            "restricted":   profile.restricted,
        }),
    }
}

pub fn profile_removed_from_project(
    profile_id: uuid::Uuid,
    project_rid: &str,
    removed_by: &str,
    occurred_at: DateTime<Utc>,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: "streaming.profile.removed_from_project".to_string(),
        aggregate: "stream_profile".to_string(),
        aggregate_id: profile_id.to_string(),
        stream_id: None,
        version: format!(
            "removed:{project_rid}:{ms}",
            ms = occurred_at.timestamp_millis()
        ),
        occurred_at,
        payload: serde_json::json!({
            "profile_id":  profile_id,
            "project_rid": project_rid,
            "removed_by":  removed_by,
        }),
    }
}

fn profile_event(
    event_type: &str,
    profile: &StreamingProfile,
    occurred_at: DateTime<Utc>,
    version: String,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: event_type.to_string(),
        aggregate: "stream_profile".to_string(),
        aggregate_id: profile.id.to_string(),
        stream_id: None,
        version,
        occurred_at,
        payload: serde_json::to_value(profile).expect("profile must serialize"),
    }
}

pub fn branch_created(branch: &StreamBranch) -> StreamingChangedEvent {
    branch_event(
        "stream.branch.created",
        branch,
        branch.created_at,
        branch.created_at.timestamp_millis().to_string(),
    )
}

pub fn branch_archived(branch: &StreamBranch) -> StreamingChangedEvent {
    let occurred_at = branch.archived_at.unwrap_or_else(Utc::now);
    branch_event(
        "stream.branch.archived",
        branch,
        occurred_at,
        branch
            .archived_at
            .map(|ts| ts.timestamp_millis().to_string())
            .unwrap_or_else(|| format!("archived:{}", branch.id)),
    )
}

pub fn branch_deleted(
    branch: &StreamBranch,
    occurred_at: DateTime<Utc>,
    head_sequence_no: i64,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: "stream.branch.deleted".to_string(),
        aggregate: "stream_branch".to_string(),
        aggregate_id: branch.id.to_string(),
        stream_id: Some(branch.stream_id),
        version: format!(
            "deleted:{head_sequence_no}:{}",
            occurred_at.timestamp_millis()
        ),
        occurred_at,
        payload: serde_json::json!({
            "branch": branch,
            "deleted": true,
        }),
    }
}

pub fn branch_merged(
    source: &StreamBranch,
    target_branch_id: Uuid,
    merged_sequence_no: i64,
    occurred_at: DateTime<Utc>,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: "stream.branch.merged".to_string(),
        aggregate: "stream_branch".to_string(),
        aggregate_id: source.id.to_string(),
        stream_id: Some(source.stream_id),
        version: format!("merged:{target_branch_id}:{merged_sequence_no}"),
        occurred_at,
        payload: serde_json::json!({
            "source_branch_id": source.id,
            "target_branch_id": target_branch_id,
            "merged_sequence_no": merged_sequence_no,
            "branch": source,
        }),
    }
}

fn branch_event(
    event_type: &str,
    branch: &StreamBranch,
    occurred_at: DateTime<Utc>,
    version: String,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: event_type.to_string(),
        aggregate: "stream_branch".to_string(),
        aggregate_id: branch.id.to_string(),
        stream_id: Some(branch.stream_id),
        version,
        occurred_at,
        payload: serde_json::to_value(branch).expect("stream branch must serialize"),
    }
}

pub fn window_created(window: &WindowDefinition) -> StreamingChangedEvent {
    window_event("stream.window.created", window, window.created_at)
}

pub fn window_updated(window: &WindowDefinition) -> StreamingChangedEvent {
    window_event("stream.window.updated", window, window.updated_at)
}

fn window_event(
    event_type: &str,
    window: &WindowDefinition,
    occurred_at: DateTime<Utc>,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: event_type.to_string(),
        aggregate: "stream_window".to_string(),
        aggregate_id: window.id.to_string(),
        stream_id: None,
        version: occurred_at.timestamp_millis().to_string(),
        occurred_at,
        payload: serde_json::to_value(window).expect("window definition must serialize"),
    }
}

pub fn topology_created(topology: &TopologyDefinition) -> StreamingChangedEvent {
    topology_event("stream.topology.created", topology, topology.created_at)
}

pub fn topology_updated(topology: &TopologyDefinition) -> StreamingChangedEvent {
    topology_event("stream.topology.updated", topology, topology.updated_at)
}

fn topology_event(
    event_type: &str,
    topology: &TopologyDefinition,
    occurred_at: DateTime<Utc>,
) -> StreamingChangedEvent {
    StreamingChangedEvent {
        event_type: event_type.to_string(),
        aggregate: "stream_topology".to_string(),
        aggregate_id: topology.id.to_string(),
        stream_id: topology.source_stream_ids.first().copied(),
        version: occurred_at.timestamp_millis().to_string(),
        occurred_at,
        payload: serde_json::to_value(topology).expect("topology definition must serialize"),
    }
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;
    use event_bus_control::schema_registry::{
        CompatibilityMode, SchemaType, check_compatibility, validate_payload,
    };
    use serde_json::json;

    use super::*;
    use crate::models::{
        branch::StreamBranch,
        stream::{ConnectorBinding, StreamDefinition, StreamField, StreamProfile, StreamSchema},
        topology::{BackpressurePolicy, TopologyDefinition},
        window::WindowDefinition,
    };

    fn sample_stream() -> StreamDefinition {
        let created_at = Utc.with_ymd_and_hms(2026, 5, 3, 9, 0, 0).unwrap();
        let updated_at = Utc.with_ymd_and_hms(2026, 5, 3, 9, 5, 0).unwrap();
        StreamDefinition {
            id: Uuid::nil(),
            name: "orders".to_string(),
            description: "Orders stream".to_string(),
            status: "active".to_string(),
            schema: StreamSchema {
                fields: vec![StreamField {
                    name: "order_id".to_string(),
                    data_type: "string".to_string(),
                    nullable: false,
                    semantic_role: "primary_key".to_string(),
                }],
                primary_key: Some("order_id".to_string()),
                watermark_field: Some("event_time".to_string()),
            },
            source_binding: ConnectorBinding::default(),
            retention_hours: 72,
            partitions: 6,
            consistency_guarantee: "at-least-once".to_string(),
            stream_profile: StreamProfile::default(),
            schema_avro: None,
            schema_fingerprint: None,
            schema_compatibility_mode: "BACKWARD".to_string(),
            default_marking: None,
            stream_type: crate::models::stream::StreamType::default(),
            compression: false,
            ingest_consistency: crate::models::stream::StreamConsistency::AtLeastOnce,
            pipeline_consistency: crate::models::stream::StreamConsistency::AtLeastOnce,
            checkpoint_interval_ms: 2_000,
            kind: crate::models::stream_view::StreamKind::Ingest,
            created_at,
            updated_at,
        }
    }

    #[test]
    fn event_id_is_stable_for_same_aggregate_and_version() {
        let id1 = derive_event_id("stream", "abc", "1");
        let id2 = derive_event_id("stream", "abc", "1");
        let id3 = derive_event_id("stream", "abc", "2");

        assert_eq!(id1, id2);
        assert_ne!(id1, id3);
    }

    #[test]
    fn stream_event_uses_expected_topic_and_key() {
        let event = stream_updated(&sample_stream());
        let outbox = event.to_outbox_event();

        assert_eq!(outbox.topic, STREAMING_CHANGED_TOPIC);
        assert_eq!(outbox.aggregate, "stream");
        assert_eq!(outbox.aggregate_id, Uuid::nil().to_string());
        assert_eq!(event.event_type, "stream.updated");
    }

    #[test]
    fn stream_payload_matches_json_schema() {
        let event = stream_created(&sample_stream());
        let payload = serde_json::to_value(&event).unwrap();

        validate_payload(SchemaType::Json, STREAMING_CHANGED_V1_JSON_SCHEMA, &payload)
            .expect("streaming payload must match schema");
    }

    #[test]
    fn streaming_schema_evolution_allows_new_optional_field() {
        let next = r#"{
          "$schema": "http://json-schema.org/draft-07/schema#",
          "title": "DatasetStreamingChangedV2Candidate",
          "type": "object",
          "required": ["event_type", "aggregate", "aggregate_id", "version", "occurred_at", "payload"],
          "properties": {
            "event_type": { "type": "string" },
            "aggregate": { "type": "string" },
            "aggregate_id": { "type": "string" },
            "stream_id": { "type": ["string", "null"] },
            "version": { "type": "string" },
            "occurred_at": { "type": "string", "format": "date-time" },
            "payload": { "type": "object" },
            "trace_id": { "type": ["string", "null"] }
          },
          "additionalProperties": false
        }"#;

        check_compatibility(
            SchemaType::Json,
            STREAMING_CHANGED_V1_JSON_SCHEMA,
            next,
            CompatibilityMode::Backward,
        )
        .expect("adding an optional field must stay backward-compatible");
    }

    #[test]
    fn branch_merge_payload_carries_target_and_sequence() {
        let branch = StreamBranch {
            id: Uuid::nil(),
            stream_id: Uuid::now_v7(),
            name: "feature/x".to_string(),
            parent_branch_id: None,
            status: "active".to_string(),
            head_sequence_no: 17,
            dataset_branch_id: Some("feature-x".to_string()),
            description: "Feature branch".to_string(),
            created_by: "user@example.com".to_string(),
            created_at: Utc::now(),
            archived_at: None,
        };
        let target_id = Uuid::now_v7();
        let event = branch_merged(&branch, target_id, 23, Utc::now());
        let payload = event.payload.as_object().unwrap();

        assert_eq!(event.aggregate, "stream_branch");
        assert_eq!(payload.get("merged_sequence_no"), Some(&json!(23)));
        assert_eq!(payload.get("target_branch_id"), Some(&json!(target_id)));
    }

    #[test]
    fn topology_payload_matches_schema() {
        let now = Utc::now();
        let stream_id = Uuid::now_v7();
        let topology = TopologyDefinition {
            id: Uuid::now_v7(),
            name: "join-orders".to_string(),
            description: "Join topology".to_string(),
            status: "active".to_string(),
            nodes: Vec::new(),
            edges: Vec::new(),
            join_definition: None,
            cep_definition: None,
            backpressure_policy: BackpressurePolicy::default(),
            source_stream_ids: vec![stream_id],
            sink_bindings: vec![ConnectorBinding::default()],
            state_backend: "rocksdb".to_string(),
            checkpoint_interval_ms: 60_000,
            runtime_kind: "builtin".to_string(),
            flink_job_name: None,
            flink_deployment_name: None,
            flink_job_id: None,
            flink_namespace: None,
            consistency_guarantee: "at-least-once".to_string(),
            created_at: now,
            updated_at: now,
        };
        let event = topology_created(&topology);
        let payload = serde_json::to_value(&event).unwrap();

        validate_payload(SchemaType::Json, STREAMING_CHANGED_V1_JSON_SCHEMA, &payload)
            .expect("topology payload must match schema");
    }

    #[test]
    fn window_events_order_by_updated_at_version() {
        let created_at = Utc.with_ymd_and_hms(2026, 5, 3, 10, 0, 0).unwrap();
        let updated_at = Utc.with_ymd_and_hms(2026, 5, 3, 10, 5, 0).unwrap();
        let window = WindowDefinition {
            id: Uuid::now_v7(),
            name: "five-minute".to_string(),
            description: "5m window".to_string(),
            status: "active".to_string(),
            window_type: "tumbling".to_string(),
            duration_seconds: 300,
            slide_seconds: 300,
            session_gap_seconds: 180,
            allowed_lateness_seconds: 30,
            aggregation_keys: vec!["customer_id".to_string()],
            measure_fields: vec!["amount".to_string()],
            created_at,
            updated_at,
        };

        let created = window_created(&window);
        let updated = window_updated(&window);

        assert_ne!(
            created.to_outbox_event().event_id,
            updated.to_outbox_event().event_id
        );
        assert_eq!(updated.version, updated_at.timestamp_millis().to_string());
    }
}
