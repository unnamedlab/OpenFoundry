//! Integration test for [`CassandraObjectStore`].
//!
//! Boots a real Cassandra container, applies the production
//! `ontology_objects.*` DDL, inserts 10 000 objects across 10 types,
//! and walks `list_by_type` with paging to validate the contract.
//!
//! Marked `#[ignore]` because CI without docker would otherwise
//! fail. Run locally with:
//!
//! ```text
//! cargo test -p cassandra-kernel --features repos -- --ignored
//! ```

#![cfg(feature = "repos")]

mod support;

use cassandra_kernel::repos::CassandraObjectStore;
use futures::{StreamExt, stream};
use storage_abstraction::repositories::{
    MarkingId, Object, ObjectId, ObjectStore, OwnerId, Page, PutOutcome, ReadConsistency, TenantId,
    TypeId,
};
use uuid::Uuid;

const KS: &str = "ontology_objects";
const TOTAL: usize = 10_000;
const TYPES: usize = 10;
const PER_TYPE: usize = TOTAL / TYPES;
const PARALLELISM: usize = 32;
const PAGE_SIZE: u32 = 200;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn cassandra_object_store_inserts_10k_and_pages_by_type() {
    let _ = tracing_subscriber::fmt::try_init();

    // -- 1. Boot Cassandra ---------------------------------------------
    let cassandra = support::start_cassandra().await;
    let session = cassandra.session.clone();

    // -- 2. Apply DDL --------------------------------------------------
    apply_ddl(session.as_ref()).await;

    // -- 3. Build the store -------------------------------------------
    let store = CassandraObjectStore::new(session.clone());
    store.warm_up().await.expect("warm-up prepares");

    // -- 4. Insert 10 000 objects across 10 types ---------------------
    let tenant = TenantId("acme".to_string());
    let owner = OwnerId(Uuid::new_v4().to_string());
    let now_ms = chrono::Utc::now().timestamp_millis();

    let inserts = (0..TOTAL).map(|i| {
        let type_id = TypeId(format!("type_{}", i % TYPES));
        let obj = Object {
            tenant: tenant.clone(),
            id: ObjectId(Uuid::now_v7().to_string()),
            type_id,
            version: 0,
            payload: serde_json::json!({"i": i, "label": format!("row-{i}")}),
            organization_id: None,
            created_at_ms: Some(now_ms + i as i64),
            updated_at_ms: now_ms + i as i64,
            owner: Some(owner.clone()),
            markings: vec![MarkingId("PUBLIC".to_string())],
        };
        let store = &store;
        async move {
            let outcome = store.put(obj, None).await.expect("put");
            assert!(matches!(outcome, PutOutcome::Inserted));
        }
    });

    stream::iter(inserts)
        .buffer_unordered(PARALLELISM)
        .for_each(|()| async {})
        .await;

    // -- 5. Page through one type and assert exactly PER_TYPE rows ----
    let target_type = TypeId("type_3".to_string());
    let mut seen_ids = std::collections::HashSet::new();
    let mut token: Option<String> = None;
    let mut pages = 0usize;
    loop {
        let page = Page {
            size: PAGE_SIZE,
            token: token.clone(),
        };
        let res = store
            .list_by_type(&tenant, &target_type, page, ReadConsistency::Eventual)
            .await
            .expect("list_by_type");
        pages += 1;
        for obj in res.items {
            assert_eq!(obj.type_id, target_type);
            assert!(seen_ids.insert(obj.id.0), "duplicate id across pages");
        }
        token = res.next_token;
        if token.is_none() {
            break;
        }
        assert!(
            pages < 50,
            "pagination did not terminate (likely token loop)"
        );
    }

    assert_eq!(
        seen_ids.len(),
        PER_TYPE,
        "expected {PER_TYPE} rows for type_3"
    );
    assert!(pages >= PER_TYPE / PAGE_SIZE as usize);

    // -- 6. Smoke-test get + version conflict -------------------------
    let some_id = ObjectId(seen_ids.iter().next().unwrap().clone());
    let fetched = store
        .get(&tenant, &some_id, ReadConsistency::Strong)
        .await
        .expect("get")
        .expect("row exists");
    assert_eq!(fetched.version, 1);

    // Conflict: try to update with the wrong expected_version.
    let stale = Object {
        version: 0,
        ..fetched.clone()
    };
    let outcome = store.put(stale, Some(99)).await.expect("put-conflict");
    match outcome {
        PutOutcome::VersionConflict {
            expected_version,
            actual_version,
        } => {
            assert_eq!(expected_version, 99);
            assert_eq!(actual_version, 1);
        }
        other => panic!("expected VersionConflict, got {other:?}"),
    }
}

/// Apply the production `ontology_objects.*` schema to the test
/// container. Inlined rather than parsed from `services/.../*.cql`
/// so this test stays self-contained.
async fn apply_ddl(s: &scylla::Session) {
    s.query(
        "CREATE KEYSPACE IF NOT EXISTS ontology_objects WITH replication = \
         {'class': 'SimpleStrategy', 'replication_factor': 1}",
        &[],
    )
    .await
    .expect("create keyspace");

    for cql in [
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_id (
             tenant text, object_id timeuuid, type_id text, owner_id uuid,
             properties text, marking frozen<set<text>>, organization_id uuid,
             revision_number bigint STATIC, created_at timestamp,
             updated_at timestamp, deleted boolean,
             PRIMARY KEY ((tenant, object_id))
         )",
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_type (
             tenant text, type_id text, updated_at timestamp, object_id timeuuid,
             owner_id uuid, marking frozen<set<text>>,
             properties_summary text, deleted boolean,
             PRIMARY KEY ((tenant, type_id), updated_at, object_id)
         ) WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)",
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_owner (
             tenant text, owner_id uuid, type_id text, object_id timeuuid,
             updated_at timestamp, deleted boolean,
             PRIMARY KEY ((tenant, owner_id), type_id, object_id)
         )",
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_marking (
             tenant text, marking_id text, object_id timeuuid, type_id text,
             owner_id uuid, updated_at timestamp, deleted boolean,
             PRIMARY KEY ((tenant, marking_id), object_id)
         )",
    ] {
        s.query(cql, &[]).await.expect("create table");
    }
    let _ = KS;
}
