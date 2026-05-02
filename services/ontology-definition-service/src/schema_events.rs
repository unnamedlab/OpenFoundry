//! Schema-change events published to `ontology.schema.v1` (S1.6.e).
//!
//! Whenever a schema definition (object type, link type, action type,
//! interface, shared property type, …) is created, updated or deleted
//! the service publishes an envelope on JetStream subject
//! `ontology.schema.v1`. Downstream consumers — SDK generation,
//! data-asset catalog, search indexer — react to these events to
//! refresh their materialised views without polling Postgres.

use std::sync::Arc;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// JetStream subject used for schema-change events.
pub const SUBJECT: &str = "ontology.schema.v1";

/// CloudEvents `type` field used by the [`event_bus_control::Publisher`]
/// envelope. Distinct from [`SUBJECT`] so we can keep the routing
/// subject stable while iterating on the event type taxonomy.
pub const EVENT_TYPE: &str = "ontology.schema.changed";

/// Default `source` used in the CloudEvents envelope.
pub const SOURCE: &str = "ontology-definition-service";

/// Kind of schema entity that changed. Mirrors the table layout of
/// `pg-schemas.ontology_schema` so consumers can reason about the
/// payload shape without re-deriving it from `entity_id`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SchemaEntity {
    ObjectType,
    LinkType,
    ActionType,
    Interface,
    SharedPropertyType,
    Property,
    OntologyProject,
}

impl SchemaEntity {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::ObjectType => "object_type",
            Self::LinkType => "link_type",
            Self::ActionType => "action_type",
            Self::Interface => "interface",
            Self::SharedPropertyType => "shared_property_type",
            Self::Property => "property",
            Self::OntologyProject => "ontology_project",
        }
    }
}

/// Operation that produced the event.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SchemaOp {
    Created,
    Updated,
    Deleted,
}

/// Wire payload for `ontology.schema.v1`.
///
/// `payload` carries the full row as JSON when available so consumers
/// can populate caches without round-tripping back to Postgres.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SchemaChangedEvent {
    pub entity: SchemaEntity,
    pub op: SchemaOp,
    pub entity_id: uuid::Uuid,
    /// Logical name of the entity (e.g. object type `name` field).
    /// Helpful for log search and idempotency keys downstream.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    /// Tenant the change applies to. `None` for platform-global rows.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tenant_id: Option<uuid::Uuid>,
    pub at: DateTime<Utc>,
    /// Optional structured payload (the row itself).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub payload: Option<serde_json::Value>,
}

impl SchemaChangedEvent {
    pub fn new(entity: SchemaEntity, op: SchemaOp, entity_id: uuid::Uuid) -> Self {
        Self {
            entity,
            op,
            entity_id,
            name: None,
            tenant_id: None,
            at: Utc::now(),
            payload: None,
        }
    }

    pub fn with_name(mut self, name: impl Into<String>) -> Self {
        self.name = Some(name.into());
        self
    }

    pub fn with_tenant(mut self, tenant: uuid::Uuid) -> Self {
        self.tenant_id = Some(tenant);
        self
    }

    pub fn with_payload(mut self, payload: serde_json::Value) -> Self {
        self.payload = Some(payload);
        self
    }
}

/// Lightweight publisher that owns a NATS JetStream context.
///
/// Errors during publish are logged but **not** propagated to the
/// caller: schema mutations are durable in Postgres, the event is a
/// best-effort cache-invalidation hint. A fully transactional contract
/// would require an outbox table; that promotion is tracked as a
/// follow-up alongside the kernel-handler migration.
#[derive(Clone)]
pub struct SchemaPublisher {
    inner: Option<Arc<event_bus_control::Publisher>>,
}

impl SchemaPublisher {
    /// Build a publisher backed by a real JetStream context.
    pub fn new(publisher: event_bus_control::Publisher) -> Self {
        Self {
            inner: Some(Arc::new(publisher)),
        }
    }

    /// No-op publisher for CI / dev where NATS is not available.
    pub fn disabled() -> Self {
        Self { inner: None }
    }

    pub fn is_enabled(&self) -> bool {
        self.inner.is_some()
    }

    /// Publish a [`SchemaChangedEvent`] to [`SUBJECT`]. Logs and
    /// swallows errors — see struct-level docs for rationale.
    pub async fn publish(&self, event: SchemaChangedEvent) {
        let Some(publisher) = self.inner.as_ref() else {
            tracing::debug!(
                entity = event.entity.as_str(),
                op = ?event.op,
                "schema publisher disabled; skipping"
            );
            return;
        };
        if let Err(error) = publisher.publish(SUBJECT, EVENT_TYPE, &event).await {
            tracing::warn!(
                error = %error,
                entity = event.entity.as_str(),
                op = ?event.op,
                "ontology.schema.v1 publish failed"
            );
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn entity_kebab_serialization() {
        let s = serde_json::to_string(&SchemaEntity::SharedPropertyType).unwrap();
        assert_eq!(s, "\"shared_property_type\"");
    }

    #[test]
    fn op_serialization() {
        assert_eq!(
            serde_json::to_string(&SchemaOp::Created).unwrap(),
            "\"created\""
        );
    }

    #[test]
    fn event_round_trip_drops_optional_fields() {
        let id = uuid::Uuid::nil();
        let event = SchemaChangedEvent::new(SchemaEntity::ObjectType, SchemaOp::Created, id);
        let json = serde_json::to_value(&event).unwrap();
        let obj = json.as_object().unwrap();
        assert!(!obj.contains_key("name"));
        assert!(!obj.contains_key("tenant_id"));
        assert!(!obj.contains_key("payload"));
        assert_eq!(obj.get("entity").unwrap(), "object_type");
        assert_eq!(obj.get("op").unwrap(), "created");
    }

    #[test]
    fn disabled_publisher_is_a_noop() {
        let publisher = SchemaPublisher::disabled();
        assert!(!publisher.is_enabled());
        let event =
            SchemaChangedEvent::new(SchemaEntity::LinkType, SchemaOp::Updated, uuid::Uuid::nil());
        // Just ensures the call doesn't panic on the disabled path.
        futures_executor_block_on(publisher.publish(event));
    }

    fn futures_executor_block_on<F: std::future::Future>(fut: F) -> F::Output {
        // Avoid pulling `futures` as a dev-dep just for this; use the
        // Tokio current-thread runtime that's already on the test path.
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(fut)
    }
}
