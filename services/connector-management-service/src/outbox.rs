use chrono::{DateTime, Utc};
use outbox::{OutboxEvent, OutboxResult, enqueue};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{Postgres, Transaction};
use uuid::Uuid;

use crate::models::connection::Connection;

pub const CONNECTION_CHANGED_TOPIC: &str = "connector.connection.changed.v1";
const EVENT_NAMESPACE: Uuid = Uuid::from_bytes([
    0x8d, 0x67, 0x93, 0x50, 0x82, 0xb1, 0x48, 0xc8, 0x9f, 0xd2, 0x06, 0x8a, 0xf9, 0x7a, 0x39, 0x34,
]);

pub const CONNECTION_CHANGED_V1_JSON_SCHEMA: &str = r#"{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "ConnectorConnectionChangedV1",
  "type": "object",
  "required": [
    "event_type",
    "aggregate",
    "aggregate_id",
    "version",
    "occurred_at",
    "name",
    "connector_type",
    "status",
    "payload"
  ],
  "properties": {
    "event_type": { "type": "string" },
    "aggregate": { "type": "string", "enum": ["connection"] },
    "aggregate_id": { "type": "string", "format": "uuid" },
    "version": { "type": "string", "minLength": 1 },
    "occurred_at": { "type": "string", "format": "date-time" },
    "name": { "type": "string" },
    "connector_type": { "type": "string" },
    "status": { "type": "string" },
    "payload": { "type": "object" }
  },
  "additionalProperties": false
}"#;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectionChangedEvent {
    pub event_type: String,
    pub aggregate: String,
    pub aggregate_id: String,
    pub version: String,
    pub occurred_at: DateTime<Utc>,
    pub name: String,
    pub connector_type: String,
    pub status: String,
    pub payload: Value,
}

impl ConnectionChangedEvent {
    pub fn to_outbox_event(&self) -> OutboxEvent {
        OutboxEvent::new(
            derive_event_id(&self.aggregate_id, &self.version),
            self.aggregate.clone(),
            self.aggregate_id.clone(),
            CONNECTION_CHANGED_TOPIC,
            serde_json::to_value(self).expect("connection outbox payload must serialize"),
        )
    }
}

pub fn derive_event_id(aggregate_id: &str, version: &str) -> Uuid {
    Uuid::new_v5(
        &EVENT_NAMESPACE,
        format!("connector-management/connection/{aggregate_id}@{version}").as_bytes(),
    )
}

pub async fn emit(
    tx: &mut Transaction<'_, Postgres>,
    event: &ConnectionChangedEvent,
) -> OutboxResult<()> {
    enqueue(tx, event.to_outbox_event()).await
}

pub fn created(connection: &Connection) -> ConnectionChangedEvent {
    connection_event("connection.created", connection, connection.created_at)
}

pub fn status_changed(connection: &Connection) -> ConnectionChangedEvent {
    connection_event(
        "connection.status_changed",
        connection,
        connection.updated_at,
    )
}

pub fn deleted(connection: &Connection, occurred_at: DateTime<Utc>) -> ConnectionChangedEvent {
    ConnectionChangedEvent {
        event_type: "connection.deleted".to_string(),
        aggregate: "connection".to_string(),
        aggregate_id: connection.id.to_string(),
        version: format!("deleted:{}", occurred_at.timestamp_millis()),
        occurred_at,
        name: connection.name.clone(),
        connector_type: connection.connector_type.clone(),
        status: "deleted".to_string(),
        payload: serde_json::json!({
            "connection": connection,
            "deleted": true,
        }),
    }
}

fn connection_event(
    event_type: &str,
    connection: &Connection,
    occurred_at: DateTime<Utc>,
) -> ConnectionChangedEvent {
    ConnectionChangedEvent {
        event_type: event_type.to_string(),
        aggregate: "connection".to_string(),
        aggregate_id: connection.id.to_string(),
        version: connection.updated_at.timestamp_millis().to_string(),
        occurred_at,
        name: connection.name.clone(),
        connector_type: connection.connector_type.clone(),
        status: connection.status.clone(),
        payload: serde_json::to_value(connection).expect("connection must serialize"),
    }
}

#[cfg(test)]
mod tests {
    use chrono::TimeZone;
    use event_bus_control::schema_registry::{
        CompatibilityMode, SchemaType, check_compatibility, validate_payload,
    };

    use super::*;

    fn sample_connection() -> Connection {
        let created_at = Utc.with_ymd_and_hms(2026, 5, 3, 9, 0, 0).unwrap();
        let updated_at = Utc.with_ymd_and_hms(2026, 5, 3, 9, 5, 0).unwrap();
        Connection {
            id: Uuid::nil(),
            name: "orders-kafka".to_string(),
            connector_type: "kafka".to_string(),
            config: serde_json::json!({"bootstrap_servers": "kafka:9092"}),
            status: "connected".to_string(),
            owner_id: Uuid::now_v7(),
            last_sync_at: None,
            created_at,
            updated_at,
        }
    }

    #[test]
    fn connection_event_uses_expected_topic_and_key() {
        let event = status_changed(&sample_connection());
        let outbox = event.to_outbox_event();

        assert_eq!(outbox.topic, CONNECTION_CHANGED_TOPIC);
        assert_eq!(outbox.aggregate, "connection");
        assert_eq!(outbox.aggregate_id, Uuid::nil().to_string());
    }

    #[test]
    fn connection_event_id_is_stable_for_same_version() {
        let id1 = derive_event_id("conn-1", "42");
        let id2 = derive_event_id("conn-1", "42");
        let id3 = derive_event_id("conn-1", "43");

        assert_eq!(id1, id2);
        assert_ne!(id1, id3);
    }

    #[test]
    fn payload_matches_json_schema() {
        let event = created(&sample_connection());
        let payload = serde_json::to_value(&event).unwrap();

        validate_payload(
            SchemaType::Json,
            CONNECTION_CHANGED_V1_JSON_SCHEMA,
            &payload,
        )
        .expect("connection payload must match schema");
    }

    #[test]
    fn schema_allows_backward_compatible_optional_field() {
        let next = r#"{
          "$schema": "http://json-schema.org/draft-07/schema#",
          "title": "ConnectorConnectionChangedV2Candidate",
          "type": "object",
          "required": ["event_type", "aggregate", "aggregate_id", "version", "occurred_at", "name", "connector_type", "status", "payload"],
          "properties": {
            "event_type": { "type": "string" },
            "aggregate": { "type": "string" },
            "aggregate_id": { "type": "string" },
            "version": { "type": "string" },
            "occurred_at": { "type": "string", "format": "date-time" },
            "name": { "type": "string" },
            "connector_type": { "type": "string" },
            "status": { "type": "string" },
            "payload": { "type": "object" },
            "trace_id": { "type": ["string", "null"] }
          },
          "additionalProperties": false
        }"#;

        check_compatibility(
            SchemaType::Json,
            CONNECTION_CHANGED_V1_JSON_SCHEMA,
            next,
            CompatibilityMode::Backward,
        )
        .expect("adding an optional field must stay backward-compatible");
    }
}
