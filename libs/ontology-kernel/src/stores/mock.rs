//! `mockall`-generated mocks of the `storage-abstraction` traits.
//!
//! The traits live in another crate, so we use the `mockall::mock!` macro
//! (rather than `#[automock]`) to mirror their surface here. The mock
//! struct names intentionally differ from the trait names — `mockall`
//! cannot generate `impl Trait for Trait`, so we name the underlying
//! structs `*Impl` and re-export them as the friendlier `MockObjectStore`
//! / `MockLinkStore` / `MockActionLogStore` aliases.

use async_trait::async_trait;
use mockall::mock;
use storage_abstraction::repositories::{
    ActionLogEntry, ActionLogStore, Link, LinkStore, LinkTypeId, MarkingId, Object, ObjectId,
    ObjectStore, OwnerId, Page, PagedResult, PutOutcome, ReadConsistency, RepoResult, TenantId,
    TypeId,
};

mock! {
    pub ObjectStoreImpl {}

    #[async_trait]
    impl ObjectStore for ObjectStoreImpl {
        async fn get(
            &self,
            tenant: &TenantId,
            id: &ObjectId,
            consistency: ReadConsistency,
        ) -> RepoResult<Option<Object>>;

        async fn put(
            &self,
            object: Object,
            expected_version: Option<u64>,
        ) -> RepoResult<PutOutcome>;

        async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool>;

        async fn list_by_type(
            &self,
            tenant: &TenantId,
            type_id: &TypeId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Object>>;

        async fn list_by_owner(
            &self,
            tenant: &TenantId,
            owner: &OwnerId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Object>>;

        async fn list_by_marking(
            &self,
            tenant: &TenantId,
            marking: &MarkingId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Object>>;
    }
}

mock! {
    pub LinkStoreImpl {}

    #[async_trait]
    impl LinkStore for LinkStoreImpl {
        async fn put(&self, link: Link) -> RepoResult<()>;

        async fn delete(
            &self,
            tenant: &TenantId,
            link_type: &LinkTypeId,
            from: &ObjectId,
            to: &ObjectId,
        ) -> RepoResult<bool>;

        async fn list_outgoing(
            &self,
            tenant: &TenantId,
            link_type: &LinkTypeId,
            from: &ObjectId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Link>>;

        async fn list_incoming(
            &self,
            tenant: &TenantId,
            link_type: &LinkTypeId,
            to: &ObjectId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<Link>>;
    }
}

mock! {
    pub ActionLogStoreImpl {}

    #[async_trait]
    impl ActionLogStore for ActionLogStoreImpl {
        async fn append(&self, entry: ActionLogEntry) -> RepoResult<()>;

        async fn list_recent(
            &self,
            tenant: &TenantId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<ActionLogEntry>>;

        async fn list_for_object(
            &self,
            tenant: &TenantId,
            object: &ObjectId,
            page: Page,
            consistency: ReadConsistency,
        ) -> RepoResult<PagedResult<ActionLogEntry>>;
    }
}

pub type MockObjectStore = MockObjectStoreImpl;
pub type MockLinkStore = MockLinkStoreImpl;
pub type MockActionLogStore = MockActionLogStoreImpl;
