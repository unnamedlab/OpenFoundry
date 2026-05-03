//! Legacy PostgreSQL adapters for the `storage-abstraction` traits.
//!
//! **Status: stub.** The legacy ontology PG schema (`object_instances`,
//! `link_instances`, `action_executions`) was designed before the
//! Cassandra-Foundry parity work and has shape mismatches with the new
//! trait contracts:
//!
//! * Tables use opaque UUID surrogate keys instead of the
//!   `(tenant, type_id, id)` composite required by [`ObjectStore`].
//! * `link_instances` has no `tenant` column and no uniqueness on
//!   `(link_type, source, target)` — the trait treats links as
//!   idempotent on that triple.
//! * `action_executions` has no idempotency guarantee on `action_id`.
//!
//! Reconciling these is the explicit goal of S1.4 (ontology-actions),
//! S1.5 (ontology-query) and S1.7 (remaining services) of
//! `docs/architecture/migration-plan-cassandra-foundry-parity.md`. Until
//! a service is migrated, its handlers continue to use `AppState::db`
//! directly with `sqlx`, bypassing the trait. The adapters defined here
//! exist to give those migrations a typed landing pad and to keep the
//! `legacy-pg` feature flag visible in the build matrix.
//!
//! When implementing a real adapter, follow the conventions:
//!
//! 1. Tenant is read from a `tenant` column if present, otherwise
//!    defaulted to the constant `LEGACY_TENANT` and warned about.
//! 2. Composite-key idempotency is emulated with `INSERT ... ON CONFLICT`
//!    on a per-table `(tenant, link_type, source, target)` unique index
//!    (PG migration introducing the index lives in the owning service).
//! 3. `expected_version` for `ObjectStore::put` maps to a `WHERE version = $n`
//!    clause; on miss, return `PutOutcome::VersionConflict`.

use async_trait::async_trait;
use sqlx::PgPool;
use storage_abstraction::repositories::{
    ActionLogEntry, ActionLogStore, Link, LinkStore, LinkTypeId, MarkingId, Object, ObjectId,
    ObjectStore, OwnerId, Page, PagedResult, PutOutcome, ReadConsistency, RepoError, RepoResult,
    TenantId, TypeId,
};

const NOT_YET: &str = "PostgreSQL adapter for storage-abstraction trait is a stub; the owning service has not yet been migrated. See migration-plan-cassandra-foundry-parity.md §S1.4-S1.7";

/// Wraps a `PgPool` and exposes the [`ObjectStore`] surface.
pub struct PostgresObjectStore {
    pub pool: PgPool,
}

impl PostgresObjectStore {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl ObjectStore for PostgresObjectStore {
    async fn get(
        &self,
        _tenant: &TenantId,
        _id: &ObjectId,
        _consistency: ReadConsistency,
    ) -> RepoResult<Option<Object>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn put(&self, _object: Object, _expected_version: Option<u64>) -> RepoResult<PutOutcome> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn delete(&self, _tenant: &TenantId, _id: &ObjectId) -> RepoResult<bool> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_by_type(
        &self,
        _tenant: &TenantId,
        _type_id: &TypeId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_by_owner(
        &self,
        _tenant: &TenantId,
        _owner: &OwnerId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_by_marking(
        &self,
        _tenant: &TenantId,
        _marking: &MarkingId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
}

/// Wraps a `PgPool` and exposes the [`LinkStore`] surface.
pub struct PostgresLinkStore {
    pub pool: PgPool,
}

impl PostgresLinkStore {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl LinkStore for PostgresLinkStore {
    async fn put(&self, _link: Link) -> RepoResult<()> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn delete(
        &self,
        _tenant: &TenantId,
        _link_type: &LinkTypeId,
        _from: &ObjectId,
        _to: &ObjectId,
    ) -> RepoResult<bool> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_outgoing(
        &self,
        _tenant: &TenantId,
        _link_type: &LinkTypeId,
        _from: &ObjectId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Link>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_incoming(
        &self,
        _tenant: &TenantId,
        _link_type: &LinkTypeId,
        _to: &ObjectId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Link>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
}

/// Wraps a `PgPool` and exposes the [`ActionLogStore`] surface.
pub struct PostgresActionLogStore {
    pub pool: PgPool,
}

impl PostgresActionLogStore {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl ActionLogStore for PostgresActionLogStore {
    async fn append(&self, _entry: ActionLogEntry) -> RepoResult<()> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_recent(
        &self,
        _tenant: &TenantId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_for_object(
        &self,
        _tenant: &TenantId,
        _object: &ObjectId,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
    async fn list_for_action(
        &self,
        _tenant: &TenantId,
        _action_id: &str,
        _page: Page,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<ActionLogEntry>> {
        Err(RepoError::Backend(NOT_YET.into()))
    }
}
