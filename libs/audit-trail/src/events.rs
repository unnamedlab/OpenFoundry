//! Canonical audit event vocabulary published by OpenFoundry services.
//!
//! Every mutation worth recording in the security audit trail is
//! represented here as a variant of [`AuditEvent`]. Variants carry
//! resource-side identifiers (RIDs, project, markings snapshot at the
//! moment the event was emitted); request-side metadata (actor, IP,
//! user-agent, request id, latency) lives in [`AuditContext`] so the
//! same event types can be reused across HTTP, gRPC and background
//! workers.
//!
//! ## Wire format
//!
//! Events are serialised inside an [`AuditEnvelope`] that mirrors the
//! schema `audit-sink` consumes from
//! [`audit.events.v1`](crate::events::TOPIC). The envelope is the
//! Kafka record value; routing happens via the Postgres outbox table
//! per ADR-0022 (Debezium reads the WAL and the EventRouter SMT routes
//! by `topic`).
//!
//! ## Foundry audit categories
//!
//! Each event maps to one or more of the categories defined in
//! `Security & governance/.../Audit log categories.md` so an external
//! SIEM can `filter(categories.contains("dataExport"))` without
//! enumerating individual event names — the same trick Foundry's
//! `audit.3` schema uses.
//!
//! ## Producer contract
//!
//! ```ignore
//! let mut tx = pool.begin().await?;
//! // ... primary write ...
//! audit_trail::events::emit(
//!     &mut tx,
//!     AuditEvent::MediaSetCreated { resource_rid, name, project_rid, schema, ... },
//!     &ctx,
//! ).await?;
//! tx.commit().await?;  // atomic with the write
//! ```
//!
//! The helper deterministically derives the outbox `event_id` from
//! `(kind, resource_rid, request_id || correlation_id || rid)` so a
//! retried handler converges to the same row under the table's primary
//! key (consumers dedupe by `event_id`, ADR-0022 § Consumer-side
//! contract).

use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

/// Kafka topic that audit-sink subscribes to. Pinned here so a typo at
/// wiring time is a compile error rather than silent log loss.
pub const TOPIC: &str = "audit.events.v1";

/// UUIDv5 namespace for derived `event_id`s. Stable so a retried
/// handler computes the same id and the outbox primary key collapses
/// duplicate inserts.
const EVENT_NAMESPACE: Uuid = Uuid::from_bytes([
    0x4b, 0x32, 0x9e, 0x71, 0x6d, 0xa4, 0x4f, 0x8e, 0x8c, 0x40, 0xc1, 0xa3, 0xee, 0x12, 0x55, 0xaa,
]);

/// Foundry-aligned audit category. SIEM rules filter on `categories`
/// rather than event names so new variants land in existing alerts
/// without rule churn (Audit logs.md → "Service-agnostic queries").
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq, Hash)]
#[serde(rename_all = "camelCase")]
pub enum AuditCategory {
    DataCreate,
    DataDelete,
    DataExport,
    DataImport,
    DataLoad,
    DataUpdate,
    ManagementMarkings,
}

impl AuditCategory {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::DataCreate => "dataCreate",
            Self::DataDelete => "dataDelete",
            Self::DataExport => "dataExport",
            Self::DataImport => "dataImport",
            Self::DataLoad => "dataLoad",
            Self::DataUpdate => "dataUpdate",
            Self::ManagementMarkings => "managementMarkings",
        }
    }
}

/// Canonical media-set audit events.
///
/// Each variant carries the RIDs that identify the resource and the
/// markings that were in effect at the moment the event was recorded
/// (`markings_at_event` — the snapshot survives later marking changes,
/// so a SIEM can reconstruct what clearance the actor needed *then*).
/// Request-side metadata (actor, IP, user-agent, request id, latency)
/// lives in [`AuditContext`] so the same enum can be emitted from any
/// process surface.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "kind")]
pub enum AuditEvent {
    #[serde(rename = "media_set.created")]
    MediaSetCreated {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        name: String,
        schema: String,
        transaction_policy: String,
        #[serde(rename = "virtual")]
        virtual_: bool,
    },
    #[serde(rename = "media_set.deleted")]
    MediaSetDeleted {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
    },
    #[serde(rename = "media_set.markings_changed")]
    MediaSetMarkingsChanged {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        previous_markings: Vec<String>,
    },
    #[serde(rename = "media_set.retention_changed")]
    MediaSetRetentionChanged {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        previous_retention_seconds: i64,
        new_retention_seconds: i64,
    },
    #[serde(rename = "media_set.transaction_opened")]
    MediaSetTransactionOpened {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        transaction_rid: String,
        branch: String,
    },
    #[serde(rename = "media_set.transaction_committed")]
    MediaSetTransactionCommitted {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        transaction_rid: String,
        branch: String,
    },
    #[serde(rename = "media_set.transaction_aborted")]
    MediaSetTransactionAborted {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        transaction_rid: String,
        branch: String,
    },
    /// Captures any access pattern that materialises media bytes server-side
    /// (Foundry "Access patterns" — image transform, OCR, transcription, …).
    /// `kind` is the access-pattern label, `persistence` describes whether
    /// the result is cached / written back / ephemeral. Surface only;
    /// no access-pattern handler exists in this service yet.
    #[serde(rename = "media_set.access_pattern_invoked")]
    MediaSetAccessPatternInvoked {
        resource_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        access_pattern: String,
        persistence: String,
    },
    #[serde(rename = "media_item.uploaded")]
    MediaItemUploaded {
        resource_rid: String,
        media_set_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        path: String,
        mime_type: String,
        size_bytes: i64,
        sha256: String,
        transaction_rid: Option<String>,
    },
    #[serde(rename = "media_item.downloaded")]
    MediaItemDownloaded {
        resource_rid: String,
        media_set_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        size_bytes: i64,
        ttl_seconds: u64,
    },
    #[serde(rename = "media_item.deleted")]
    MediaItemDeleted {
        resource_rid: String,
        media_set_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        size_bytes: i64,
    },
    #[serde(rename = "media_item.marking_overridden")]
    MediaItemMarkingOverridden {
        resource_rid: String,
        media_set_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        previous_markings: Vec<String>,
    },
    #[serde(rename = "virtual_media_item.registered")]
    VirtualMediaItemRegistered {
        resource_rid: String,
        media_set_rid: String,
        project_rid: String,
        markings_at_event: Vec<String>,
        physical_path: String,
        item_path: String,
    },
}

impl AuditEvent {
    /// Stable wire identifier (e.g. `"media_set.created"`). Mirrored at
    /// the envelope's `kind` field so SIEM filters do not need to
    /// inspect the nested payload.
    pub fn kind(&self) -> &'static str {
        match self {
            Self::MediaSetCreated { .. } => "media_set.created",
            Self::MediaSetDeleted { .. } => "media_set.deleted",
            Self::MediaSetMarkingsChanged { .. } => "media_set.markings_changed",
            Self::MediaSetRetentionChanged { .. } => "media_set.retention_changed",
            Self::MediaSetTransactionOpened { .. } => "media_set.transaction_opened",
            Self::MediaSetTransactionCommitted { .. } => "media_set.transaction_committed",
            Self::MediaSetTransactionAborted { .. } => "media_set.transaction_aborted",
            Self::MediaSetAccessPatternInvoked { .. } => "media_set.access_pattern_invoked",
            Self::MediaItemUploaded { .. } => "media_item.uploaded",
            Self::MediaItemDownloaded { .. } => "media_item.downloaded",
            Self::MediaItemDeleted { .. } => "media_item.deleted",
            Self::MediaItemMarkingOverridden { .. } => "media_item.marking_overridden",
            Self::VirtualMediaItemRegistered { .. } => "virtual_media_item.registered",
        }
    }

    /// Foundry-style audit categories for this event. SIEM rules
    /// filter on these so new variants slot into existing alerts.
    pub fn categories(&self) -> &'static [AuditCategory] {
        match self {
            Self::MediaSetCreated { .. } => &[AuditCategory::DataCreate],
            Self::MediaSetDeleted { .. } => &[AuditCategory::DataDelete],
            Self::MediaSetMarkingsChanged { .. } => &[AuditCategory::ManagementMarkings],
            Self::MediaSetRetentionChanged { .. } => &[AuditCategory::DataUpdate],
            Self::MediaSetTransactionOpened { .. }
            | Self::MediaSetTransactionCommitted { .. }
            | Self::MediaSetTransactionAborted { .. } => &[AuditCategory::DataUpdate],
            Self::MediaSetAccessPatternInvoked { .. } => &[AuditCategory::DataLoad],
            Self::MediaItemUploaded { .. } => &[AuditCategory::DataImport],
            Self::MediaItemDownloaded { .. } => &[AuditCategory::DataExport],
            Self::MediaItemDeleted { .. } => &[AuditCategory::DataDelete],
            Self::MediaItemMarkingOverridden { .. } => &[AuditCategory::ManagementMarkings],
            Self::VirtualMediaItemRegistered { .. } => &[AuditCategory::DataCreate],
        }
    }

    /// RID this event is "about" — used as both the outbox aggregate id
    /// and the canonical resource link surfaced by the UI's audit
    /// viewer. For media-item events this is the item RID; for
    /// media-set events it is the set RID itself.
    pub fn resource_rid(&self) -> &str {
        match self {
            Self::MediaSetCreated { resource_rid, .. }
            | Self::MediaSetDeleted { resource_rid, .. }
            | Self::MediaSetMarkingsChanged { resource_rid, .. }
            | Self::MediaSetRetentionChanged { resource_rid, .. }
            | Self::MediaSetTransactionOpened { resource_rid, .. }
            | Self::MediaSetTransactionCommitted { resource_rid, .. }
            | Self::MediaSetTransactionAborted { resource_rid, .. }
            | Self::MediaSetAccessPatternInvoked { resource_rid, .. }
            | Self::MediaItemUploaded { resource_rid, .. }
            | Self::MediaItemDownloaded { resource_rid, .. }
            | Self::MediaItemDeleted { resource_rid, .. }
            | Self::MediaItemMarkingOverridden { resource_rid, .. }
            | Self::VirtualMediaItemRegistered { resource_rid, .. } => resource_rid,
        }
    }

    /// Project the resource lives under. Surfaced at the envelope top
    /// level so the audit-sink Iceberg table can be partitioned /
    /// filtered by tenant or project without unpacking `payload`.
    pub fn project_rid(&self) -> &str {
        match self {
            Self::MediaSetCreated { project_rid, .. }
            | Self::MediaSetDeleted { project_rid, .. }
            | Self::MediaSetMarkingsChanged { project_rid, .. }
            | Self::MediaSetRetentionChanged { project_rid, .. }
            | Self::MediaSetTransactionOpened { project_rid, .. }
            | Self::MediaSetTransactionCommitted { project_rid, .. }
            | Self::MediaSetTransactionAborted { project_rid, .. }
            | Self::MediaSetAccessPatternInvoked { project_rid, .. }
            | Self::MediaItemUploaded { project_rid, .. }
            | Self::MediaItemDownloaded { project_rid, .. }
            | Self::MediaItemDeleted { project_rid, .. }
            | Self::MediaItemMarkingOverridden { project_rid, .. }
            | Self::VirtualMediaItemRegistered { project_rid, .. } => project_rid,
        }
    }

    /// Markings in effect on the resource at emission time. Locked into
    /// the audit envelope so a future marking change does not retro-
    /// actively alter the historical clearance evaluation.
    pub fn markings_at_event(&self) -> &[String] {
        match self {
            Self::MediaSetCreated { markings_at_event, .. }
            | Self::MediaSetDeleted { markings_at_event, .. }
            | Self::MediaSetMarkingsChanged { markings_at_event, .. }
            | Self::MediaSetRetentionChanged { markings_at_event, .. }
            | Self::MediaSetTransactionOpened { markings_at_event, .. }
            | Self::MediaSetTransactionCommitted { markings_at_event, .. }
            | Self::MediaSetTransactionAborted { markings_at_event, .. }
            | Self::MediaSetAccessPatternInvoked { markings_at_event, .. }
            | Self::MediaItemUploaded { markings_at_event, .. }
            | Self::MediaItemDownloaded { markings_at_event, .. }
            | Self::MediaItemDeleted { markings_at_event, .. }
            | Self::MediaItemMarkingOverridden { markings_at_event, .. }
            | Self::VirtualMediaItemRegistered { markings_at_event, .. } => markings_at_event,
        }
    }
}

/// Request-side metadata captured by the audit middleware (or supplied
/// directly by background workers). All fields are optional so the
/// same struct serves call-sites that have rich HTTP context and the
/// ones that only know the actor.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct AuditContext {
    pub actor_id: Option<String>,
    pub ip: Option<String>,
    pub user_agent: Option<String>,
    pub request_id: Option<String>,
    pub correlation_id: Option<String>,
    pub latency_ms: Option<u64>,
    pub source_service: Option<String>,
}

impl AuditContext {
    /// Convenience: build a context that carries only the actor — the
    /// minimum a background worker needs to record a meaningful event.
    pub fn for_actor(actor_id: impl Into<String>) -> Self {
        Self {
            actor_id: Some(actor_id.into()),
            ..Self::default()
        }
    }

    pub fn with_request_id(mut self, request_id: impl Into<String>) -> Self {
        self.request_id = Some(request_id.into());
        self
    }

    pub fn with_correlation_id(mut self, correlation_id: impl Into<String>) -> Self {
        self.correlation_id = Some(correlation_id.into());
        self
    }

    pub fn with_ip(mut self, ip: impl Into<String>) -> Self {
        self.ip = Some(ip.into());
        self
    }

    pub fn with_user_agent(mut self, ua: impl Into<String>) -> Self {
        self.user_agent = Some(ua.into());
        self
    }

    pub fn with_latency_ms(mut self, latency_ms: u64) -> Self {
        self.latency_ms = Some(latency_ms);
        self
    }

    pub fn with_source_service(mut self, source: impl Into<String>) -> Self {
        self.source_service = Some(source.into());
        self
    }
}

/// Wire envelope landing on `audit.events.v1`. Mirrors the shape that
/// `audit-sink::AuditEnvelope` decodes, keeping the cross-service
/// schema centred in one place.
///
/// Fields:
/// * `event_id` — UUID v5 derived from event identity; consumers dedupe.
/// * `at` — Unix epoch microseconds (audit-sink's partition key).
/// * `kind` — copy of [`AuditEvent::kind`] hoisted to the top level.
/// * `categories` — Foundry audit categories for SIEM filtering.
/// * `resource_rid` / `project_rid` / `markings_at_event` — promoted to
///   the top level per `audit.3` schema guarantee #3.
/// * `actor_id`, `ip`, `user_agent`, `request_id`, `latency_ms` — from
///   the request context.
/// * `payload` — full event JSON (the discriminated union — preserved
///   so consumers that need detail unpack the variant body).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditEnvelope {
    pub event_id: Uuid,
    pub at: i64,
    pub kind: String,
    pub categories: Vec<String>,
    pub resource_rid: String,
    pub project_rid: String,
    pub markings_at_event: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub actor_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ip: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user_agent: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub correlation_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub latency_ms: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub source_service: Option<String>,
    pub occurred_at: DateTime<Utc>,
    pub payload: Value,
}

impl AuditEnvelope {
    /// Build an envelope for `event` with metadata from `ctx`. The
    /// `event_id` is derived deterministically from
    /// `(kind, resource_rid, request_id || correlation_id || resource_rid)`
    /// so a retried handler converges to the same outbox row.
    pub fn build(event: &AuditEvent, ctx: &AuditContext, occurred_at: DateTime<Utc>) -> Self {
        let kind = event.kind().to_string();
        let categories = event
            .categories()
            .iter()
            .map(|c| c.as_str().to_string())
            .collect();
        let resource_rid = event.resource_rid().to_string();
        let project_rid = event.project_rid().to_string();
        let markings_at_event = event.markings_at_event().to_vec();
        let payload = serde_json::to_value(event)
            .expect("AuditEvent variants are infallibly serializable");

        let identity_seed = ctx
            .request_id
            .as_deref()
            .or(ctx.correlation_id.as_deref())
            .unwrap_or(&resource_rid);
        let event_id = derive_event_id(&kind, &resource_rid, identity_seed);

        Self {
            event_id,
            at: occurred_at.timestamp_micros(),
            kind,
            categories,
            resource_rid,
            project_rid,
            markings_at_event,
            actor_id: ctx.actor_id.clone(),
            ip: ctx.ip.clone(),
            user_agent: ctx.user_agent.clone(),
            request_id: ctx.request_id.clone(),
            correlation_id: ctx.correlation_id.clone(),
            latency_ms: ctx.latency_ms,
            source_service: ctx.source_service.clone(),
            occurred_at,
            payload,
        }
    }
}

/// Deterministic event id derived from `(kind, resource_rid, identity_seed)`.
/// Same inputs → same UUIDv5; the outbox primary key collapses retries.
pub fn derive_event_id(kind: &str, resource_rid: &str, identity_seed: &str) -> Uuid {
    Uuid::new_v5(
        &EVENT_NAMESPACE,
        format!("audit/{kind}/{resource_rid}/{identity_seed}").as_bytes(),
    )
}

/// Outbox + Postgres-backed publisher. Re-exposed only when the
/// `outbox` feature is enabled — keeps the base crate dependency-light
/// for callers that only need the event vocabulary (Cedar, gRPC code-
/// generators, the front-end TypeScript bridge, …).
#[cfg(feature = "outbox")]
pub mod publisher {
    use super::{AuditContext, AuditEnvelope, AuditEvent, TOPIC};
    use chrono::Utc;
    use outbox::{OutboxEvent, OutboxResult, enqueue};
    use sqlx::{Postgres, Transaction};

    /// Build the [`OutboxEvent`] for an audit envelope. Aggregate is
    /// `audit_event`, aggregate id is the resource RID — that gives
    /// Debezium a stable Kafka partition key per resource so events for
    /// the same media set arrive in order on the consumer side.
    pub fn to_outbox_event(envelope: &AuditEnvelope) -> OutboxEvent {
        let payload = serde_json::to_value(envelope)
            .expect("AuditEnvelope is infallibly serializable");
        let mut event = OutboxEvent::new(
            envelope.event_id,
            "audit_event",
            envelope.resource_rid.clone(),
            TOPIC,
            payload,
        );
        if let Some(corr) = &envelope.correlation_id {
            event = event.with_header("ol-run-id", corr.clone());
        }
        if let Some(req) = &envelope.request_id {
            event = event.with_header("x-request-id", req.clone());
        }
        event
    }

    /// Build the audit envelope, append it to `outbox.events` inside
    /// the caller's transaction, and return. The caller owns the
    /// surrounding `tx.commit()` — the SQL mutation and the audit
    /// emission therefore land atomically (ADR-0022).
    pub async fn emit(
        tx: &mut Transaction<'_, Postgres>,
        event: AuditEvent,
        ctx: &AuditContext,
    ) -> OutboxResult<AuditEnvelope> {
        let envelope = AuditEnvelope::build(&event, ctx, Utc::now());
        let outbox_event = to_outbox_event(&envelope);
        enqueue(tx, outbox_event).await?;
        Ok(envelope)
    }
}

#[cfg(feature = "outbox")]
pub use publisher::{emit, to_outbox_event};

/// Promote the request-context fields onto an OpenLineage / lineage
/// header bag for callers that thread audit context through a
/// non-Postgres channel (e.g. tracing spans). The base crate produces
/// the map; callers decide how to attach it.
pub fn lineage_headers(envelope: &AuditEnvelope) -> HashMap<String, String> {
    let mut out = HashMap::new();
    if let Some(req) = &envelope.request_id {
        out.insert("x-request-id".to_string(), req.clone());
    }
    if let Some(corr) = &envelope.correlation_id {
        out.insert("ol-run-id".to_string(), corr.clone());
    }
    if let Some(actor) = &envelope.actor_id {
        out.insert("x-actor".to_string(), actor.clone());
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;
    use serde_json::json;

    fn sample_set_created() -> AuditEvent {
        AuditEvent::MediaSetCreated {
            resource_rid: "ri.foundry.main.media_set.abc".into(),
            project_rid: "ri.foundry.main.project.proj".into(),
            markings_at_event: vec!["public".into()],
            name: "fixture".into(),
            schema: "IMAGE".into(),
            transaction_policy: "TRANSACTIONLESS".into(),
            virtual_: false,
        }
    }

    #[test]
    fn topic_is_pinned_to_audit_events_v1() {
        assert_eq!(TOPIC, "audit.events.v1");
    }

    #[test]
    fn variant_kind_matches_serde_tag() {
        let event = sample_set_created();
        let payload = serde_json::to_value(&event).unwrap();
        assert_eq!(payload.get("kind").and_then(|v| v.as_str()), Some("media_set.created"));
        assert_eq!(event.kind(), "media_set.created");
    }

    #[test]
    fn envelope_promotes_top_level_fields() {
        let event = sample_set_created();
        let ctx = AuditContext::for_actor("user-1")
            .with_ip("10.0.0.5")
            .with_user_agent("OpenFoundry-Web/1.0")
            .with_request_id("req-42")
            .with_latency_ms(17)
            .with_source_service("media-sets-service");
        let occurred_at = Utc.with_ymd_and_hms(2026, 5, 3, 12, 0, 0).unwrap();
        let envelope = AuditEnvelope::build(&event, &ctx, occurred_at);

        assert_eq!(envelope.kind, "media_set.created");
        assert_eq!(envelope.resource_rid, "ri.foundry.main.media_set.abc");
        assert_eq!(envelope.project_rid, "ri.foundry.main.project.proj");
        assert_eq!(envelope.markings_at_event, vec!["public".to_string()]);
        assert_eq!(envelope.actor_id.as_deref(), Some("user-1"));
        assert_eq!(envelope.ip.as_deref(), Some("10.0.0.5"));
        assert_eq!(envelope.request_id.as_deref(), Some("req-42"));
        assert_eq!(envelope.latency_ms, Some(17));
        assert_eq!(envelope.source_service.as_deref(), Some("media-sets-service"));
        assert_eq!(envelope.categories, vec!["dataCreate"]);
        // payload is the full discriminated union body
        assert_eq!(envelope.payload.get("kind"), Some(&json!("media_set.created")));
        assert_eq!(envelope.payload.get("name"), Some(&json!("fixture")));
    }

    #[test]
    fn event_id_is_deterministic_per_request_id() {
        let event = sample_set_created();
        let ctx = AuditContext::for_actor("u").with_request_id("req-stable");
        let now = Utc.with_ymd_and_hms(2026, 5, 3, 12, 0, 0).unwrap();
        let a = AuditEnvelope::build(&event, &ctx, now);
        // Build twice — same request id, same identity → same event id.
        let b = AuditEnvelope::build(&event, &ctx, now);
        assert_eq!(a.event_id, b.event_id);

        // Different request id → different event id.
        let other_ctx = AuditContext::for_actor("u").with_request_id("req-other");
        let c = AuditEnvelope::build(&event, &other_ctx, now);
        assert_ne!(a.event_id, c.event_id);
    }

    #[test]
    fn event_id_falls_back_to_resource_rid_without_request_or_correlation() {
        let event = sample_set_created();
        let bare = AuditContext::default();
        let now = Utc::now();
        // Falls back to the resource_rid as the identity seed.
        let envelope = AuditEnvelope::build(&event, &bare, now);
        let expected = derive_event_id(
            event.kind(),
            event.resource_rid(),
            event.resource_rid(),
        );
        assert_eq!(envelope.event_id, expected);
    }

    #[test]
    fn categories_match_expected_foundry_mapping() {
        // Sanity check on the SIEM-facing category mapping. If new
        // variants land they should slot into existing categories
        // rather than coining bespoke names.
        assert_eq!(
            AuditEvent::MediaItemUploaded {
                resource_rid: "x".into(),
                media_set_rid: "y".into(),
                project_rid: "z".into(),
                markings_at_event: vec![],
                path: "p".into(),
                mime_type: "m".into(),
                size_bytes: 0,
                sha256: "".into(),
                transaction_rid: None,
            }
            .categories(),
            &[AuditCategory::DataImport],
        );
        assert_eq!(
            AuditEvent::MediaItemDownloaded {
                resource_rid: "x".into(),
                media_set_rid: "y".into(),
                project_rid: "z".into(),
                markings_at_event: vec![],
                size_bytes: 0,
                ttl_seconds: 0,
            }
            .categories(),
            &[AuditCategory::DataExport],
        );
        assert_eq!(
            AuditEvent::MediaSetMarkingsChanged {
                resource_rid: "x".into(),
                project_rid: "z".into(),
                markings_at_event: vec![],
                previous_markings: vec![],
            }
            .categories(),
            &[AuditCategory::ManagementMarkings],
        );
    }

    #[test]
    fn envelope_round_trips_through_serde() {
        let event = sample_set_created();
        let ctx = AuditContext::for_actor("u1");
        let envelope = AuditEnvelope::build(&event, &ctx, Utc::now());
        let s = serde_json::to_string(&envelope).expect("serialize");
        let back: AuditEnvelope = serde_json::from_str(&s).expect("deserialize");
        assert_eq!(back.event_id, envelope.event_id);
        assert_eq!(back.kind, envelope.kind);
        assert_eq!(back.payload, envelope.payload);
    }
}
