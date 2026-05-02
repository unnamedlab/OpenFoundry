//! Generic repository traits used by every OpenFoundry service that
//! persists ontology-shaped data, links, sessions, action history or
//! exposes search.
//!
//! ## Why this lives in `storage-abstraction`
//!
//! The traits are **storage-agnostic by design**: their concrete
//! implementations live in `libs/cassandra-kernel` (operational
//! store) and `libs/search-abstraction` (Vespa / OpenSearch). Putting
//! them next to `StorageBackend` keeps the two abstractions in the
//! same crate so services depend on a single `storage-abstraction`
//! and pick the wiring they need at composition time.
//!
//! ## What is intentionally **not** here
//!
//! There is no `OutboxStore` trait. The transactional outbox lives
//! in Postgres (`pg-policy.outbox.events`) and is written from the
//! same `sqlx` transaction as the business mutation in the handler.
//! Wrapping that with a repository trait would force every outbox
//! write through a layer of indirection that buys nothing — see
//! [ADR-0022](../../docs/architecture/adr/ADR-0022-transactional-outbox-postgres-debezium.md).
//!
//! ## Consistency
//!
//! Every read-side trait takes a [`ReadConsistency`]. Cassandra
//! implementations map this to `LOCAL_QUORUM` / `LOCAL_ONE` /
//! `BoundedStaleness` is a hint (the driver itself does not support
//! it natively; backends are free to implement it as a cache-TTL
//! gate). Search backends use the same enum for "wait for indexing"
//! semantics.
//!
//! ## Pagination
//!
//! [`Page`] is a token-based page. The token is opaque to callers
//! and is exactly what each backend needs (Cassandra paging state,
//! Vespa `continuation`, OpenSearch `search_after`).

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::Duration;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// IDs
// ---------------------------------------------------------------------------

/// Stable identifier for a stored object. Backends decide the lexical
/// shape (UUIDv7, TimeUUID, …); the trait surface is opaque.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct ObjectId(pub String);

impl From<&str> for ObjectId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Stable identifier for an object type (a node label in ontology
/// terms). For example `aircraft`, `flight_event`, `customer`.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct TypeId(pub String);

impl From<&str> for TypeId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Stable identifier for a link type between two object types.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct LinkTypeId(pub String);

impl From<&str> for LinkTypeId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Tenant scope. Every read and write is implicitly tenant-scoped;
/// backends must never serve cross-tenant data.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct TenantId(pub String);

impl From<&str> for TenantId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Owner of an object (the principal that created it). Maps to the
/// `objects_by_owner` Cassandra access pattern (S1.1.b).
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct OwnerId(pub String);

impl From<&str> for OwnerId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Classification / marking label that gates access to an object.
/// Maps to the `objects_by_marking` Cassandra access pattern (S1.1.b).
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct MarkingId(pub String);

impl From<&str> for MarkingId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

// ---------------------------------------------------------------------------
// Domain payloads
// ---------------------------------------------------------------------------

/// A persisted ontology object. The payload is opaque (`serde_json::Value`)
/// because the schema is per type and lives in [`SchemaStore`]; this
/// trait surface is intentionally schema-free.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Object {
    /// Tenant the object belongs to.
    pub tenant: TenantId,
    /// Object identifier.
    pub id: ObjectId,
    /// Type of the object.
    pub type_id: TypeId,
    /// Optimistic-locking version. Monotonic per `(tenant, id)`.
    pub version: u64,
    /// JSON payload. Validation against [`Schema`] is the caller's job.
    pub payload: serde_json::Value,
    /// Server timestamp of the last write, in milliseconds.
    pub updated_at_ms: i64,
    /// Principal that owns the object. Optional for backwards
    /// compatibility with backends that have not yet populated it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner: Option<OwnerId>,
    /// Marking labels gating access to the object. Empty set is
    /// the safe default (no marking attached).
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub markings: Vec<MarkingId>,
}

/// A directed link between two objects.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Link {
    /// Tenant the link belongs to.
    pub tenant: TenantId,
    /// Link type.
    pub link_type: LinkTypeId,
    /// Source object.
    pub from: ObjectId,
    /// Destination object.
    pub to: ObjectId,
    /// Optional small payload (rarely used; large blobs belong on the
    /// adjacent `Object`).
    pub payload: Option<serde_json::Value>,
    /// Server timestamp.
    pub created_at_ms: i64,
}

/// Versioned schema for a [`TypeId`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Schema {
    /// Type the schema describes.
    pub type_id: TypeId,
    /// Monotonic version. Incremented every time the schema changes.
    pub version: u32,
    /// JSON Schema describing the object payload.
    pub json_schema: serde_json::Value,
    /// Server timestamp.
    pub created_at_ms: i64,
}

/// A user / service-account session.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Session {
    /// Tenant the session belongs to.
    pub tenant: TenantId,
    /// Opaque session identifier (typically a TimeUUID).
    pub id: String,
    /// Subject the session authenticates.
    pub subject: String,
    /// Free-form attributes (scopes, MFA level, …).
    pub attributes: HashMap<String, String>,
    /// Issued-at timestamp (ms).
    pub issued_at_ms: i64,
    /// Absolute expiry (ms); the backend should also enforce TTL.
    pub expires_at_ms: i64,
}

/// A single ontology action log entry.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ActionLogEntry {
    /// Tenant scope.
    pub tenant: TenantId,
    /// Action identifier (TimeUUID).
    pub action_id: String,
    /// Action kind, e.g. `create_aircraft`, `merge_records`.
    pub kind: String,
    /// Subject (user or service-account) that issued the action.
    pub subject: String,
    /// Object touched by the action, if any.
    pub object: Option<ObjectId>,
    /// JSON payload describing the action's parameters or diff.
    pub payload: serde_json::Value,
    /// Server timestamp (ms).
    pub recorded_at_ms: i64,
}

// ---------------------------------------------------------------------------
// Pagination, consistency, outcomes
// ---------------------------------------------------------------------------

/// Token-based pagination. The token is opaque and may only be used
/// against the same backend that produced it.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Page {
    /// Maximum rows the caller wants. Backends may return fewer.
    pub size: u32,
    /// Continuation token returned by the previous page, or `None`
    /// to start at the beginning.
    pub token: Option<String>,
}

/// Result of a paged list call.
#[derive(Debug, Clone)]
pub struct PagedResult<T> {
    /// Items in this page.
    pub items: Vec<T>,
    /// Token for the next page, or `None` if exhausted.
    pub next_token: Option<String>,
}

/// Consistency hint passed to read-side calls.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReadConsistency {
    /// LOCAL_QUORUM on Cassandra; "wait for indexing" on search.
    Strong,
    /// LOCAL_ONE on Cassandra; immediate / cached on search.
    Eventual,
    /// Eventual but with an upper bound on staleness. Backends that
    /// cannot honour this fall back to [`ReadConsistency::Eventual`].
    BoundedStaleness(Duration),
}

impl Default for ReadConsistency {
    fn default() -> Self {
        Self::Strong
    }
}

/// Outcome of an optimistic-concurrency `put`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PutOutcome {
    /// First insert; previous version did not exist.
    Inserted,
    /// Update applied; the persisted row now has `new_version`.
    Updated {
        /// Version the row had before the update.
        previous_version: u64,
        /// Version the row has after the update.
        new_version: u64,
    },
    /// Optimistic lock failed — `expected_version` did not match.
    /// Caller must re-read and retry.
    VersionConflict {
        /// Version the caller declared.
        expected_version: u64,
        /// Version actually stored at write time.
        actual_version: u64,
    },
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

/// Errors surfaced by every repository trait.
#[derive(Debug, thiserror::Error)]
pub enum RepoError {
    /// Object / link / session was not found.
    #[error("not found: {0}")]
    NotFound(String),

    /// Caller passed an argument the backend cannot satisfy
    /// (malformed token, payload too large, …).
    #[error("invalid argument: {0}")]
    InvalidArgument(String),

    /// Tenant scope violation. Backends raise this rather than
    /// returning empty results so callers do not silently confuse a
    /// missing object with a missing tenant.
    #[error("tenant scope violation: {0}")]
    TenantScope(String),

    /// Backend-level failure (network, timeout, decode, …). Wraps
    /// the original error message; the repository contract does not
    /// promise to expose backend-typed errors.
    #[error("backend error: {0}")]
    Backend(String),
}

/// Result alias.
pub type RepoResult<T> = Result<T, RepoError>;

// ---------------------------------------------------------------------------
// Traits
// ---------------------------------------------------------------------------

/// CRUD over [`Object`] with optimistic concurrency.
#[async_trait]
pub trait ObjectStore: Send + Sync {
    /// Fetch one object by `(tenant, id)`. Returns `Ok(None)` for a
    /// genuine miss; reserves [`RepoError::NotFound`] for the case
    /// where the *type* is unknown.
    async fn get(
        &self,
        tenant: &TenantId,
        id: &ObjectId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Object>>;

    /// Insert or update with optimistic concurrency.
    /// `expected_version = None` ⇒ insert-only (fails on conflict).
    async fn put(
        &self,
        obj: Object,
        expected_version: Option<u64>,
    ) -> RepoResult<PutOutcome>;

    /// Delete by `(tenant, id)`. Returns `Ok(false)` if the object
    /// did not exist — deletes are idempotent.
    async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool>;

    /// Page through every object of a given type within a tenant.
    /// Backends must enforce stable ordering across pages (typically
    /// by clustering key DESC).
    async fn list_by_type(
        &self,
        tenant: &TenantId,
        type_id: &TypeId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>>;

    /// Page through every object owned by `owner` within a tenant.
    /// Maps to the `objects_by_owner` Cassandra access pattern
    /// (S1.1.b). Default impl walks `list_by_type` results across
    /// all types for backends that have not specialised it; this is
    /// **not** acceptable for production traffic — Cassandra impls
    /// must override.
    async fn list_by_owner(
        &self,
        _tenant: &TenantId,
        _owner: &OwnerId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        Err(RepoError::Backend(
            "list_by_owner not implemented by this backend".into(),
        ))
    }

    /// Page through every object bearing `marking` within a tenant.
    /// Maps to the `objects_by_marking` Cassandra access pattern
    /// (S1.1.b). Same caveat as [`ObjectStore::list_by_owner`]
    /// regarding production use of the default impl.
    async fn list_by_marking(
        &self,
        _tenant: &TenantId,
        _marking: &MarkingId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        Err(RepoError::Backend(
            "list_by_marking not implemented by this backend".into(),
        ))
    }
}

/// CRUD over [`Link`].
#[async_trait]
pub trait LinkStore: Send + Sync {
    /// Persist a link. Links are immutable: a second `put` of the
    /// same `(tenant, link_type, from, to)` triple is a no-op.
    async fn put(&self, link: Link) -> RepoResult<()>;

    /// Delete a link triple. Returns `Ok(false)` if absent.
    async fn delete(
        &self,
        tenant: &TenantId,
        link_type: &LinkTypeId,
        from: &ObjectId,
        to: &ObjectId,
    ) -> RepoResult<bool>;

    /// All outgoing links of a given type from `from`.
    async fn list_outgoing(
        &self,
        tenant: &TenantId,
        link_type: &LinkTypeId,
        from: &ObjectId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Link>>;

    /// All incoming links of a given type into `to`.
    async fn list_incoming(
        &self,
        tenant: &TenantId,
        link_type: &LinkTypeId,
        to: &ObjectId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Link>>;
}

/// Per-type schema registry.
#[async_trait]
pub trait SchemaStore: Send + Sync {
    /// Get the latest schema for a type, or `None` if unknown.
    async fn get_latest(
        &self,
        type_id: &TypeId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Schema>>;

    /// Get a specific version.
    async fn get_version(
        &self,
        type_id: &TypeId,
        version: u32,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Schema>>;

    /// Append a new schema version. Implementations must reject any
    /// version ≤ the latest known one.
    async fn put(&self, schema: Schema) -> RepoResult<()>;
}

/// Session storage. Backed by the `auth_runtime` keyspace in
/// production; sessions carry a TTL enforced at the storage layer.
#[async_trait]
pub trait SessionStore: Send + Sync {
    /// Fetch by session id. Expired sessions return `Ok(None)`.
    async fn get(
        &self,
        tenant: &TenantId,
        id: &str,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Session>>;

    /// Persist a session. Implementations should set the storage TTL
    /// to `expires_at_ms - now`.
    async fn put(&self, session: Session) -> RepoResult<()>;

    /// Revoke a session immediately, regardless of its TTL.
    async fn revoke(&self, tenant: &TenantId, id: &str) -> RepoResult<bool>;
}

/// Append-only action log.
#[async_trait]
pub trait ActionLogStore: Send + Sync {
    /// Append one entry. The append is atomic and idempotent on
    /// `(tenant, action_id)`.
    async fn append(&self, entry: ActionLogEntry) -> RepoResult<()>;

    /// Page through actions for a tenant in time-DESC order.
    async fn list_recent(
        &self,
        tenant: &TenantId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>>;

    /// Page through actions touching a specific object.
    async fn list_for_object(
        &self,
        tenant: &TenantId,
        object: &ObjectId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>>;
}

// ---------------------------------------------------------------------------
// Search backend
// ---------------------------------------------------------------------------

/// Free-form search query as received from the API.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchQuery {
    /// Tenant scope; mandatory.
    pub tenant: TenantId,
    /// Optional restriction to a single type.
    pub type_id: Option<TypeId>,
    /// Free-form lexical query (`q=`).
    pub q: Option<String>,
    /// Equality filters on payload fields. Backends decide which
    /// fields are filterable based on their schema/index definition.
    pub filters: HashMap<String, String>,
    /// Page size & token.
    pub page: Page,
}

/// One hit returned by [`SearchBackend::search`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchHit {
    /// Object identifier.
    pub id: ObjectId,
    /// Object type.
    pub type_id: TypeId,
    /// Backend-provided relevance score.
    pub score: f32,
    /// Optional inline payload snippet for UI rendering.
    pub snippet: Option<serde_json::Value>,
}

/// Indexable payload pushed to the search backend by the funnel.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IndexDoc {
    /// Tenant the document belongs to.
    pub tenant: TenantId,
    /// Object identifier.
    pub id: ObjectId,
    /// Object type.
    pub type_id: TypeId,
    /// Full payload to index.
    pub payload: serde_json::Value,
    /// Source-of-truth version (used by the backend to discard
    /// out-of-order updates).
    pub version: u64,
    /// Optional dense vector for ANN / kNN retrieval. Backends with
    /// a registered vector field index it under that field name.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub embedding: Option<Vec<f32>>,
}

/// kNN / ANN query.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VectorQuery {
    /// Tenant scope; mandatory.
    pub tenant: TenantId,
    /// Optional restriction to a single type.
    pub type_id: Option<TypeId>,
    /// Vector to compare against (must match the dimension declared
    /// in the backend's schema).
    pub embedding: Vec<f32>,
    /// Number of neighbours to return.
    pub k: usize,
    /// Equality filters applied **before** the kNN search.
    pub filters: HashMap<String, String>,
}

/// Outcome of [`SearchBackend::bulk_index`].
#[derive(Debug, Clone, Default)]
pub struct BulkOutcome {
    /// Number of documents successfully indexed.
    pub indexed: usize,
    /// Per-document failures (id + reason).
    pub failed: Vec<(ObjectId, String)>,
}

/// Search-engine abstraction. Implemented by Vespa (production) and
/// OpenSearch (dev/CI), per
/// [ADR-0028](../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md).
#[async_trait]
pub trait SearchBackend: Send + Sync {
    /// Run a query. The `consistency` hint controls whether the
    /// backend should wait for in-flight indexing to flush.
    async fn search(
        &self,
        query: SearchQuery,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<SearchHit>>;

    /// Index (or re-index) one document. Implementations must
    /// discard a write whose `version` is older than the currently
    /// indexed one for the same `(tenant, id)`.
    async fn index(&self, doc: IndexDoc) -> RepoResult<()>;

    /// Remove a document from the index.
    async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool>;

    /// kNN / ANN search over the document `embedding` field.
    /// Default impl returns [`RepoError::Backend`] so that backends
    /// without vector support fail loudly.
    async fn search_vector(
        &self,
        _query: VectorQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<Vec<SearchHit>> {
        Err(RepoError::Backend(
            "vector search not supported by this backend".into(),
        ))
    }

    /// Bulk-index a batch of documents. Default impl loops over
    /// [`SearchBackend::index`], collecting per-document failures —
    /// good enough for in-memory and small batches; backends with
    /// native bulk APIs (Vespa `/document/v1`, OpenSearch `_bulk`)
    /// should override for throughput.
    async fn bulk_index(&self, docs: Vec<IndexDoc>) -> RepoResult<BulkOutcome> {
        let mut out = BulkOutcome::default();
        for d in docs {
            let id = d.id.clone();
            match self.index(d).await {
                Ok(()) => out.indexed += 1,
                Err(e) => out.failed.push((id, e.to_string())),
            }
        }
        Ok(out)
    }
}

// ---------------------------------------------------------------------------
// In-memory NOOP implementations
// ---------------------------------------------------------------------------

/// Drop-in fakes for unit tests. Never use in production.
///
/// Each store keeps its own `Mutex<HashMap>` — concurrency-safe but
/// not partition-tolerant; the goal is to let unit tests exercise
/// the trait surface without spinning up Cassandra or Vespa.
pub mod noop {
    use super::*;

    /// In-memory [`ObjectStore`].
    #[derive(Default)]
    pub struct InMemoryObjectStore {
        rows: Mutex<HashMap<(TenantId, ObjectId), Object>>,
    }

    #[async_trait]
    impl ObjectStore for InMemoryObjectStore {
        async fn get(
            &self,
            tenant: &TenantId,
            id: &ObjectId,
            _consistency: ReadConsistency,
        ) -> RepoResult<Option<Object>> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .get(&(tenant.clone(), id.clone()))
                .cloned())
        }

        async fn put(
            &self,
            obj: Object,
            expected_version: Option<u64>,
        ) -> RepoResult<PutOutcome> {
            let mut rows = self.rows.lock().unwrap();
            let key = (obj.tenant.clone(), obj.id.clone());
            match (rows.get(&key).cloned(), expected_version) {
                (None, None) | (None, Some(0)) => {
                    let mut to_insert = obj.clone();
                    to_insert.version = 1;
                    rows.insert(key, to_insert);
                    Ok(PutOutcome::Inserted)
                }
                (None, Some(v)) => Ok(PutOutcome::VersionConflict {
                    expected_version: v,
                    actual_version: 0,
                }),
                (Some(existing), expected) => {
                    if let Some(v) = expected {
                        if v != existing.version {
                            return Ok(PutOutcome::VersionConflict {
                                expected_version: v,
                                actual_version: existing.version,
                            });
                        }
                    }
                    let new_version = existing.version + 1;
                    let mut to_update = obj.clone();
                    to_update.version = new_version;
                    rows.insert(key, to_update);
                    Ok(PutOutcome::Updated {
                        previous_version: existing.version,
                        new_version,
                    })
                }
            }
        }

        async fn delete(
            &self,
            tenant: &TenantId,
            id: &ObjectId,
        ) -> RepoResult<bool> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .remove(&(tenant.clone(), id.clone()))
                .is_some())
        }

        async fn list_by_type(
            &self,
            tenant: &TenantId,
            type_id: &TypeId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Object>> {
            let rows = self.rows.lock().unwrap();
            let mut items: Vec<Object> = rows
                .values()
                .filter(|o| &o.tenant == tenant && &o.type_id == type_id)
                .cloned()
                .collect();
            items.sort_by(|a, b| b.updated_at_ms.cmp(&a.updated_at_ms));
            let limit = page.size.max(1) as usize;
            items.truncate(limit);
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }

        async fn list_by_owner(
            &self,
            tenant: &TenantId,
            owner: &OwnerId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Object>> {
            let rows = self.rows.lock().unwrap();
            let mut items: Vec<Object> = rows
                .values()
                .filter(|o| &o.tenant == tenant && o.owner.as_ref() == Some(owner))
                .cloned()
                .collect();
            items.sort_by(|a, b| b.updated_at_ms.cmp(&a.updated_at_ms));
            items.truncate(page.size.max(1) as usize);
            Ok(PagedResult { items, next_token: None })
        }

        async fn list_by_marking(
            &self,
            tenant: &TenantId,
            marking: &MarkingId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Object>> {
            let rows = self.rows.lock().unwrap();
            let mut items: Vec<Object> = rows
                .values()
                .filter(|o| {
                    &o.tenant == tenant && o.markings.iter().any(|m| m == marking)
                })
                .cloned()
                .collect();
            items.sort_by(|a, b| b.updated_at_ms.cmp(&a.updated_at_ms));
            items.truncate(page.size.max(1) as usize);
            Ok(PagedResult { items, next_token: None })
        }
    }

    /// In-memory [`LinkStore`].
    #[derive(Default)]
    pub struct InMemoryLinkStore {
        rows: Mutex<Vec<Link>>,
    }

    #[async_trait]
    impl LinkStore for InMemoryLinkStore {
        async fn put(&self, link: Link) -> RepoResult<()> {
            let mut rows = self.rows.lock().unwrap();
            let exists = rows.iter().any(|l| {
                l.tenant == link.tenant
                    && l.link_type == link.link_type
                    && l.from == link.from
                    && l.to == link.to
            });
            if !exists {
                rows.push(link);
            }
            Ok(())
        }

        async fn delete(
            &self,
            tenant: &TenantId,
            link_type: &LinkTypeId,
            from: &ObjectId,
            to: &ObjectId,
        ) -> RepoResult<bool> {
            let mut rows = self.rows.lock().unwrap();
            let len_before = rows.len();
            rows.retain(|l| {
                !(l.tenant == *tenant
                    && l.link_type == *link_type
                    && l.from == *from
                    && l.to == *to)
            });
            Ok(rows.len() != len_before)
        }

        async fn list_outgoing(
            &self,
            tenant: &TenantId,
            link_type: &LinkTypeId,
            from: &ObjectId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Link>> {
            let rows = self.rows.lock().unwrap();
            let items: Vec<Link> = rows
                .iter()
                .filter(|l| {
                    l.tenant == *tenant
                        && l.link_type == *link_type
                        && l.from == *from
                })
                .take(page.size.max(1) as usize)
                .cloned()
                .collect();
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }

        async fn list_incoming(
            &self,
            tenant: &TenantId,
            link_type: &LinkTypeId,
            to: &ObjectId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Link>> {
            let rows = self.rows.lock().unwrap();
            let items: Vec<Link> = rows
                .iter()
                .filter(|l| {
                    l.tenant == *tenant
                        && l.link_type == *link_type
                        && l.to == *to
                })
                .take(page.size.max(1) as usize)
                .cloned()
                .collect();
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }
    }

    /// In-memory [`SchemaStore`].
    #[derive(Default)]
    pub struct InMemorySchemaStore {
        rows: Mutex<HashMap<(TypeId, u32), Schema>>,
    }

    #[async_trait]
    impl SchemaStore for InMemorySchemaStore {
        async fn get_latest(
            &self,
            type_id: &TypeId,
            _consistency: ReadConsistency,
        ) -> RepoResult<Option<Schema>> {
            let rows = self.rows.lock().unwrap();
            Ok(rows
                .iter()
                .filter(|((t, _), _)| t == type_id)
                .max_by_key(|((_, v), _)| *v)
                .map(|(_, s)| s.clone()))
        }

        async fn get_version(
            &self,
            type_id: &TypeId,
            version: u32,
            _consistency: ReadConsistency,
        ) -> RepoResult<Option<Schema>> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .get(&(type_id.clone(), version))
                .cloned())
        }

        async fn put(&self, schema: Schema) -> RepoResult<()> {
            let mut rows = self.rows.lock().unwrap();
            let latest = rows
                .iter()
                .filter(|((t, _), _)| t == &schema.type_id)
                .map(|((_, v), _)| *v)
                .max()
                .unwrap_or(0);
            if schema.version <= latest {
                return Err(RepoError::InvalidArgument(format!(
                    "schema version {} not greater than latest {}",
                    schema.version, latest
                )));
            }
            rows.insert((schema.type_id.clone(), schema.version), schema);
            Ok(())
        }
    }

    /// In-memory [`SessionStore`].
    #[derive(Default)]
    pub struct InMemorySessionStore {
        rows: Mutex<HashMap<(TenantId, String), Session>>,
    }

    #[async_trait]
    impl SessionStore for InMemorySessionStore {
        async fn get(
            &self,
            tenant: &TenantId,
            id: &str,
            _consistency: ReadConsistency,
        ) -> RepoResult<Option<Session>> {
            let rows = self.rows.lock().unwrap();
            let now = chrono::Utc::now().timestamp_millis();
            Ok(rows
                .get(&(tenant.clone(), id.to_string()))
                .filter(|s| s.expires_at_ms > now)
                .cloned())
        }

        async fn put(&self, session: Session) -> RepoResult<()> {
            self.rows
                .lock()
                .unwrap()
                .insert((session.tenant.clone(), session.id.clone()), session);
            Ok(())
        }

        async fn revoke(&self, tenant: &TenantId, id: &str) -> RepoResult<bool> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .remove(&(tenant.clone(), id.to_string()))
                .is_some())
        }
    }

    /// In-memory [`ActionLogStore`].
    #[derive(Default)]
    pub struct InMemoryActionLogStore {
        rows: Mutex<Vec<ActionLogEntry>>,
    }

    #[async_trait]
    impl ActionLogStore for InMemoryActionLogStore {
        async fn append(&self, entry: ActionLogEntry) -> RepoResult<()> {
            let mut rows = self.rows.lock().unwrap();
            if !rows
                .iter()
                .any(|e| e.tenant == entry.tenant && e.action_id == entry.action_id)
            {
                rows.push(entry);
            }
            Ok(())
        }

        async fn list_recent(
            &self,
            tenant: &TenantId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<ActionLogEntry>> {
            let rows = self.rows.lock().unwrap();
            let mut items: Vec<ActionLogEntry> = rows
                .iter()
                .filter(|e| &e.tenant == tenant)
                .cloned()
                .collect();
            items.sort_by(|a, b| b.recorded_at_ms.cmp(&a.recorded_at_ms));
            items.truncate(page.size.max(1) as usize);
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }

        async fn list_for_object(
            &self,
            tenant: &TenantId,
            object: &ObjectId,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<ActionLogEntry>> {
            let rows = self.rows.lock().unwrap();
            let mut items: Vec<ActionLogEntry> = rows
                .iter()
                .filter(|e| &e.tenant == tenant && e.object.as_ref() == Some(object))
                .cloned()
                .collect();
            items.sort_by(|a, b| b.recorded_at_ms.cmp(&a.recorded_at_ms));
            items.truncate(page.size.max(1) as usize);
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }
    }

    /// In-memory [`SearchBackend`]. Naive substring match over the
    /// payload's JSON serialisation — good enough to wire up code
    /// paths in unit tests.
    #[derive(Default)]
    pub struct InMemorySearchBackend {
        rows: Mutex<HashMap<(TenantId, ObjectId), IndexDoc>>,
    }

    #[async_trait]
    impl SearchBackend for InMemorySearchBackend {
        async fn search(
            &self,
            query: SearchQuery,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<SearchHit>> {
            let rows = self.rows.lock().unwrap();
            let q = query.q.unwrap_or_default().to_lowercase();
            let mut items: Vec<SearchHit> = rows
                .values()
                .filter(|d| d.tenant == query.tenant)
                .filter(|d| {
                    query.type_id.as_ref().map_or(true, |t| &d.type_id == t)
                })
                .filter(|d| {
                    query
                        .filters
                        .iter()
                        .all(|(k, v)| d.payload.get(k).and_then(|x| x.as_str()) == Some(v.as_str()))
                })
                .filter(|d| {
                    if q.is_empty() {
                        true
                    } else {
                        d.payload.to_string().to_lowercase().contains(&q)
                    }
                })
                .map(|d| SearchHit {
                    id: d.id.clone(),
                    type_id: d.type_id.clone(),
                    score: 1.0,
                    snippet: Some(d.payload.clone()),
                })
                .collect();
            items.truncate(query.page.size.max(1) as usize);
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }

        async fn index(&self, doc: IndexDoc) -> RepoResult<()> {
            let mut rows = self.rows.lock().unwrap();
            let key = (doc.tenant.clone(), doc.id.clone());
            if let Some(existing) = rows.get(&key) {
                if existing.version >= doc.version {
                    return Ok(()); // discard stale write
                }
            }
            rows.insert(key, doc);
            Ok(())
        }

        async fn delete(
            &self,
            tenant: &TenantId,
            id: &ObjectId,
        ) -> RepoResult<bool> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .remove(&(tenant.clone(), id.clone()))
                .is_some())
        }

        async fn search_vector(
            &self,
            query: VectorQuery,
            _consistency: ReadConsistency,
        ) -> RepoResult<Vec<SearchHit>> {
            fn cosine(a: &[f32], b: &[f32]) -> f32 {
                if a.is_empty() || a.len() != b.len() {
                    return 0.0;
                }
                let mut dot = 0.0f32;
                let mut na = 0.0f32;
                let mut nb = 0.0f32;
                for (x, y) in a.iter().zip(b.iter()) {
                    dot += x * y;
                    na += x * x;
                    nb += y * y;
                }
                let den = (na.sqrt() * nb.sqrt()).max(f32::EPSILON);
                dot / den
            }

            let rows = self.rows.lock().unwrap();
            let mut scored: Vec<(f32, &IndexDoc)> = rows
                .values()
                .filter(|d| d.tenant == query.tenant)
                .filter(|d| query.type_id.as_ref().map_or(true, |t| &d.type_id == t))
                .filter(|d| {
                    query.filters.iter().all(|(k, v)| {
                        d.payload.get(k).and_then(|x| x.as_str()) == Some(v.as_str())
                    })
                })
                .filter_map(|d| {
                    d.embedding
                        .as_ref()
                        .map(|e| (cosine(e, &query.embedding), d))
                })
                .collect();
            scored.sort_by(|a, b| b.0.partial_cmp(&a.0).unwrap_or(std::cmp::Ordering::Equal));
            scored.truncate(query.k.max(1));
            Ok(scored
                .into_iter()
                .map(|(s, d)| SearchHit {
                    id: d.id.clone(),
                    type_id: d.type_id.clone(),
                    score: s,
                    snippet: Some(d.payload.clone()),
                })
                .collect())
        }
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use noop::*;

    fn obj(tenant: &str, id: &str, type_id: &str, version: u64) -> Object {
        Object {
            tenant: TenantId(tenant.into()),
            id: ObjectId(id.into()),
            type_id: TypeId(type_id.into()),
            version,
            payload: serde_json::json!({"hello": "world"}),
            updated_at_ms: 0,
            owner: None,
            markings: Vec::new(),
        }
    }

    #[tokio::test]
    async fn object_store_optimistic_lock() {
        let s = InMemoryObjectStore::default();
        let o = obj("t1", "obj1", "type1", 0);

        assert_eq!(
            s.put(o.clone(), None).await.unwrap(),
            PutOutcome::Inserted
        );

        // Update with correct expected version succeeds.
        let outcome = s.put(o.clone(), Some(1)).await.unwrap();
        assert!(matches!(
            outcome,
            PutOutcome::Updated {
                previous_version: 1,
                new_version: 2
            }
        ));

        // Update with stale expected version fails.
        let outcome = s.put(o.clone(), Some(1)).await.unwrap();
        assert!(matches!(
            outcome,
            PutOutcome::VersionConflict {
                expected_version: 1,
                actual_version: 2
            }
        ));
    }

    #[tokio::test]
    async fn schema_store_rejects_non_monotonic_version() {
        let s = InMemorySchemaStore::default();
        let v1 = Schema {
            type_id: TypeId("t".into()),
            version: 1,
            json_schema: serde_json::json!({}),
            created_at_ms: 0,
        };
        s.put(v1.clone()).await.unwrap();
        assert!(matches!(
            s.put(v1).await,
            Err(RepoError::InvalidArgument(_))
        ));
    }

    #[tokio::test]
    async fn search_backend_discards_stale_writes() {
        let s = InMemorySearchBackend::default();
        let mk = |v: u64| IndexDoc {
            tenant: TenantId("t".into()),
            id: ObjectId("a".into()),
            type_id: TypeId("type".into()),
            payload: serde_json::json!({"v": v}),
            version: v,
            embedding: None,
        };
        s.index(mk(2)).await.unwrap();
        s.index(mk(1)).await.unwrap();
        let hits = s
            .search(
                SearchQuery {
                    tenant: TenantId("t".into()),
                    type_id: None,
                    q: None,
                    filters: HashMap::new(),
                    page: Page {
                        size: 10,
                        token: None,
                    },
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(hits.items.len(), 1);
        assert_eq!(hits.items[0].snippet, Some(serde_json::json!({"v": 2})));
    }

    #[tokio::test]
    async fn object_store_list_by_owner_and_marking() {
        let s = InMemoryObjectStore::default();
        let mut a = obj("t1", "a", "T", 1);
        a.owner = Some(OwnerId("alice".into()));
        a.markings = vec![MarkingId("public".into())];
        let mut b = obj("t1", "b", "T", 1);
        b.owner = Some(OwnerId("bob".into()));
        b.markings = vec![MarkingId("secret".into())];
        let mut c = obj("t1", "c", "T", 1);
        c.owner = Some(OwnerId("alice".into()));
        c.markings = vec![MarkingId("secret".into()), MarkingId("public".into())];
        s.put(a, None).await.unwrap();
        s.put(b, None).await.unwrap();
        s.put(c, None).await.unwrap();

        let by_alice = s
            .list_by_owner(
                &TenantId("t1".into()),
                &OwnerId("alice".into()),
                Page { size: 10, token: None },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(by_alice.items.len(), 2);

        let secret = s
            .list_by_marking(
                &TenantId("t1".into()),
                &MarkingId("secret".into()),
                Page { size: 10, token: None },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(secret.items.len(), 2);

        // Tenant isolation.
        let other = s
            .list_by_owner(
                &TenantId("t2".into()),
                &OwnerId("alice".into()),
                Page { size: 10, token: None },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert!(other.items.is_empty());
    }
}
