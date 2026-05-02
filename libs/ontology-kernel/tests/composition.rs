//! Unit tests for `ontology_kernel::domain::composition`.
//!
//! Exercises the pure trait-driven helpers against both the in-memory
//! noop store (round-trip semantics) and `mockall`-generated mocks
//! (interaction and error semantics). No I/O.

use ontology_kernel::domain::composition::{self, CompositionError};
use ontology_kernel::stores::mock::{MockLinkStore, MockObjectStore};
use storage_abstraction::repositories::{
    LinkTypeId, ObjectId, ObjectStore, Page, PagedResult, ReadConsistency, RepoError, TenantId,
    noop::InMemoryLinkStore,
};

fn t() -> TenantId {
    TenantId("tenant-A".into())
}
fn lt() -> LinkTypeId {
    LinkTypeId("owns".into())
}
fn obj(s: &str) -> ObjectId {
    ObjectId(s.into())
}

#[tokio::test]
async fn create_link_validates_inputs() {
    let store = InMemoryLinkStore::default();

    let err = composition::create_link(
        &store,
        TenantId("".into()),
        lt(),
        obj("a"),
        obj("b"),
        serde_json::json!({}),
        0,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, CompositionError::EmptyTenant));

    let err = composition::create_link(
        &store,
        t(),
        LinkTypeId("".into()),
        obj("a"),
        obj("b"),
        serde_json::json!({}),
        0,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, CompositionError::EmptyLinkType));

    let err = composition::create_link(
        &store,
        t(),
        lt(),
        obj("a"),
        obj("a"),
        serde_json::json!({}),
        0,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, CompositionError::SelfLoop));
}

#[tokio::test]
async fn create_link_is_idempotent_against_inmemory_store() {
    let store = InMemoryLinkStore::default();

    let inserted = composition::create_link(
        &store,
        t(),
        lt(),
        obj("a"),
        obj("b"),
        serde_json::json!({"weight": 1}),
        100,
    )
    .await
    .unwrap();
    assert!(inserted, "first put must report inserted");

    let second = composition::create_link(
        &store,
        t(),
        lt(),
        obj("a"),
        obj("b"),
        serde_json::json!({"weight": 2}),
        200,
    )
    .await
    .unwrap();
    assert!(!second, "duplicate put must report no-op");
}

#[tokio::test]
async fn delete_link_round_trip_against_inmemory_store() {
    let store = InMemoryLinkStore::default();
    composition::create_link(
        &store,
        t(),
        lt(),
        obj("a"),
        obj("b"),
        serde_json::json!({}),
        0,
    )
    .await
    .unwrap();

    let deleted = composition::delete_link(&store, t(), lt(), obj("a"), obj("b"))
        .await
        .unwrap();
    assert!(deleted);

    let again = composition::delete_link(&store, t(), lt(), obj("a"), obj("b"))
        .await
        .unwrap();
    assert!(!again, "second delete must be a no-op");
}

#[tokio::test]
async fn create_link_propagates_repo_errors_via_mock() {
    let mut mock = MockLinkStore::new();
    mock.expect_list_outgoing().returning(|_, _, _, _, _| {
        Ok(PagedResult {
            items: Vec::new(),
            next_token: None,
        })
    });
    mock.expect_put()
        .returning(|_| Err(RepoError::Backend("simulated cassandra timeout".into())));

    let err = composition::create_link(
        &mock,
        t(),
        lt(),
        obj("a"),
        obj("b"),
        serde_json::json!({}),
        0,
    )
    .await
    .unwrap_err();
    assert!(matches!(err, CompositionError::Repo(RepoError::Backend(_))));
}

#[tokio::test]
async fn mock_object_store_compiles_and_can_be_programmed() {
    // Smoke test that the generated MockObjectStore matches the trait
    // surface — particularly the new `list_by_owner` / `list_by_marking`
    // methods added in S1.2.a.
    let mut mock = MockObjectStore::new();
    mock.expect_list_by_type().returning(|_, _, _, _| {
        Ok(PagedResult {
            items: Vec::new(),
            next_token: None,
        })
    });
    let res = mock
        .list_by_type(
            &t(),
            &storage_abstraction::repositories::TypeId("Type".into()),
            Page {
                size: 10,
                token: None,
            },
            ReadConsistency::Eventual,
        )
        .await
        .unwrap();
    assert!(res.items.is_empty());
}
