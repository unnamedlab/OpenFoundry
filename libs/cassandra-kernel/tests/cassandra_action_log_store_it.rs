//! Integration test for [`CassandraActionLogStore`].
//!
//! Boots a real Cassandra container, applies the production-shaped
//! `actions_log.*` DDL, then validates append/retry idempotency and reads by
//! tenant recency, action id and object id.
//!
//! Marked `#[ignore]` because CI without Docker would otherwise fail. Run
//! locally with:
//!
//! ```text
//! cargo test -p cassandra-kernel --features repos --test cassandra_action_log_store_it -- --ignored
//! ```

#![cfg(feature = "repos")]

use std::{collections::HashSet, sync::Arc, time::Duration};

use cassandra_kernel::{ClusterConfig, SessionBuilder, repos::CassandraActionLogStore};
use storage_abstraction::repositories::{
    ActionLogEntry, ActionLogStore, ObjectId, Page, ReadConsistency, TenantId,
};
use testcontainers::{
    GenericImage,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn cassandra_action_log_store_appends_retries_and_reads() {
    let _ = tracing_subscriber::fmt::try_init();

    let image = GenericImage::new("cassandra", "5.0.2")
        .with_exposed_port(9042.tcp())
        .with_wait_for(WaitFor::message_on_stdout(
            "Starting listening for CQL clients",
        ));
    let container = image.start().await.expect("starting cassandra");
    let host = container.get_host().await.expect("host").to_string();
    let port = container
        .get_host_port_ipv4(9042)
        .await
        .expect("mapped port");

    tokio::time::sleep(Duration::from_secs(5)).await;

    let cfg = ClusterConfig {
        contact_points: vec![format!("{host}:{port}")],
        local_datacenter: "datacenter1".to_string(),
        ..ClusterConfig::dev_local()
    };
    let session = Arc::new(SessionBuilder::new(cfg).build().await.expect("session"));
    apply_ddl(&session).await;

    let store = CassandraActionLogStore::new(session);
    store.warm_up().await.expect("warm-up prepares");

    let tenant = TenantId("tenant-a".into());
    let action_id = Uuid::now_v7().to_string();
    let object = ObjectId(Uuid::now_v7().to_string());
    let subject = Uuid::now_v7().to_string();
    let now_ms = chrono::Utc::now().timestamp_millis();

    let first = ActionLogEntry {
        tenant: tenant.clone(),
        event_id: Some("event-1".into()),
        action_id: action_id.clone(),
        kind: "action_attempt".into(),
        subject: subject.clone(),
        object: Some(object.clone()),
        payload: serde_json::json!({ "status": "applied", "attempt": 1 }),
        recorded_at_ms: now_ms,
    };
    store.append(first.clone()).await.expect("append first");

    let retry = ActionLogEntry {
        event_id: Some("event-1".into()),
        action_id: Uuid::now_v7().to_string(),
        payload: serde_json::json!({ "status": "failed", "attempt": 99 }),
        recorded_at_ms: now_ms + 10_000,
        ..first.clone()
    };
    store.append(retry).await.expect("retry first");

    let recent = store
        .list_recent(
            &tenant,
            Page {
                size: 10,
                token: None,
            },
            ReadConsistency::Strong,
        )
        .await
        .expect("list recent");
    let event_one = recent
        .items
        .iter()
        .filter(|entry| entry.event_id.as_deref() == Some("event-1"))
        .collect::<Vec<_>>();
    assert_eq!(event_one.len(), 1);
    assert_eq!(event_one[0].action_id, action_id);
    assert_eq!(event_one[0].payload["attempt"], 1);

    for idx in 2..=3 {
        store
            .append(ActionLogEntry {
                tenant: tenant.clone(),
                event_id: Some(format!("event-{idx}")),
                action_id: action_id.clone(),
                kind: "action_attempt".into(),
                subject: subject.clone(),
                object: Some(object.clone()),
                payload: serde_json::json!({ "status": "applied", "attempt": idx }),
                recorded_at_ms: now_ms + idx as i64,
            })
            .await
            .expect("append additional action event");
    }

    let first_action_page = store
        .list_for_action(
            &tenant,
            &action_id,
            Page {
                size: 2,
                token: None,
            },
            ReadConsistency::Strong,
        )
        .await
        .expect("list action page 1");
    assert_eq!(first_action_page.items.len(), 2);
    assert!(first_action_page.next_token.is_some());

    let second_action_page = store
        .list_for_action(
            &tenant,
            &action_id,
            Page {
                size: 2,
                token: first_action_page.next_token,
            },
            ReadConsistency::Strong,
        )
        .await
        .expect("list action page 2");

    let seen_event_ids = first_action_page
        .items
        .into_iter()
        .chain(second_action_page.items)
        .map(|entry| entry.event_id.expect("event id"))
        .collect::<HashSet<_>>();
    assert_eq!(seen_event_ids.len(), 3);
    assert!(seen_event_ids.contains("event-1"));
    assert!(seen_event_ids.contains("event-2"));
    assert!(seen_event_ids.contains("event-3"));

    let by_object = store
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
        .expect("list by object");
    assert_eq!(by_object.items.len(), 3);
    assert!(
        by_object
            .items
            .iter()
            .all(|entry| entry.object.as_ref() == Some(&object))
    );
}

async fn apply_ddl(s: &scylla::Session) {
    s.query(
        "CREATE KEYSPACE IF NOT EXISTS actions_log WITH replication = \
         {'class': 'SimpleStrategy', 'replication_factor': 1}",
        &[],
    )
    .await
    .expect("create keyspace");

    for cql in [
        "CREATE TABLE IF NOT EXISTS actions_log.actions_log (
             tenant text,
             day_bucket date,
             applied_at timestamp,
             action_id timeuuid,
             kind text,
             actor_id uuid,
             subject text,
             target_object_id timeuuid,
             target_type_id text,
             payload text,
             status text,
             failure_type text,
             duration_ms int,
             event_id text,
             PRIMARY KEY ((tenant, day_bucket), applied_at, action_id)
         ) WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)",
        "CREATE TABLE IF NOT EXISTS actions_log.actions_by_object (
             tenant text,
             target_object_id timeuuid,
             applied_at timestamp,
             action_id timeuuid,
             kind text,
             actor_id uuid,
             subject text,
             payload text,
             event_id text,
             PRIMARY KEY ((tenant, target_object_id), applied_at, action_id)
         ) WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)",
        "CREATE TABLE IF NOT EXISTS actions_log.actions_by_action (
             tenant text,
             action_id timeuuid,
             day_bucket date,
             applied_at timestamp,
             event_id text,
             kind text,
             actor_id uuid,
             subject text,
             target_object_id timeuuid,
             payload text,
             PRIMARY KEY ((tenant, action_id, day_bucket), applied_at, event_id)
         ) WITH CLUSTERING ORDER BY (applied_at DESC, event_id ASC)",
        "CREATE TABLE IF NOT EXISTS actions_log.actions_by_event (
             tenant text,
             event_id text,
             action_id timeuuid,
             kind text,
             actor_id uuid,
             subject text,
             target_object_id timeuuid,
             payload text,
             applied_at timestamp,
             day_bucket date,
             PRIMARY KEY ((tenant, event_id))
         )",
    ] {
        s.query(cql, &[]).await.expect("create table");
    }
}
