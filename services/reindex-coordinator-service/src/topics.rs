//! Pinned Kafka topic constants.
//!
//! Topic names are part of the wire contract with
//! `services/ontology-indexer` (consumer of `ontology.reindex.v1`)
//! and with whatever control plane dispatches reindex requests
//! (producer of `ontology.reindex.requested.v1`). Pinning them as
//! `&'static str` constants makes a typo a compile error.

/// Input topic. The coordinator subscribes here. Payload is
/// JSON-serialised [`crate::event::ReindexRequestedV1`].
///
/// Replaces the Temporal task queue `openfoundry.reindex` (no caller
/// in-tree, see
/// `docs/architecture/refactor/reindex-worker-inventory.md` §1).
pub const ONTOLOGY_REINDEX_REQUESTED_V1: &str = "ontology.reindex.requested.v1";

/// Output (data plane) topic. One Kafka record per
/// re-indexed object. Same payload shape as
/// `ontology.object.changed.v1` so `ontology-indexer` can ingest
/// both with the same decoder.
pub const ONTOLOGY_REINDEX_V1: &str = "ontology.reindex.v1";

/// Output (control plane) topic. One record per terminal job
/// transition (`completed` / `failed` / `cancelled`). Payload is
/// JSON-serialised [`crate::event::ReindexCompletedV1`].
pub const ONTOLOGY_REINDEX_COMPLETED_V1: &str = "ontology.reindex.completed.v1";
