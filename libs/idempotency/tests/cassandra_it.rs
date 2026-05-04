//! Cassandra testcontainer integration test for
//! [`CassandraIdempotencyStore`].
//!
//! Boots a Cassandra 5 testcontainer, creates the keyspace + table
//! from `migrations/0001_processed_events.cql` (with the keyspace
//! topology adjusted for the single-DC test container), and verifies
//! the same invariants as the Postgres test:
//!
//! 1. First call for an `event_id` returns `FirstSeen`, second
//!    returns `AlreadyProcessed`.
//! 2. Concurrent callers see exactly one `FirstSeen` — the LWT
//!    atomicity guarantee.
//! 3. The `idempotent` wrapper skips the closure on replay.
//!
//! Gated by `--features it-cassandra` AND marked `#[ignore]` so plain
//! `cargo test -p idempotency --all-features` doesn't pull a
//! 200 MB Cassandra image. Run with:
//!
//! ```text
//! cargo test -p idempotency --features it-cassandra -- --ignored --test-threads=1
//! ```

#![cfg(feature = "it-cassandra")]

use std::env;
use std::sync::Arc;
use std::time::{Duration, Instant};

use idempotency::cassandra::CassandraIdempotencyStore;
use idempotency::{IdempotencyStore, Outcome, idempotent};
use scylla::{Session, SessionBuilder};
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const KS_TABLE: &str = "idem.processed_events";
const DEFAULT_CASSANDRA_TEST_IMAGE: &str = "cassandra:5.0.2";

async fn boot_cassandra() -> (testcontainers::ContainerAsync<GenericImage>, Arc<Session>) {
    let image_ref = env::var("CASSANDRA_TEST_IMAGE")
        .ok()
        .filter(|v| !v.trim().is_empty())
        .unwrap_or_else(|| DEFAULT_CASSANDRA_TEST_IMAGE.to_string());
    let (name, tag) = image_ref
        .rsplit_once(':')
        .map(|(n, t)| (n.to_string(), t.to_string()))
        .unwrap_or((image_ref.clone(), "latest".to_string()));

    let image = GenericImage::new(name, tag)
        .with_exposed_port(9042.tcp())
        .with_wait_for(WaitFor::message_on_stdout(
            "Starting listening for CQL clients",
        ))
        .with_startup_timeout(Duration::from_secs(240));

    let container = image.start().await.expect("start cassandra container");
    let host = container
        .get_host()
        .await
        .expect("cassandra container host")
        .to_string();
    let port = container
        .get_host_port_ipv4(9042)
        .await
        .expect("cassandra mapped port");
    let endpoint = format!("{host}:{port}");

    // Connect with retry — the Cassandra container reports CQL
    // ready on stdout slightly before the listener actually accepts
    // connections.
    let started = Instant::now();
    let session = loop {
        match SessionBuilder::new().known_node(&endpoint).build().await {
            Ok(s) => break s,
            Err(err) if started.elapsed() < Duration::from_secs(90) => {
                tracing::debug!(?err, "waiting for CQL listener");
                tokio::time::sleep(Duration::from_secs(2)).await;
            }
            Err(err) => panic!("CQL connect timed out: {err:?}"),
        }
    };

    // Apply the schema. We replace the production NetworkTopology
    // strategy with SimpleStrategy since the test container is a
    // single node with no rack/dc topology configured.
    session
        .query(
            "CREATE KEYSPACE IF NOT EXISTS idem WITH replication = \
             {'class': 'SimpleStrategy', 'replication_factor': 1}",
            &[],
        )
        .await
        .expect("create keyspace");
    session
        .query(
            "CREATE TABLE IF NOT EXISTS idem.processed_events (\
             event_id uuid PRIMARY KEY, processed_at timestamp\
             ) WITH default_time_to_live = 2592000",
            &[],
        )
        .await
        .expect("create table");

    (container, Arc::new(session))
}

#[derive(Debug, thiserror::Error)]
#[error("processing failed")]
struct ProcessError;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker + cassandra image"]
async fn first_call_then_duplicate() {
    let (_c, session) = boot_cassandra().await;
    let store = CassandraIdempotencyStore::new(session, KS_TABLE);
    let id = Uuid::now_v7();
    assert_eq!(
        store.check_and_record(id).await.unwrap(),
        Outcome::FirstSeen
    );
    assert_eq!(
        store.check_and_record(id).await.unwrap(),
        Outcome::AlreadyProcessed
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker + cassandra image"]
async fn concurrent_callers_see_exactly_one_first_seen() {
    let (_c, session) = boot_cassandra().await;
    let store = Arc::new(CassandraIdempotencyStore::new(session, KS_TABLE));
    let id = Uuid::now_v7();

    let mut handles = Vec::with_capacity(8);
    for _ in 0..8 {
        let s = store.clone();
        handles.push(tokio::spawn(async move {
            s.check_and_record(id).await.unwrap()
        }));
    }
    let mut first = 0;
    for h in handles {
        if matches!(h.await.unwrap(), Outcome::FirstSeen) {
            first += 1;
        }
    }
    assert_eq!(first, 1, "LWT must serialise to exactly one winner");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker + cassandra image"]
async fn idempotent_wrapper_runs_closure_once() {
    let (_c, session) = boot_cassandra().await;
    let store = CassandraIdempotencyStore::new(session, KS_TABLE);
    let id = Uuid::now_v7();

    let r1 = idempotent::<_, _, _, _, ProcessError>(&store, id, || async { Ok::<_, _>(99) })
        .await
        .unwrap();
    assert_eq!(r1, Some(99));

    let r2 = idempotent::<_, _, _, _, ProcessError>(&store, id, || async {
        Err::<i32, _>(ProcessError)
    })
    .await
    .unwrap();
    assert!(r2.is_none());
}
