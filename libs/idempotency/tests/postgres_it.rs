//! Postgres testcontainer integration test for [`PgIdempotencyStore`].
//!
//! Boots a Postgres 16 testcontainer, applies the
//! `migrations/0001_processed_events.sql` schema, and exercises the
//! main invariants of ADR-0038:
//!
//! 1. First call for an `event_id` returns `FirstSeen`, second call
//!    returns `AlreadyProcessed`.
//! 2. Concurrent callers racing on the same `event_id` see exactly
//!    one `FirstSeen` (atomicity of `INSERT … ON CONFLICT DO NOTHING
//!    RETURNING`).
//! 3. The `idempotent` wrapper composes correctly: closure runs once
//!    and not on replay.
//! 4. Custom `&'static str` table names work (the lib intentionally
//!    interpolates the slot at construction time).
//!
//! Gated by `--features it-postgres` so the default `cargo test
//! -p idempotency` stays a no-IO unit run.

#![cfg(feature = "it-postgres")]

use std::sync::Arc;

use idempotency::postgres::PgIdempotencyStore;
use idempotency::{IdempotencyStore, Outcome, idempotent};
use sqlx::{Connection, PgConnection, Row, postgres::PgPoolOptions};
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const MIGRATION: &str = include_str!("../migrations/0001_processed_events.sql");
const TABLE: &str = "idem.processed_events";

async fn boot_pg() -> (testcontainers::ContainerAsync<GenericImage>, sqlx::PgPool) {
    let image = GenericImage::new("postgres", "16-alpine")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_USER", "of")
        .with_env_var("POSTGRES_PASSWORD", "of")
        .with_env_var("POSTGRES_DB", "idem_test");

    let pg = image.start().await.expect("start postgres testcontainer");
    let host_port = pg
        .get_host_port_ipv4(5432)
        .await
        .expect("expose host port for postgres");
    let url = format!("postgres://of:of@127.0.0.1:{host_port}/idem_test");

    let mut admin = PgConnection::connect(&url).await.expect("connect admin");
    sqlx::raw_sql(MIGRATION)
        .execute(&mut admin)
        .await
        .expect("apply idempotency migration");
    drop(admin);

    let pool = PgPoolOptions::new()
        .max_connections(8)
        .connect(&url)
        .await
        .expect("connect pool");
    (pg, pool)
}

#[derive(Debug, thiserror::Error)]
#[error("processing failed")]
struct ProcessError;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn first_call_then_duplicate() {
    let (_pg, pool) = boot_pg().await;
    let store = PgIdempotencyStore::new(pool.clone(), TABLE);
    let id = Uuid::now_v7();

    assert_eq!(
        store.check_and_record(id).await.unwrap(),
        Outcome::FirstSeen
    );
    assert_eq!(
        store.check_and_record(id).await.unwrap(),
        Outcome::AlreadyProcessed
    );

    // The row was actually persisted with a non-NULL processed_at.
    let row = sqlx::query("SELECT processed_at FROM idem.processed_events WHERE event_id = $1")
        .bind(id)
        .fetch_one(&pool)
        .await
        .unwrap();
    let _ts: chrono::DateTime<chrono::Utc> = row.try_get("processed_at").unwrap();
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn concurrent_callers_see_exactly_one_first_seen() {
    let (_pg, pool) = boot_pg().await;
    let store = Arc::new(PgIdempotencyStore::new(pool, TABLE));
    let id = Uuid::now_v7();

    // 16 racers on the same event id. Atomicity of `INSERT … ON
    // CONFLICT DO NOTHING RETURNING` MUST give us exactly one
    // `FirstSeen` and 15 `AlreadyProcessed`.
    let mut handles = Vec::with_capacity(16);
    for _ in 0..16 {
        let s = store.clone();
        handles.push(tokio::spawn(async move {
            s.check_and_record(id).await.unwrap()
        }));
    }
    let mut first = 0;
    let mut dupes = 0;
    for h in handles {
        match h.await.unwrap() {
            Outcome::FirstSeen => first += 1,
            Outcome::AlreadyProcessed => dupes += 1,
        }
    }
    assert_eq!(first, 1, "exactly one racer must win, got {first}");
    assert_eq!(dupes, 15);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn idempotent_wrapper_runs_closure_once() {
    let (_pg, pool) = boot_pg().await;
    let store = PgIdempotencyStore::new(pool, TABLE);
    let id = Uuid::now_v7();

    let r1 = idempotent::<_, _, _, _, ProcessError>(&store, id, || async { Ok::<_, _>(42) })
        .await
        .unwrap();
    assert_eq!(r1, Some(42));

    // On replay, the closure must NOT run, even though it would
    // error if invoked.
    let r2 = idempotent::<_, _, _, _, ProcessError>(&store, id, || async {
        Err::<i32, _>(ProcessError)
    })
    .await
    .unwrap();
    assert!(r2.is_none());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn distinct_event_ids_are_independent() {
    let (_pg, pool) = boot_pg().await;
    let store = PgIdempotencyStore::new(pool, TABLE);

    let a = Uuid::now_v7();
    let b = Uuid::now_v7();
    assert_eq!(store.check_and_record(a).await.unwrap(), Outcome::FirstSeen);
    assert_eq!(store.check_and_record(b).await.unwrap(), Outcome::FirstSeen);
    assert_eq!(
        store.check_and_record(a).await.unwrap(),
        Outcome::AlreadyProcessed
    );
    assert_eq!(
        store.check_and_record(b).await.unwrap(),
        Outcome::AlreadyProcessed
    );
}
