//! OpenLineage v1 event helper for `lineage.events.v1`.
//!
//! Producers (pipeline-build, pipeline-schedule, workflow-automation,
//! ontology-actions) build a [`LineageEvent`] with [`LineageEvent::new`]
//! and hand it to [`enqueue`], which serialises an OpenLineage 1.x
//! `RunEvent` JSON payload, derives a deterministic event id, attaches
//! the canonical `ol-*` headers and forwards to [`crate::enqueue`]
//! inside the caller's transaction.
//!
//! Reference: <https://openlineage.io/spec/1-0-5/OpenLineage.json>.
//!
//! The Iceberg materialisation in `lineage-service::runtime::decode_event`
//! consumes this exact shape (`eventType`, `eventTime`, `producer`,
//! `schemaURL`, `run.runId`, `job.namespace`, `job.name`, `inputs[]`,
//! `outputs[]`).

use chrono::{DateTime, Utc};
use serde::Serialize;
use serde_json::{Map, Value, json};
use sqlx::{Postgres, Transaction};
use uuid::Uuid;

use crate::{OutboxEvent, OutboxResult};

/// Kafka topic that lineage-service consumes for Iceberg materialisation.
pub const TOPIC: &str = "lineage.events.v1";

/// OpenLineage spec URL referenced in every emitted event.
pub const SCHEMA_URL: &str = "https://openlineage.io/spec/1-0-5/OpenLineage.json";

/// Producer URI announced in the OL `producer` field. Pinned to the
/// repository so consumers can attribute events to OpenFoundry without
/// guessing.
pub const PRODUCER: &str = "https://github.com/open-foundry/openfoundry";

/// Aggregate name written to `outbox.events.aggregate` for lineage
/// records. Kept separate from the per-service aggregates ("build",
/// "schedule_run", "workflow_run", "ontology_object") so consumers can
/// filter the lineage stream without parsing payloads.
pub const AGGREGATE: &str = "lineage_run";

/// UUID-v5 namespace for deterministic OL event ids — flipping any
/// of `(run_id, event_type, event_time, namespace, job)` produces a
/// new id while replays of the same call collapse via the outbox PK.
const EVENT_ID_NAMESPACE: Uuid = Uuid::from_bytes([
    0x6e, 0x18, 0x5a, 0x0b, 0x0c, 0x77, 0x5c, 0x6d, 0x9d, 0x77, 0x4e, 0x14, 0xc1, 0x2a, 0x71, 0x83,
]);

/// OpenLineage event-type enum. The Iceberg consumer normalises this
/// to upper-case before storing, so the wire form is also upper-case.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LineageEventType {
    Start,
    Running,
    Complete,
    Fail,
    Abort,
}

impl LineageEventType {
    pub fn as_str(self) -> &'static str {
        match self {
            LineageEventType::Start => "START",
            LineageEventType::Running => "RUNNING",
            LineageEventType::Complete => "COMPLETE",
            LineageEventType::Fail => "FAIL",
            LineageEventType::Abort => "ABORT",
        }
    }

    /// `true` when the event terminates the run in OpenLineage's
    /// state machine (the lineage-service sets `completed_at` from
    /// the event time when this returns true).
    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            LineageEventType::Complete | LineageEventType::Fail | LineageEventType::Abort
        )
    }
}

/// Minimal OpenLineage dataset reference. `facets` is free-form JSON
/// (e.g. schema, dataSource) and may be omitted on first emit.
#[derive(Debug, Clone, Serialize)]
pub struct LineageDataset {
    pub namespace: String,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub facets: Option<Value>,
}

impl LineageDataset {
    pub fn new(namespace: impl Into<String>, name: impl Into<String>) -> Self {
        Self {
            namespace: namespace.into(),
            name: name.into(),
            facets: None,
        }
    }

    pub fn with_facets(mut self, facets: Value) -> Self {
        self.facets = Some(facets);
        self
    }
}

/// In-memory builder for an OpenLineage RunEvent. Hand to
/// [`enqueue`] once populated.
#[derive(Debug, Clone)]
pub struct LineageEvent {
    pub event_type: LineageEventType,
    pub event_time: DateTime<Utc>,
    pub run_id: Uuid,
    pub parent_run_id: Option<Uuid>,
    pub job_namespace: String,
    pub job_name: String,
    pub inputs: Vec<LineageDataset>,
    pub outputs: Vec<LineageDataset>,
    pub run_facets: Map<String, Value>,
    pub job_facets: Map<String, Value>,
}

impl LineageEvent {
    pub fn new(
        event_type: LineageEventType,
        run_id: Uuid,
        job_namespace: impl Into<String>,
        job_name: impl Into<String>,
    ) -> Self {
        Self {
            event_type,
            event_time: Utc::now(),
            run_id,
            parent_run_id: None,
            job_namespace: job_namespace.into(),
            job_name: job_name.into(),
            inputs: Vec::new(),
            outputs: Vec::new(),
            run_facets: Map::new(),
            job_facets: Map::new(),
        }
    }

    pub fn at(mut self, event_time: DateTime<Utc>) -> Self {
        self.event_time = event_time;
        self
    }

    pub fn with_parent(mut self, parent_run_id: Uuid) -> Self {
        self.parent_run_id = Some(parent_run_id);
        self
    }

    pub fn with_input(mut self, dataset: LineageDataset) -> Self {
        self.inputs.push(dataset);
        self
    }

    pub fn with_output(mut self, dataset: LineageDataset) -> Self {
        self.outputs.push(dataset);
        self
    }

    pub fn with_run_facet(mut self, name: impl Into<String>, facet: Value) -> Self {
        self.run_facets.insert(name.into(), facet);
        self
    }

    pub fn with_job_facet(mut self, name: impl Into<String>, facet: Value) -> Self {
        self.job_facets.insert(name.into(), facet);
        self
    }

    /// Render the OpenLineage 1.x JSON payload that downstream
    /// consumers (and the lineage-service Iceberg writer) decode.
    pub fn to_payload(&self) -> Value {
        let mut run = Map::new();
        run.insert("runId".to_string(), Value::String(self.run_id.to_string()));
        if let Some(parent) = self.parent_run_id {
            run.insert(
                "facets".to_string(),
                merge_run_facets(
                    &self.run_facets,
                    parent,
                    &self.job_namespace,
                    &self.job_name,
                ),
            );
        } else if !self.run_facets.is_empty() {
            run.insert("facets".to_string(), Value::Object(self.run_facets.clone()));
        }

        let mut job = Map::new();
        job.insert(
            "namespace".to_string(),
            Value::String(self.job_namespace.clone()),
        );
        job.insert("name".to_string(), Value::String(self.job_name.clone()));
        if !self.job_facets.is_empty() {
            job.insert("facets".to_string(), Value::Object(self.job_facets.clone()));
        }

        json!({
            "eventType": self.event_type.as_str(),
            "eventTime": self.event_time.to_rfc3339(),
            "producer": PRODUCER,
            "schemaURL": SCHEMA_URL,
            "run": Value::Object(run),
            "job": Value::Object(job),
            "inputs": self.inputs,
            "outputs": self.outputs,
        })
    }

    fn derive_event_id(&self) -> Uuid {
        let key = format!(
            "{}|{}|{}|{}|{}",
            self.run_id,
            self.event_type.as_str(),
            self.event_time.timestamp_micros(),
            self.job_namespace,
            self.job_name,
        );
        Uuid::new_v5(&EVENT_ID_NAMESPACE, key.as_bytes())
    }
}

/// Append `event` to `outbox.events` with topic `lineage.events.v1`.
///
/// Adds the canonical `ol-*` headers (`ol-run-id`, `ol-namespace`,
/// `ol-job`, plus `ol-parent-run-id` when present) so consumers can
/// filter without deserialising the payload, mirroring the pattern
/// documented in [`crate`].
pub async fn enqueue(tx: &mut Transaction<'_, Postgres>, event: LineageEvent) -> OutboxResult<()> {
    let event_id = event.derive_event_id();
    let payload = event.to_payload();
    let mut outbox_event = OutboxEvent::new(
        event_id,
        AGGREGATE,
        event.run_id.to_string(),
        TOPIC,
        payload,
    )
    .with_header("ol-run-id", event.run_id.to_string())
    .with_header("ol-namespace", event.job_namespace.clone())
    .with_header("ol-job", event.job_name.clone());
    if let Some(parent) = event.parent_run_id {
        outbox_event = outbox_event.with_header("ol-parent-run-id", parent.to_string());
    }
    crate::enqueue(tx, outbox_event).await
}

fn merge_run_facets(
    existing: &Map<String, Value>,
    parent_run_id: Uuid,
    parent_namespace: &str,
    parent_name: &str,
) -> Value {
    let mut facets = existing.clone();
    facets.insert(
        "parent".to_string(),
        json!({
            "_producer": PRODUCER,
            "_schemaURL": "https://openlineage.io/spec/facets/1-0-0/ParentRunFacet.json",
            "run": { "runId": parent_run_id.to_string() },
            "job": { "namespace": parent_namespace, "name": parent_name },
        }),
    );
    Value::Object(facets)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixed_event(event_type: LineageEventType) -> LineageEvent {
        let event_time = DateTime::parse_from_rfc3339("2026-05-03T10:11:12Z")
            .unwrap()
            .with_timezone(&Utc);
        LineageEvent::new(
            event_type,
            Uuid::parse_str("f1b9c3e0-2a6f-4d7a-8ad3-95f73b4a3d52").unwrap(),
            "of://pipelines",
            "pipeline.build",
        )
        .at(event_time)
    }

    #[test]
    fn topic_pin_matches_consumer_constant() {
        // Mirrors `lineage_service::kafka_to_iceberg::SOURCE_TOPIC` —
        // changing one without the other breaks the materialisation.
        assert_eq!(TOPIC, "lineage.events.v1");
    }

    #[test]
    fn payload_matches_openlineage_v1_required_fields() {
        let event = fixed_event(LineageEventType::Start)
            .with_input(LineageDataset::new("of://datasets", "source-a"))
            .with_output(LineageDataset::new("of://datasets", "target-b"));
        let payload = event.to_payload();
        assert_eq!(payload["eventType"], "START");
        assert_eq!(payload["producer"], PRODUCER);
        assert_eq!(payload["schemaURL"], SCHEMA_URL);
        assert_eq!(
            payload["run"]["runId"],
            "f1b9c3e0-2a6f-4d7a-8ad3-95f73b4a3d52"
        );
        assert_eq!(payload["job"]["namespace"], "of://pipelines");
        assert_eq!(payload["job"]["name"], "pipeline.build");
        assert_eq!(payload["inputs"][0]["name"], "source-a");
        assert_eq!(payload["outputs"][0]["name"], "target-b");
    }

    #[test]
    fn parent_run_id_emits_parent_run_facet() {
        let parent = Uuid::parse_str("c0a8d81f-30c4-4dde-bd3a-5dee4d1ce96f").unwrap();
        let event = fixed_event(LineageEventType::Start).with_parent(parent);
        let payload = event.to_payload();
        assert_eq!(
            payload["run"]["facets"]["parent"]["run"]["runId"],
            parent.to_string()
        );
    }

    #[test]
    fn event_id_is_deterministic_across_replays() {
        let a = fixed_event(LineageEventType::Complete).derive_event_id();
        let b = fixed_event(LineageEventType::Complete).derive_event_id();
        assert_eq!(a, b);
    }

    #[test]
    fn event_id_changes_when_event_type_changes() {
        let start = fixed_event(LineageEventType::Start).derive_event_id();
        let complete = fixed_event(LineageEventType::Complete).derive_event_id();
        assert_ne!(start, complete);
    }

    #[test]
    fn terminal_states_match_iceberg_consumer_contract() {
        assert!(LineageEventType::Complete.is_terminal());
        assert!(LineageEventType::Fail.is_terminal());
        assert!(LineageEventType::Abort.is_terminal());
        assert!(!LineageEventType::Start.is_terminal());
        assert!(!LineageEventType::Running.is_terminal());
    }
}
