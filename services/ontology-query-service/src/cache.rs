//! Local read cache fronting an [`ObjectStore`] (S1.5.a).
//!
//! Wraps any `Arc<dyn ObjectStore>` (Cassandra in production,
//! `InMemoryObjectStore` in tests) with a `moka::future::Cache`
//! keyed by `(tenant, object_id)`.
//!
//! ## Consistency contract (§S1.5.c / §S1.5.d)
//!
//! * [`ReadConsistency::Strong`] reads **bypass** the cache. The
//!   inner store is asked with the same `Strong` hint, which the
//!   Cassandra impl maps to `LOCAL_QUORUM`. The result is *also*
//!   inserted into the cache so subsequent eventual reads pick up
//!   the fresh value without waiting for an invalidation event.
//! * [`ReadConsistency::Eventual`] (and `BoundedStaleness`) reads
//!   first probe the cache. On miss they fall through to the inner
//!   store with `LOCAL_ONE` and populate the cache.
//!
//! ## Invalidation (§S1.5.b)
//!
//! Writes go to `ontology-actions-service`, never to this service.
//! The cache is invalidated via:
//!
//! 1. **Local-write invalidation** — every `put`/`delete` we route
//!    through this adapter calls `invalidate(tenant, id)` after the
//!    write succeeds. Today the read service is read-only so this
//!    branch is unused; it stays wired so the same adapter can be
//!    reused by services that mix reads and writes.
//! 2. **Cross-replica invalidation** via the
//!    [`crate::invalidation`] module: a NATS subscriber on
//!    `ontology.write.v1` calls [`CachingObjectStore::invalidate`]
//!    for every event it receives.
//!
//! ## Sizing
//!
//! Capacity defaults to **100 000 entries** with a **30 s TTL** as
//! prescribed by the migration plan. Both knobs are configurable via
//! environment so SREs can tune per-cluster.

use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use moka::future::Cache;
use storage_abstraction::repositories::{
    LinkTypeId, MarkingId, Object, ObjectId, ObjectStore, OwnerId, Page, PagedResult, PutOutcome,
    ReadConsistency, RepoResult, TenantId, TypeId,
};

/// Default cache capacity (entries). Plan §S1.5.a.
pub const DEFAULT_CAPACITY: u64 = 100_000;
/// Default per-entry TTL. Plan §S1.5.a.
pub const DEFAULT_TTL: Duration = Duration::from_secs(30);

/// Caching wrapper around an inner [`ObjectStore`].
pub struct CachingObjectStore {
    inner: Arc<dyn ObjectStore>,
    cache: Cache<CacheKey, Arc<Object>>,
}

/// Cache key. We split tenant from id so [`invalidate_tenant`] can
/// in principle iterate on a per-tenant view; today it only short-
/// circuits when the cache is empty (moka does not expose key-prefix
/// scans), but the shape keeps that future work cheap.
///
/// [`invalidate_tenant`]: CachingObjectStore::invalidate_tenant
#[derive(Debug, Clone, Hash, PartialEq, Eq)]
pub struct CacheKey {
    pub tenant: String,
    pub object_id: String,
}

impl CacheKey {
    pub fn new(tenant: &TenantId, id: &ObjectId) -> Self {
        Self {
            tenant: tenant.0.clone(),
            object_id: id.0.clone(),
        }
    }
}

impl CachingObjectStore {
    /// Build with the plan's defaults (100k entries / 30 s TTL).
    pub fn new(inner: Arc<dyn ObjectStore>) -> Self {
        Self::with_config(inner, DEFAULT_CAPACITY, DEFAULT_TTL)
    }

    /// Build with custom sizing. Capacity is clamped to ≥ 1.
    pub fn with_config(inner: Arc<dyn ObjectStore>, capacity: u64, ttl: Duration) -> Self {
        let capacity = capacity.max(1);
        let cache = Cache::builder()
            .max_capacity(capacity)
            .time_to_live(ttl)
            .build();
        Self { inner, cache }
    }

    /// Drop the cached entry for `(tenant, id)`. Called by the
    /// [`crate::invalidation`] subscriber and by any internal write
    /// path that mutates `(tenant, id)`.
    pub async fn invalidate(&self, tenant: &TenantId, id: &ObjectId) {
        self.cache.invalidate(&CacheKey::new(tenant, id)).await;
    }

    /// Drop every cached entry. Used as a defensive nuke after
    /// long-lived disconnects from the invalidation bus
    /// (see [`crate::invalidation::run`]).
    pub fn invalidate_all(&self) {
        self.cache.invalidate_all();
    }

    /// Best-effort tenant-scoped invalidation. Today this is a
    /// no-op-or-nuke: moka 0.12 does not expose key-prefix iteration
    /// without a full scan, so we only invalidate everything when the
    /// caller explicitly asks. The signature is preserved so the
    /// invalidation subscriber can call it without conditional code
    /// once we adopt a sharded cache (see `docs/architecture/`).
    pub fn invalidate_tenant(&self, _tenant: &TenantId) {
        self.cache.invalidate_all();
    }

    /// Approximate cached entry count. Useful for `/metrics`.
    pub fn entry_count(&self) -> u64 {
        self.cache.entry_count()
    }
}

#[async_trait]
impl ObjectStore for CachingObjectStore {
    async fn get(
        &self,
        tenant: &TenantId,
        id: &ObjectId,
        consistency: ReadConsistency,
    ) -> RepoResult<Option<Object>> {
        // Strong reads bypass the cache (plan §S1.5.d).
        if matches!(consistency, ReadConsistency::Strong) {
            let fresh = self.inner.get(tenant, id, consistency).await?;
            if let Some(obj) = &fresh {
                self.cache
                    .insert(CacheKey::new(tenant, id), Arc::new(obj.clone()))
                    .await;
            }
            return Ok(fresh);
        }

        // Eventual / bounded: probe cache first.
        let key = CacheKey::new(tenant, id);
        if let Some(hit) = self.cache.get(&key).await {
            return Ok(Some((*hit).clone()));
        }
        let fetched = self.inner.get(tenant, id, consistency).await?;
        if let Some(obj) = &fetched {
            self.cache.insert(key, Arc::new(obj.clone())).await;
        }
        Ok(fetched)
    }

    async fn put(&self, obj: Object, expected_version: Option<u64>) -> RepoResult<PutOutcome> {
        let tenant = obj.tenant.clone();
        let id = obj.id.clone();
        let outcome = self.inner.put(obj, expected_version).await?;
        // Best-effort: invalidate regardless of outcome so we never
        // serve a value that contradicts a write we just acknowledged.
        self.cache.invalidate(&CacheKey::new(&tenant, &id)).await;
        Ok(outcome)
    }

    async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool> {
        let removed = self.inner.delete(tenant, id).await?;
        self.cache.invalidate(&CacheKey::new(tenant, id)).await;
        Ok(removed)
    }

    async fn list_by_type(
        &self,
        tenant: &TenantId,
        type_id: &TypeId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        // List queries are not cached: result sets are large and
        // their identity (tenant + type + page token + consistency)
        // is poor as a cache key. The per-row cache still warms up
        // through subsequent point reads.
        self.inner
            .list_by_type(tenant, type_id, page, consistency)
            .await
    }

    async fn list_by_owner(
        &self,
        tenant: &TenantId,
        owner: &OwnerId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        self.inner
            .list_by_owner(tenant, owner, page, consistency)
            .await
    }

    async fn list_by_marking(
        &self,
        tenant: &TenantId,
        marking: &MarkingId,
        page: Page,
        consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<Object>> {
        self.inner
            .list_by_marking(tenant, marking, page, consistency)
            .await
    }
}

// `LinkTypeId` is only re-exported so doc-links resolve.
#[allow(dead_code)]
const _: Option<LinkTypeId> = None;

#[cfg(test)]
mod tests {
    use super::*;
    use storage_abstraction::repositories::PutOutcome;
    use storage_abstraction::repositories::noop::InMemoryObjectStore;

    fn obj(tenant: &str, id: &str, version: u64) -> Object {
        Object {
            tenant: TenantId(tenant.into()),
            id: ObjectId(id.into()),
            type_id: TypeId("t".into()),
            version,
            payload: serde_json::json!({"v": version}),
            organization_id: None,
            created_at_ms: Some(0),
            updated_at_ms: 0,
            owner: None,
            markings: vec![],
        }
    }

    #[tokio::test]
    async fn eventual_reads_hit_cache_after_first_miss() {
        let inner = Arc::new(InMemoryObjectStore::default());
        inner.put(obj("acme", "o1", 1), None).await.expect("seed");
        let cache = CachingObjectStore::new(inner.clone());

        let t = TenantId("acme".into());
        let id = ObjectId("o1".into());

        let first = cache
            .get(&t, &id, ReadConsistency::Eventual)
            .await
            .expect("first")
            .expect("present");
        assert_eq!(first.version, 1);

        // Mutate the inner store without going through the cache;
        // an eventual read MUST still see the cached value.
        match inner
            .put(obj("acme", "o1", 1), Some(1))
            .await
            .expect("update")
        {
            PutOutcome::Updated { new_version, .. } => assert_eq!(new_version, 2),
            other => panic!("unexpected: {other:?}"),
        }
        let cached = cache
            .get(&t, &id, ReadConsistency::Eventual)
            .await
            .expect("cached")
            .expect("present");
        assert_eq!(cached.version, 1, "eventual read should hit stale cache");
    }

    #[tokio::test]
    async fn strong_reads_bypass_cache_and_refresh_it() {
        let inner = Arc::new(InMemoryObjectStore::default());
        inner.put(obj("acme", "o1", 1), None).await.expect("seed");
        let cache = CachingObjectStore::new(inner.clone());

        let t = TenantId("acme".into());
        let id = ObjectId("o1".into());

        // Warm cache with stale value.
        let _ = cache.get(&t, &id, ReadConsistency::Eventual).await;
        inner
            .put(obj("acme", "o1", 1), Some(1))
            .await
            .expect("bump");

        let strong = cache
            .get(&t, &id, ReadConsistency::Strong)
            .await
            .expect("strong")
            .expect("present");
        assert_eq!(strong.version, 2, "strong read returns fresh value");

        // After the strong read the cache must reflect the new version.
        let after = cache
            .get(&t, &id, ReadConsistency::Eventual)
            .await
            .expect("post")
            .expect("present");
        assert_eq!(after.version, 2);
    }

    #[tokio::test]
    async fn invalidate_drops_entry() {
        let inner = Arc::new(InMemoryObjectStore::default());
        inner.put(obj("acme", "o1", 1), None).await.expect("seed");
        let cache = CachingObjectStore::new(inner.clone());

        let t = TenantId("acme".into());
        let id = ObjectId("o1".into());

        let _ = cache.get(&t, &id, ReadConsistency::Eventual).await;
        inner
            .put(obj("acme", "o1", 1), Some(1))
            .await
            .expect("bump");
        cache.invalidate(&t, &id).await;

        let after = cache
            .get(&t, &id, ReadConsistency::Eventual)
            .await
            .expect("post")
            .expect("present");
        assert_eq!(after.version, 2, "invalidate forces re-read");
    }
}
