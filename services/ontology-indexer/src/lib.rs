//! `ontology-indexer` — Kafka consumer → `SearchBackend::index`
//!
//! ## Substrate scope
//!
//! This crate ships the **pure logic** that turns a domain event
//! payload (the JSON body that lands on `ontology.object.changed.v1`
//! / `ontology.action.applied.v1` after the EventRouter SMT) into a
//! [`storage_abstraction::IndexDoc`] suitable for
//! [`search_abstraction::SearchBackend::index`].
//!
//! The Kafka consumer loop and Prometheus metrics live behind the
//! `runtime` feature flag (enabled by `bin`). The pure logic is
//! always compiled so CI does not need `librdkafka` to validate the
//! deserialiser.
//!
//! ## Idempotency
//!
//! Per ADR-0028, every backend (Vespa, OpenSearch) is required to
//! discard a write whose `version` is older than the currently
//! indexed one for the same `(tenant, id)`. The deserialiser is
//! therefore allowed to be at-least-once; the backend is the
//! authority on staleness.
//!
//! The stable de-duplication key is `(tenant, id, version)`. We
//! surface it via [`IndexKey`] for metrics and tracing.

use serde::{Deserialize, Serialize};
use storage_abstraction::repositories::{IndexDoc, ObjectId, TenantId, TypeId};
use thiserror::Error;

pub mod schema;

#[cfg(feature = "runtime")]
pub mod runtime;

/// Topics this indexer subscribes to. Pinned here so a typo on the
/// wiring side is a compile error, not a silent silence.
pub mod topics {
    pub const ONTOLOGY_OBJECT_CHANGED_V1: &str = "ontology.object.changed.v1";
    pub const ONTOLOGY_ACTION_APPLIED_V1: &str = "ontology.action.applied.v1";

    /// Topic dedicated to backfill / re-index runs driven by the
    /// `workers-go/reindex` workflow. Same payload shape as the
    /// live topic; separate so backfill traffic does not starve
    /// the live consumer group.
    pub const ONTOLOGY_REINDEX_V1: &str = "ontology.reindex.v1";
}

/// Stable identity of an indexer write, used by metrics and the
/// Vespa `condition` / OpenSearch `if_seq_no` short-circuit.
#[derive(Debug, Clone, Eq, PartialEq, Hash)]
pub struct IndexKey {
    pub tenant: TenantId,
    pub id: ObjectId,
    pub version: u64,
}

/// Wire format of an `ontology.object.changed.v1` event payload.
///
/// This is the JSON the EventRouter SMT places on the value bus
/// (the `payload` column of `outbox.events`). Fields mirror the
/// canonical ontology object shape.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObjectChangedV1 {
    pub tenant: TenantId,
    pub id: ObjectId,
    pub type_id: TypeId,
    pub version: u64,
    /// Full materialised object payload to index. Backends decide
    /// what to expose via their schema.
    pub payload: serde_json::Value,
    /// Optional dense vector. When present the backend writes it
    /// to the registered `embedding` field.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub embedding: Option<Vec<f32>>,
    /// Tombstone flag. The producer side sets this on hard-deletes
    /// (the outbox row carries the same flag in the payload).
    #[serde(default)]
    pub deleted: bool,
}

#[derive(Debug, Error)]
pub enum DecodeError {
    #[error("invalid utf-8 payload")]
    Utf8(#[from] std::str::Utf8Error),
    #[error("invalid JSON payload: {0}")]
    Json(#[from] serde_json::Error),
}

/// Decision the consumer loop has to make per record.
#[derive(Debug, Clone)]
pub enum IndexAction {
    /// Forward to `SearchBackend::index(doc)`.
    Index { key: IndexKey, doc: IndexDoc },
    /// Forward to `SearchBackend::delete(tenant, id)`.
    Delete { key: IndexKey },
}

impl IndexAction {
    pub fn key(&self) -> &IndexKey {
        match self {
            IndexAction::Index { key, .. } | IndexAction::Delete { key } => key,
        }
    }
}

/// Pure decoder: bytes from Kafka → action for the backend.
pub fn decode_object_changed(bytes: &[u8]) -> Result<IndexAction, DecodeError> {
    let evt: ObjectChangedV1 = serde_json::from_slice(bytes)?;
    let key = IndexKey {
        tenant: evt.tenant.clone(),
        id: evt.id.clone(),
        version: evt.version,
    };
    if evt.deleted {
        Ok(IndexAction::Delete { key })
    } else {
        let doc = IndexDoc {
            tenant: evt.tenant,
            id: evt.id,
            type_id: evt.type_id,
            payload: evt.payload,
            version: evt.version,
            embedding: evt.embedding,
        };
        Ok(IndexAction::Index { key, doc })
    }
}

/// Resolve the `SEARCH_BACKEND` env var into a strongly typed
/// selector. Defaults to `vespa` (production target).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BackendKind {
    Vespa,
    OpenSearch,
}

impl BackendKind {
    pub fn from_env(value: Option<&str>) -> Self {
        match value.map(|v| v.trim().to_ascii_lowercase()).as_deref() {
            Some("opensearch") => BackendKind::OpenSearch,
            Some("vespa") | None | Some("") => BackendKind::Vespa,
            // Anything else is a misconfiguration; fail loudly.
            Some(other) => panic!("unknown SEARCH_BACKEND value: {other:?}"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn obj_changed(version: u64, deleted: bool) -> Vec<u8> {
        serde_json::to_vec(&json!({
            "tenant": "acme",
            "id": "obj-1",
            "type_id": "Aircraft",
            "version": version,
            "payload": { "tail_number": "EC-123" },
            "deleted": deleted,
        }))
        .unwrap()
    }

    #[test]
    fn decodes_index_action() {
        let action = decode_object_changed(&obj_changed(7, false)).unwrap();
        assert!(matches!(action, IndexAction::Index { .. }));
        assert_eq!(action.key().version, 7);
    }

    #[test]
    fn decodes_delete_action() {
        let action = decode_object_changed(&obj_changed(8, true)).unwrap();
        assert!(matches!(action, IndexAction::Delete { .. }));
        assert_eq!(action.key().version, 8);
    }

    #[test]
    fn rejects_invalid_payload() {
        let err = decode_object_changed(b"not json").unwrap_err();
        assert!(matches!(err, DecodeError::Json(_)));
    }

    #[test]
    fn backend_kind_from_env_defaults_vespa() {
        assert_eq!(BackendKind::from_env(None), BackendKind::Vespa);
        assert_eq!(BackendKind::from_env(Some("")), BackendKind::Vespa);
        assert_eq!(BackendKind::from_env(Some("VESPA")), BackendKind::Vespa);
        assert_eq!(
            BackendKind::from_env(Some("opensearch")),
            BackendKind::OpenSearch
        );
    }
}
