//! Integration test for [`CassandraLinkStore`].
//!
//! Boots a real Cassandra container, applies the production
//! `ontology_indexes.links_*` DDL, then validates create/list/delete,
//! idempotent create semantics and opaque paging.
//!
//! Marked `#[ignore]` because CI without Docker would otherwise fail. Run
//! locally with:
//!
//! ```text
//! cargo test -p cassandra-kernel --features repos --test cassandra_link_store_it -- --ignored
//! ```

#![cfg(feature = "repos")]

mod support;

use std::collections::HashSet;

use cassandra_kernel::repos::CassandraLinkStore;
use storage_abstraction::repositories::{
    Link, LinkStore, LinkTypeId, ObjectId, Page, ReadConsistency, TenantId,
};
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn cassandra_link_store_round_trips_and_pages() {
    let _ = tracing_subscriber::fmt::try_init();

    let cassandra = support::start_cassandra().await;
    let session = cassandra.session.clone();
    apply_ddl(session.as_ref()).await;

    let store = CassandraLinkStore::new(session);
    store.warm_up().await.expect("warm-up prepares");

    let tenant = TenantId("tenant-a".into());
    let link_type = LinkTypeId("owns".into());
    let source = ObjectId(Uuid::now_v7().to_string());
    let targets = (0..3)
        .map(|_| ObjectId(Uuid::now_v7().to_string()))
        .collect::<Vec<_>>();
    let now_ms = chrono::Utc::now().timestamp_millis();

    for (idx, target) in targets.iter().enumerate() {
        store
            .put(Link {
                tenant: tenant.clone(),
                link_type: link_type.clone(),
                from: source.clone(),
                to: target.clone(),
                payload: Some(serde_json::json!({ "rank": idx })),
                created_at_ms: now_ms + idx as i64,
            })
            .await
            .expect("put link");
    }

    // Same logical link should be a no-op; the original payload must remain.
    store
        .put(Link {
            tenant: tenant.clone(),
            link_type: link_type.clone(),
            from: source.clone(),
            to: targets[0].clone(),
            payload: Some(serde_json::json!({ "rank": 99 })),
            created_at_ms: now_ms + 99,
        })
        .await
        .expect("idempotent put");

    let mut token = None;
    let mut seen = HashSet::new();
    let mut first_payload = None;
    loop {
        let page = store
            .list_outgoing(
                &tenant,
                &link_type,
                &source,
                Page {
                    size: 1,
                    token: token.clone(),
                },
                ReadConsistency::Strong,
            )
            .await
            .expect("list outgoing");
        for link in page.items {
            if link.to == targets[0] {
                first_payload = link.payload.clone();
            }
            assert_eq!(link.from, source);
            assert!(seen.insert(link.to.0), "duplicate link across pages");
        }
        token = page.next_token;
        if token.is_none() {
            break;
        }
    }

    assert_eq!(seen.len(), targets.len());
    assert_eq!(first_payload, Some(serde_json::json!({ "rank": 0 })));

    let incoming = store
        .list_incoming(
            &tenant,
            &link_type,
            &targets[1],
            Page {
                size: 10,
                token: None,
            },
            ReadConsistency::Eventual,
        )
        .await
        .expect("list incoming");
    assert_eq!(incoming.items.len(), 1);
    assert_eq!(incoming.items[0].from, source);
    assert_eq!(incoming.items[0].to, targets[1]);

    assert!(
        store
            .delete(&tenant, &link_type, &source, &targets[1])
            .await
            .expect("delete existing")
    );
    assert!(
        !store
            .delete(&tenant, &link_type, &source, &targets[1])
            .await
            .expect("delete is idempotent")
    );

    let incoming = store
        .list_incoming(
            &tenant,
            &link_type,
            &targets[1],
            Page {
                size: 10,
                token: None,
            },
            ReadConsistency::Strong,
        )
        .await
        .expect("list incoming after delete");
    assert!(incoming.items.is_empty());
}

async fn apply_ddl(s: &scylla::Session) {
    s.query(
        "CREATE KEYSPACE IF NOT EXISTS ontology_indexes WITH replication = \
         {'class': 'SimpleStrategy', 'replication_factor': 1}",
        &[],
    )
    .await
    .expect("create keyspace");

    for cql in [
        "CREATE TABLE IF NOT EXISTS ontology_indexes.links_outgoing (
             tenant text,
             source_id timeuuid,
             link_type text,
             target_id timeuuid,
             target_type text,
             properties text,
             created_at timestamp,
             PRIMARY KEY ((tenant, source_id), link_type, target_id)
         ) WITH CLUSTERING ORDER BY (link_type ASC, target_id ASC)",
        "CREATE TABLE IF NOT EXISTS ontology_indexes.links_incoming (
             tenant text,
             target_id timeuuid,
             link_type text,
             source_id timeuuid,
             source_type text,
             properties text,
             created_at timestamp,
             PRIMARY KEY ((tenant, target_id), link_type, source_id)
         ) WITH CLUSTERING ORDER BY (link_type ASC, source_id ASC)",
    ] {
        s.query(cql, &[]).await.expect("create table");
    }
}
