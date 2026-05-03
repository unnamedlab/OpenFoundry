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
use std::sync::{Arc, Mutex};
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

/// Stable identifier for a saved object set definition.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct ObjectSetId(pub String);

impl From<&str> for ObjectSetId {
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
    /// Owning organization / workspace UUID when the runtime uses that
    /// partitioning model. Optional so older adapters can omit it.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub organization_id: Option<String>,
    /// Original creation timestamp in milliseconds. Optional because some
    /// backends only project the latest update time.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_at_ms: Option<i64>,
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
    /// Deterministic event id used for idempotent append/retry. Backends may
    /// derive one from the logical entry when callers leave it empty.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub event_id: Option<String>,
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

/// One materialized row for a saved object set.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObjectSetMaterializedRow {
    /// Stable row identifier inside the materialization. Usually the base
    /// object id when present, otherwise a deterministic row ordinal.
    pub row_id: String,
    /// Zero-based row ordinal in the materialized result.
    pub ordinal: u32,
    /// Projected row payload.
    pub payload: serde_json::Value,
}

/// Metadata describing the latest object set materialization.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObjectSetMaterializationMetadata {
    /// Tenant scope.
    pub tenant: TenantId,
    /// Object set definition id.
    pub set_id: ObjectSetId,
    /// Base ontology type for the materialized set.
    pub base_type_id: TypeId,
    /// Store-specific materialization id. The default search-backed store uses
    /// the generation timestamp in milliseconds.
    pub materialization_id: String,
    /// Generation timestamp in milliseconds.
    pub generated_at_ms: i64,
    /// Number of base objects that matched before joins/projections.
    pub total_base_matches: u64,
    /// Total rows produced by evaluation before any API limit was applied.
    pub total_rows: u64,
    /// Number of rows persisted in the read model.
    pub stored_row_count: u64,
    /// Traversal neighbors touched while evaluating the set.
    pub traversal_neighbor_count: u64,
}

/// Full replacement payload for an object set materialization.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ObjectSetMaterialization {
    /// Tenant scope.
    pub tenant: TenantId,
    /// Object set definition id.
    pub set_id: ObjectSetId,
    /// Base ontology type for the materialized set.
    pub base_type_id: TypeId,
    /// Generation timestamp in milliseconds.
    pub generated_at_ms: i64,
    /// Number of base objects that matched before joins/projections.
    pub total_base_matches: u64,
    /// Total rows produced by evaluation before any API limit was applied.
    pub total_rows: u64,
    /// Traversal neighbors touched while evaluating the set.
    pub traversal_neighbor_count: u64,
    /// Rows persisted in the read model.
    pub rows: Vec<ObjectSetMaterializedRow>,
}

/// Logical bucket for declarative ontology definitions.
///
/// These records are low-cardinality control-plane data. Production keeps
/// them in PostgreSQL (`pg-schemas`) for transactional editing and schema
/// governance, but handlers should depend on [`DefinitionStore`] rather than
/// embedding `sqlx` calls.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct DefinitionKind(pub String);

impl From<&str> for DefinitionKind {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Stable identifier for a declarative definition row.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct DefinitionId(pub String);

impl From<&str> for DefinitionId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// One declarative definition record.
///
/// `payload` is intentionally JSON so `storage-abstraction` stays independent
/// from ontology-kernel's HTTP model structs. Callers can deserialize it into
/// `ObjectType`, `Property`, `ActionType`, etc. at the edge of the handler.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DefinitionRecord {
    /// Logical record kind, e.g. `object_type`, `property`, `action_type`.
    pub kind: DefinitionKind,
    /// Stable record id.
    pub id: DefinitionId,
    /// Optional tenant scope. Most schema definitions are global within a
    /// deployment today; the field is present for project-scoped definitions.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tenant: Option<TenantId>,
    /// Optional owner/principal id.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<String>,
    /// Optional parent definition id (`object_type_id`, `interface_id`, ...).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<DefinitionId>,
    /// Monotonic logical version when the backend tracks one. Backends without
    /// a version column may use `updated_at_ms` as a coarse version.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub version: Option<u64>,
    /// Definition payload as JSON.
    pub payload: serde_json::Value,
    /// Creation timestamp in milliseconds when available.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub created_at_ms: Option<i64>,
    /// Update timestamp in milliseconds when available.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub updated_at_ms: Option<i64>,
}

/// Query over declarative definitions.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DefinitionQuery {
    /// Logical kind to list.
    pub kind: DefinitionKind,
    /// Optional tenant scope.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tenant: Option<TenantId>,
    /// Optional owner filter.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub owner_id: Option<String>,
    /// Optional parent filter.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<DefinitionId>,
    /// Optional backend-specific equality filters over known fields.
    #[serde(default, skip_serializing_if = "HashMap::is_empty")]
    pub filters: HashMap<String, String>,
    /// Optional lexical search over name/display fields.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub search: Option<String>,
    /// Page request.
    pub page: Page,
}

/// Logical bucket for generic read-model records.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct ReadModelKind(pub String);

impl From<&str> for ReadModelKind {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// Stable identifier for a read-model row.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct ReadModelId(pub String);

impl From<&str> for ReadModelId {
    fn from(s: &str) -> Self {
        Self(s.to_string())
    }
}

/// One generic read-model record.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReadModelRecord {
    /// Logical read-model kind, e.g. `function_run`, `project_working_state`.
    pub kind: ReadModelKind,
    /// Tenant scope.
    pub tenant: TenantId,
    /// Stable record id.
    pub id: ReadModelId,
    /// Optional parent id for query-owned models.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<ReadModelId>,
    /// JSON payload.
    pub payload: serde_json::Value,
    /// Backend version / sequence.
    pub version: u64,
    /// Update timestamp in milliseconds.
    pub updated_at_ms: i64,
}

/// Query over generic read-model records.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReadModelQuery {
    /// Logical kind to list.
    pub kind: ReadModelKind,
    /// Tenant scope.
    pub tenant: TenantId,
    /// Optional parent filter.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub parent_id: Option<ReadModelId>,
    /// Optional equality filters over known payload fields.
    #[serde(default, skip_serializing_if = "HashMap::is_empty")]
    pub filters: HashMap<String, String>,
    /// Page request.
    pub page: Page,
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
    async fn put(&self, obj: Object, expected_version: Option<u64>) -> RepoResult<PutOutcome>;

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
    /// `(tenant, event_id)`; implementations may derive a deterministic
    /// `event_id` when the field is empty.
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

    /// Page through actions for a specific action id.
    async fn list_for_action(
        &self,
        tenant: &TenantId,
        action_id: &str,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>>;
}

/// Repository for declarative ontology definitions retained in PostgreSQL.
#[async_trait]
pub trait DefinitionStore: Send + Sync {
    /// Load one definition.
    async fn get(
        &self,
        kind: &DefinitionKind,
        id: &DefinitionId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<DefinitionRecord>>;

    /// List definitions by kind and lightweight filters.
    async fn list(
        &self,
        query: DefinitionQuery,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<DefinitionRecord>>;

    /// Insert or update one definition. `expected_version = None` means
    /// insert/upsert depending on the backend's natural contract.
    async fn put(
        &self,
        record: DefinitionRecord,
        expected_version: Option<u64>,
    ) -> RepoResult<PutOutcome>;

    /// Delete one definition. Returns `Ok(false)` when absent.
    async fn delete(&self, kind: &DefinitionKind, id: &DefinitionId) -> RepoResult<bool>;

    /// Count definitions for admin/inventory surfaces.
    async fn count(&self, query: DefinitionQuery, consistency: ReadConsistency) -> RepoResult<u64> {
        Ok(self.list(query, consistency).await?.items.len() as u64)
    }
}

/// Generic read-model repository for warm runtime projections that do not
/// deserve bespoke traits yet.
#[async_trait]
pub trait ReadModelStore: Send + Sync {
    /// Load one read-model row.
    async fn get(
        &self,
        kind: &ReadModelKind,
        tenant: &TenantId,
        id: &ReadModelId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<ReadModelRecord>>;

    /// List read-model rows by kind/tenant and lightweight filters.
    async fn list(
        &self,
        query: ReadModelQuery,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ReadModelRecord>>;

    /// Upsert one row. Implementations discard stale writes whose version is
    /// older than the currently stored version.
    async fn put(&self, record: ReadModelRecord) -> RepoResult<PutOutcome>;

    /// Delete one row.
    async fn delete(
        &self,
        kind: &ReadModelKind,
        tenant: &TenantId,
        id: &ReadModelId,
    ) -> RepoResult<bool>;
}

/// Read model for saved object set materializations.
///
/// Definitions remain in PostgreSQL (`pg-schemas`), but evaluated runtime
/// rows and materialization metadata belong in the search/read-model plane.
#[async_trait]
pub trait ObjectSetMaterializationStore: Send + Sync {
    /// Atomically replace the latest materialization as observed by readers.
    /// Implementations should publish row documents first and make metadata
    /// visible last.
    async fn replace(
        &self,
        materialization: ObjectSetMaterialization,
    ) -> RepoResult<ObjectSetMaterializationMetadata>;

    /// Load latest materialization metadata for one object set.
    async fn get_metadata(
        &self,
        tenant: &TenantId,
        set_id: &ObjectSetId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<ObjectSetMaterializationMetadata>>;

    /// Page through rows from the latest materialization.
    async fn list_rows(
        &self,
        tenant: &TenantId,
        set_id: &ObjectSetId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ObjectSetMaterializedRow>>;

    /// Delete materialized rows and metadata for one object set.
    async fn delete(&self, tenant: &TenantId, set_id: &ObjectSetId) -> RepoResult<bool>;
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
// Object set materialization via SearchBackend
// ---------------------------------------------------------------------------

const OBJECT_SET_META_TYPE: &str = "__object_set_materialization_meta";
const OBJECT_SET_ROW_TYPE: &str = "__object_set_materialization_row";

/// [`ObjectSetMaterializationStore`] backed by any [`SearchBackend`].
///
/// Rows are indexed as deterministic documents keyed by object-set id and row
/// ordinal; metadata is a single deterministic document per set. The metadata
/// document is written last so readers never observe a latest materialization
/// before its rows have been published.
pub struct SearchBackedObjectSetMaterializationStore {
    search: Arc<dyn SearchBackend>,
}

impl SearchBackedObjectSetMaterializationStore {
    /// Build a materialization store over an existing search backend.
    pub fn new(search: Arc<dyn SearchBackend>) -> Self {
        Self { search }
    }

    fn metadata_doc_id(set_id: &ObjectSetId) -> ObjectId {
        ObjectId(format!("object-set:{}:metadata", set_id.0))
    }

    fn row_doc_id(set_id: &ObjectSetId, ordinal: u64) -> ObjectId {
        ObjectId(format!("object-set:{}:row:{ordinal}", set_id.0))
    }

    fn version_from_ms(ms: i64) -> u64 {
        u64::try_from(ms).unwrap_or(0).max(1)
    }

    fn materialization_id(ms: i64) -> String {
        ms.to_string()
    }

    fn metadata_from_materialization(
        materialization: &ObjectSetMaterialization,
    ) -> ObjectSetMaterializationMetadata {
        ObjectSetMaterializationMetadata {
            tenant: materialization.tenant.clone(),
            set_id: materialization.set_id.clone(),
            base_type_id: materialization.base_type_id.clone(),
            materialization_id: Self::materialization_id(materialization.generated_at_ms),
            generated_at_ms: materialization.generated_at_ms,
            total_base_matches: materialization.total_base_matches,
            total_rows: materialization.total_rows,
            stored_row_count: materialization.rows.len() as u64,
            traversal_neighbor_count: materialization.traversal_neighbor_count,
        }
    }

    fn metadata_doc(metadata: &ObjectSetMaterializationMetadata) -> IndexDoc {
        IndexDoc {
            tenant: metadata.tenant.clone(),
            id: Self::metadata_doc_id(&metadata.set_id),
            type_id: TypeId(OBJECT_SET_META_TYPE.to_string()),
            payload: serde_json::json!({
                "kind": "object_set_materialization_metadata",
                "object_set_id": metadata.set_id.0,
                "base_type_id": metadata.base_type_id.0,
                "materialization_id": metadata.materialization_id,
                "generated_at_ms": metadata.generated_at_ms,
                "total_base_matches": metadata.total_base_matches,
                "total_rows": metadata.total_rows,
                "stored_row_count": metadata.stored_row_count,
                "traversal_neighbor_count": metadata.traversal_neighbor_count,
            }),
            version: Self::version_from_ms(metadata.generated_at_ms),
            embedding: None,
        }
    }

    fn row_doc(
        materialization: &ObjectSetMaterialization,
        row: &ObjectSetMaterializedRow,
    ) -> IndexDoc {
        IndexDoc {
            tenant: materialization.tenant.clone(),
            id: Self::row_doc_id(&materialization.set_id, row.ordinal as u64),
            type_id: TypeId(OBJECT_SET_ROW_TYPE.to_string()),
            payload: serde_json::json!({
                "kind": "object_set_materialization_row",
                "object_set_id": materialization.set_id.0,
                "base_type_id": materialization.base_type_id.0,
                "materialization_id": Self::materialization_id(materialization.generated_at_ms),
                "generated_at_ms": materialization.generated_at_ms,
                "row_id": row.row_id,
                "ordinal": row.ordinal,
                "row": row.payload,
            }),
            version: Self::version_from_ms(materialization.generated_at_ms),
            embedding: None,
        }
    }

    fn u64_field(payload: &serde_json::Value, field: &str) -> RepoResult<u64> {
        payload
            .get(field)
            .and_then(serde_json::Value::as_u64)
            .ok_or_else(|| RepoError::Backend(format!("missing {field} in object set metadata")))
    }

    fn i64_field(payload: &serde_json::Value, field: &str) -> RepoResult<i64> {
        payload
            .get(field)
            .and_then(serde_json::Value::as_i64)
            .ok_or_else(|| RepoError::Backend(format!("missing {field} in object set metadata")))
    }

    fn string_field(payload: &serde_json::Value, field: &str) -> RepoResult<String> {
        payload
            .get(field)
            .and_then(serde_json::Value::as_str)
            .map(ToOwned::to_owned)
            .ok_or_else(|| RepoError::Backend(format!("missing {field} in object set metadata")))
    }

    fn metadata_from_payload(
        tenant: &TenantId,
        payload: &serde_json::Value,
    ) -> RepoResult<ObjectSetMaterializationMetadata> {
        Ok(ObjectSetMaterializationMetadata {
            tenant: tenant.clone(),
            set_id: ObjectSetId(Self::string_field(payload, "object_set_id")?),
            base_type_id: TypeId(Self::string_field(payload, "base_type_id")?),
            materialization_id: Self::string_field(payload, "materialization_id")?,
            generated_at_ms: Self::i64_field(payload, "generated_at_ms")?,
            total_base_matches: Self::u64_field(payload, "total_base_matches")?,
            total_rows: Self::u64_field(payload, "total_rows")?,
            stored_row_count: Self::u64_field(payload, "stored_row_count")?,
            traversal_neighbor_count: Self::u64_field(payload, "traversal_neighbor_count")?,
        })
    }

    fn row_from_payload(payload: &serde_json::Value) -> RepoResult<ObjectSetMaterializedRow> {
        let ordinal = Self::u64_field(payload, "ordinal")?;
        let ordinal = u32::try_from(ordinal).map_err(|_| {
            RepoError::Backend("object set materialized row ordinal overflows u32".to_string())
        })?;
        Ok(ObjectSetMaterializedRow {
            row_id: Self::string_field(payload, "row_id")?,
            ordinal,
            payload: payload
                .get("row")
                .cloned()
                .unwrap_or(serde_json::Value::Null),
        })
    }
}

#[async_trait]
impl ObjectSetMaterializationStore for SearchBackedObjectSetMaterializationStore {
    async fn replace(
        &self,
        materialization: ObjectSetMaterialization,
    ) -> RepoResult<ObjectSetMaterializationMetadata> {
        let previous = self
            .get_metadata(
                &materialization.tenant,
                &materialization.set_id,
                ReadConsistency::Eventual,
            )
            .await?;
        let metadata = Self::metadata_from_materialization(&materialization);

        let rows = materialization
            .rows
            .iter()
            .map(|row| Self::row_doc(&materialization, row))
            .collect();
        let outcome = self.search.bulk_index(rows).await?;
        if !outcome.failed.is_empty() {
            let failed = outcome
                .failed
                .into_iter()
                .map(|(id, error)| format!("{}: {error}", id.0))
                .collect::<Vec<_>>()
                .join(", ");
            return Err(RepoError::Backend(format!(
                "failed to index object set materialization rows: {failed}"
            )));
        }

        self.search
            .index(Self::metadata_doc(&metadata))
            .await
            .map_err(|error| RepoError::Backend(error.to_string()))?;

        if let Some(previous) = previous {
            for ordinal in metadata.stored_row_count..previous.stored_row_count {
                let _ = self
                    .search
                    .delete(
                        &materialization.tenant,
                        &Self::row_doc_id(&metadata.set_id, ordinal),
                    )
                    .await;
            }
        }

        Ok(metadata)
    }

    async fn get_metadata(
        &self,
        tenant: &TenantId,
        set_id: &ObjectSetId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<ObjectSetMaterializationMetadata>> {
        let mut filters = HashMap::new();
        filters.insert("object_set_id".to_string(), set_id.0.clone());
        let result = self
            .search
            .search(
                SearchQuery {
                    tenant: tenant.clone(),
                    type_id: Some(TypeId(OBJECT_SET_META_TYPE.to_string())),
                    q: None,
                    filters,
                    page: Page {
                        size: 1,
                        token: None,
                    },
                },
                consistency,
            )
            .await?;

        result
            .items
            .into_iter()
            .next()
            .and_then(|hit| hit.snippet)
            .map(|payload| Self::metadata_from_payload(tenant, &payload))
            .transpose()
    }

    async fn list_rows(
        &self,
        tenant: &TenantId,
        set_id: &ObjectSetId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ObjectSetMaterializedRow>> {
        let Some(metadata) = self.get_metadata(tenant, set_id, consistency).await? else {
            return Ok(PagedResult {
                items: Vec::new(),
                next_token: None,
            });
        };

        let mut filters = HashMap::new();
        filters.insert("object_set_id".to_string(), set_id.0.clone());
        filters.insert(
            "materialization_id".to_string(),
            metadata.materialization_id.clone(),
        );
        let result = self
            .search
            .search(
                SearchQuery {
                    tenant: tenant.clone(),
                    type_id: Some(TypeId(OBJECT_SET_ROW_TYPE.to_string())),
                    q: None,
                    filters,
                    page,
                },
                consistency,
            )
            .await?;

        let mut items = result
            .items
            .into_iter()
            .filter_map(|hit| hit.snippet)
            .map(|payload| Self::row_from_payload(&payload))
            .collect::<RepoResult<Vec<_>>>()?;
        items.sort_by_key(|row| row.ordinal);
        Ok(PagedResult {
            items,
            next_token: result.next_token,
        })
    }

    async fn delete(&self, tenant: &TenantId, set_id: &ObjectSetId) -> RepoResult<bool> {
        let previous = self
            .get_metadata(tenant, set_id, ReadConsistency::Eventual)
            .await?;
        let mut deleted = self
            .search
            .delete(tenant, &Self::metadata_doc_id(set_id))
            .await?;

        if let Some(previous) = previous {
            for ordinal in 0..previous.stored_row_count {
                deleted |= self
                    .search
                    .delete(tenant, &Self::row_doc_id(set_id, ordinal))
                    .await?;
            }
        }

        Ok(deleted)
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

        async fn put(&self, obj: Object, expected_version: Option<u64>) -> RepoResult<PutOutcome> {
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

        async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool> {
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
            Ok(PagedResult {
                items,
                next_token: None,
            })
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
                .filter(|o| &o.tenant == tenant && o.markings.iter().any(|m| m == marking))
                .cloned()
                .collect();
            items.sort_by(|a, b| b.updated_at_ms.cmp(&a.updated_at_ms));
            items.truncate(page.size.max(1) as usize);
            Ok(PagedResult {
                items,
                next_token: None,
            })
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
                .filter(|l| l.tenant == *tenant && l.link_type == *link_type && l.from == *from)
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
                .filter(|l| l.tenant == *tenant && l.link_type == *link_type && l.to == *to)
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
            let event_id = entry
                .event_id
                .clone()
                .unwrap_or_else(|| entry.action_id.clone());
            if !rows.iter().any(|e| {
                e.tenant == entry.tenant
                    && e.event_id
                        .as_ref()
                        .map(String::as_str)
                        .unwrap_or(e.action_id.as_str())
                        == event_id
            }) {
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

        async fn list_for_action(
            &self,
            tenant: &TenantId,
            action_id: &str,
            page: Page,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<ActionLogEntry>> {
            let rows = self.rows.lock().unwrap();
            let mut items: Vec<ActionLogEntry> = rows
                .iter()
                .filter(|e| &e.tenant == tenant && e.action_id == action_id)
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

    /// In-memory [`DefinitionStore`].
    #[derive(Default)]
    pub struct InMemoryDefinitionStore {
        rows: Mutex<HashMap<(DefinitionKind, DefinitionId), DefinitionRecord>>,
    }

    impl InMemoryDefinitionStore {
        fn matches_query(record: &DefinitionRecord, query: &DefinitionQuery) -> bool {
            if record.kind != query.kind {
                return false;
            }
            if query
                .tenant
                .as_ref()
                .is_some_and(|tenant| record.tenant.as_ref() != Some(tenant))
            {
                return false;
            }
            if query
                .owner_id
                .as_ref()
                .is_some_and(|owner_id| record.owner_id.as_ref() != Some(owner_id))
            {
                return false;
            }
            if query
                .parent_id
                .as_ref()
                .is_some_and(|parent_id| record.parent_id.as_ref() != Some(parent_id))
            {
                return false;
            }
            if query.filters.iter().any(|(field, expected)| {
                record
                    .payload
                    .get(field)
                    .and_then(serde_json::Value::as_str)
                    != Some(expected.as_str())
            }) {
                return false;
            }
            if let Some(search) = query.search.as_deref() {
                let needle = search.trim().to_lowercase();
                if !needle.is_empty()
                    && !record.payload.to_string().to_lowercase().contains(&needle)
                {
                    return false;
                }
            }
            true
        }
    }

    #[async_trait]
    impl DefinitionStore for InMemoryDefinitionStore {
        async fn get(
            &self,
            kind: &DefinitionKind,
            id: &DefinitionId,
            _consistency: ReadConsistency,
        ) -> RepoResult<Option<DefinitionRecord>> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .get(&(kind.clone(), id.clone()))
                .cloned())
        }

        async fn list(
            &self,
            query: DefinitionQuery,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<DefinitionRecord>> {
            let offset = match query.page.token.as_deref() {
                Some(token) => token.parse::<usize>().map_err(|_| {
                    RepoError::InvalidArgument("definition page token is invalid".to_string())
                })?,
                None => 0,
            };
            let limit = query.page.size.max(1) as usize;
            let rows = self.rows.lock().unwrap();
            let mut items = rows
                .values()
                .filter(|record| Self::matches_query(record, &query))
                .cloned()
                .collect::<Vec<_>>();
            items.sort_by(|left, right| {
                right
                    .updated_at_ms
                    .cmp(&left.updated_at_ms)
                    .then_with(|| left.id.0.cmp(&right.id.0))
            });
            let total = items.len();
            let items = items
                .into_iter()
                .skip(offset)
                .take(limit)
                .collect::<Vec<_>>();
            Ok(PagedResult {
                items,
                next_token: (offset + limit < total).then(|| (offset + limit).to_string()),
            })
        }

        async fn put(
            &self,
            mut record: DefinitionRecord,
            expected_version: Option<u64>,
        ) -> RepoResult<PutOutcome> {
            let mut rows = self.rows.lock().unwrap();
            let key = (record.kind.clone(), record.id.clone());
            let previous = rows.get(&key).cloned();
            match previous {
                None => {
                    if let Some(expected) = expected_version {
                        if expected != 0 {
                            return Ok(PutOutcome::VersionConflict {
                                expected_version: expected,
                                actual_version: 0,
                            });
                        }
                    }
                    record.version = Some(record.version.unwrap_or(1).max(1));
                    rows.insert(key, record);
                    Ok(PutOutcome::Inserted)
                }
                Some(existing) => {
                    let actual_version = existing.version.unwrap_or(0);
                    if let Some(expected) = expected_version {
                        if expected != actual_version {
                            return Ok(PutOutcome::VersionConflict {
                                expected_version: expected,
                                actual_version,
                            });
                        }
                    }
                    let new_version = record.version.unwrap_or(actual_version + 1);
                    record.version = Some(new_version.max(actual_version + 1));
                    rows.insert(key, record);
                    Ok(PutOutcome::Updated {
                        previous_version: actual_version,
                        new_version: new_version.max(actual_version + 1),
                    })
                }
            }
        }

        async fn delete(&self, kind: &DefinitionKind, id: &DefinitionId) -> RepoResult<bool> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .remove(&(kind.clone(), id.clone()))
                .is_some())
        }
    }

    /// In-memory [`ReadModelStore`].
    #[derive(Default)]
    pub struct InMemoryReadModelStore {
        rows: Mutex<HashMap<(ReadModelKind, TenantId, ReadModelId), ReadModelRecord>>,
    }

    impl InMemoryReadModelStore {
        fn matches_query(record: &ReadModelRecord, query: &ReadModelQuery) -> bool {
            if record.kind != query.kind || record.tenant != query.tenant {
                return false;
            }
            if query
                .parent_id
                .as_ref()
                .is_some_and(|parent_id| record.parent_id.as_ref() != Some(parent_id))
            {
                return false;
            }
            !query.filters.iter().any(|(field, expected)| {
                record
                    .payload
                    .get(field)
                    .and_then(serde_json::Value::as_str)
                    != Some(expected.as_str())
            })
        }
    }

    #[async_trait]
    impl ReadModelStore for InMemoryReadModelStore {
        async fn get(
            &self,
            kind: &ReadModelKind,
            tenant: &TenantId,
            id: &ReadModelId,
            _consistency: ReadConsistency,
        ) -> RepoResult<Option<ReadModelRecord>> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .get(&(kind.clone(), tenant.clone(), id.clone()))
                .cloned())
        }

        async fn list(
            &self,
            query: ReadModelQuery,
            _consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<ReadModelRecord>> {
            let rows = self.rows.lock().unwrap();
            let mut items = rows
                .values()
                .filter(|record| Self::matches_query(record, &query))
                .cloned()
                .collect::<Vec<_>>();
            items.sort_by(|left, right| {
                right
                    .updated_at_ms
                    .cmp(&left.updated_at_ms)
                    .then_with(|| left.id.0.cmp(&right.id.0))
            });
            items.truncate(query.page.size.max(1) as usize);
            Ok(PagedResult {
                items,
                next_token: None,
            })
        }

        async fn put(&self, record: ReadModelRecord) -> RepoResult<PutOutcome> {
            let mut rows = self.rows.lock().unwrap();
            let key = (
                record.kind.clone(),
                record.tenant.clone(),
                record.id.clone(),
            );
            match rows.get(&key).cloned() {
                None => {
                    rows.insert(key, record);
                    Ok(PutOutcome::Inserted)
                }
                Some(existing) if existing.version > record.version => {
                    Ok(PutOutcome::VersionConflict {
                        expected_version: record.version,
                        actual_version: existing.version,
                    })
                }
                Some(existing) => {
                    rows.insert(key, record.clone());
                    Ok(PutOutcome::Updated {
                        previous_version: existing.version,
                        new_version: record.version,
                    })
                }
            }
        }

        async fn delete(
            &self,
            kind: &ReadModelKind,
            tenant: &TenantId,
            id: &ReadModelId,
        ) -> RepoResult<bool> {
            Ok(self
                .rows
                .lock()
                .unwrap()
                .remove(&(kind.clone(), tenant.clone(), id.clone()))
                .is_some())
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
            let mut items: Vec<SearchHit> =
                rows.values()
                    .filter(|d| d.tenant == query.tenant)
                    .filter(|d| query.type_id.as_ref().map_or(true, |t| &d.type_id == t))
                    .filter(|d| {
                        query.filters.iter().all(|(k, v)| {
                            d.payload.get(k).and_then(|x| x.as_str()) == Some(v.as_str())
                        })
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

        async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool> {
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
            let mut scored: Vec<(f32, &IndexDoc)> =
                rows.values()
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
            organization_id: None,
            created_at_ms: Some(0),
            updated_at_ms: 0,
            owner: None,
            markings: Vec::new(),
        }
    }

    #[tokio::test]
    async fn object_store_optimistic_lock() {
        let s = InMemoryObjectStore::default();
        let o = obj("t1", "obj1", "type1", 0);

        assert_eq!(s.put(o.clone(), None).await.unwrap(), PutOutcome::Inserted);

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
    async fn action_log_store_dedupes_and_reads_by_action_and_object() {
        let s = InMemoryActionLogStore::default();
        let tenant = TenantId("tenant-a".into());
        let object = ObjectId("object-a".into());
        let entry = ActionLogEntry {
            tenant: tenant.clone(),
            event_id: Some("event-1".into()),
            action_id: "action-a".into(),
            kind: "action_attempt".into(),
            subject: "user-a".into(),
            object: Some(object.clone()),
            payload: serde_json::json!({ "attempt": 1 }),
            recorded_at_ms: 10,
        };
        s.append(entry.clone()).await.unwrap();
        s.append(ActionLogEntry {
            payload: serde_json::json!({ "attempt": 99 }),
            recorded_at_ms: 20,
            ..entry
        })
        .await
        .unwrap();

        let by_action = s
            .list_for_action(
                &tenant,
                "action-a",
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Strong,
            )
            .await
            .unwrap();
        assert_eq!(by_action.items.len(), 1);
        assert_eq!(by_action.items[0].payload["attempt"], 1);

        let by_object = s
            .list_for_object(
                &tenant,
                &object,
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(by_object.items.len(), 1);
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
    async fn object_set_materialization_store_replaces_and_deletes_rows() {
        let search: Arc<dyn SearchBackend> = Arc::new(InMemorySearchBackend::default());
        let store = SearchBackedObjectSetMaterializationStore::new(search);
        let tenant = TenantId("tenant-a".into());
        let set_id = ObjectSetId("set-1".into());
        let base_type_id = TypeId("aircraft".into());

        let first = ObjectSetMaterialization {
            tenant: tenant.clone(),
            set_id: set_id.clone(),
            base_type_id: base_type_id.clone(),
            generated_at_ms: 100,
            total_base_matches: 2,
            total_rows: 2,
            traversal_neighbor_count: 0,
            rows: vec![
                ObjectSetMaterializedRow {
                    row_id: "a".into(),
                    ordinal: 0,
                    payload: serde_json::json!({"id": "a"}),
                },
                ObjectSetMaterializedRow {
                    row_id: "b".into(),
                    ordinal: 1,
                    payload: serde_json::json!({"id": "b"}),
                },
            ],
        };
        let metadata = store.replace(first).await.unwrap();
        assert_eq!(metadata.stored_row_count, 2);

        let rows = store
            .list_rows(
                &tenant,
                &set_id,
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(rows.items.len(), 2);

        let second = ObjectSetMaterialization {
            tenant: tenant.clone(),
            set_id: set_id.clone(),
            base_type_id,
            generated_at_ms: 200,
            total_base_matches: 1,
            total_rows: 1,
            traversal_neighbor_count: 0,
            rows: vec![ObjectSetMaterializedRow {
                row_id: "c".into(),
                ordinal: 0,
                payload: serde_json::json!({"id": "c"}),
            }],
        };
        let metadata = store.replace(second).await.unwrap();
        assert_eq!(metadata.stored_row_count, 1);

        let rows = store
            .list_rows(
                &tenant,
                &set_id,
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(rows.items.len(), 1);
        assert_eq!(rows.items[0].row_id, "c");

        assert!(store.delete(&tenant, &set_id).await.unwrap());
        let metadata = store
            .get_metadata(&tenant, &set_id, ReadConsistency::Eventual)
            .await
            .unwrap();
        assert!(metadata.is_none());
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
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(by_alice.items.len(), 2);

        let secret = s
            .list_by_marking(
                &TenantId("t1".into()),
                &MarkingId("secret".into()),
                Page {
                    size: 10,
                    token: None,
                },
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
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert!(other.items.is_empty());
    }

    #[tokio::test]
    async fn definition_store_filters_and_versions_records() {
        let s = InMemoryDefinitionStore::default();
        let kind = DefinitionKind("action_type".into());
        let id = DefinitionId("act-1".into());
        let record = DefinitionRecord {
            kind: kind.clone(),
            id: id.clone(),
            tenant: None,
            owner_id: Some("owner-a".into()),
            parent_id: Some(DefinitionId("type-a".into())),
            version: None,
            payload: serde_json::json!({"name": "Approve Case"}),
            created_at_ms: Some(100),
            updated_at_ms: Some(100),
        };

        assert_eq!(
            s.put(record.clone(), None).await.unwrap(),
            PutOutcome::Inserted
        );
        let loaded = s
            .get(&kind, &id, ReadConsistency::Eventual)
            .await
            .unwrap()
            .unwrap();
        assert_eq!(loaded.version, Some(1));

        let result = s
            .list(
                DefinitionQuery {
                    kind,
                    tenant: None,
                    owner_id: Some("owner-a".into()),
                    parent_id: Some(DefinitionId("type-a".into())),
                    filters: HashMap::new(),
                    search: Some("approve".into()),
                    page: Page {
                        size: 10,
                        token: None,
                    },
                },
                ReadConsistency::Eventual,
            )
            .await
            .unwrap();
        assert_eq!(result.items.len(), 1);
    }

    #[tokio::test]
    async fn read_model_store_discards_stale_versions() {
        let s = InMemoryReadModelStore::default();
        let kind = ReadModelKind("function_run".into());
        let tenant = TenantId("tenant-a".into());
        let id = ReadModelId("run-1".into());
        let make_record = |version| ReadModelRecord {
            kind: kind.clone(),
            tenant: tenant.clone(),
            id: id.clone(),
            parent_id: Some(ReadModelId("package-1".into())),
            payload: serde_json::json!({"status": "success"}),
            version,
            updated_at_ms: version as i64,
        };

        assert_eq!(s.put(make_record(2)).await.unwrap(), PutOutcome::Inserted);
        assert!(matches!(
            s.put(make_record(1)).await.unwrap(),
            PutOutcome::VersionConflict {
                expected_version: 1,
                actual_version: 2
            }
        ));

        let result = s
            .list(
                ReadModelQuery {
                    kind,
                    tenant,
                    parent_id: Some(ReadModelId("package-1".into())),
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
        assert_eq!(result.items.len(), 1);
        assert_eq!(result.items[0].version, 2);
    }
}
