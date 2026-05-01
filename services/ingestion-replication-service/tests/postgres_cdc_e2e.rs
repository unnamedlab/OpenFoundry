//! End-to-end Postgres CDC test.
//!
//! Spins up `debezium/postgres:16-alpine` (chosen because the official
//! `postgres:16` image already ships with the built-in `test_decoding`
//! plugin and we set `wal_level=logical` via `-c`), lets the worker
//! discover/create its slot+publication, applies an
//! `INSERT` / `UPDATE` / `DELETE` cycle, and asserts that:
//!
//! * the in-memory publisher receives one envelope per row change,
//! * each envelope carries the `cdc.op`, `cdc.lsn`, `cdc.tx_id` and
//!   `cdc.table` headers,
//! * the persisted `ingestion_checkpoints.last_lsn` advances monotonically,
//! * a worker restart resumes from the persisted LSN and does **not**
//!   replay events that were already published before the previous run
//!   shut down.
//!
//! Marked `#[ignore]` so plain `cargo test` stays fast in CI without
//! Docker; opt in with `cargo test -p ingestion-replication-service
//! --test postgres_cdc_e2e -- --ignored --nocapture`.

#![cfg(test)]

use std::time::Duration;

use ingestion_replication_service::cdc::{
    CdcOp, EventPublisher, InMemoryPublisher, PostgresCdcConfig, load_or_init_checkpoint,
    poll_once, run,
};
use sqlx::postgres::PgPoolOptions;
use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};
use tokio::time::timeout;
use uuid::Uuid;

const PG_USER: &str = "openfoundry";
const PG_PASSWORD: &str = "openfoundry";
const PG_DB: &str = "cdc_e2e";

/// Boot one Postgres container with `wal_level=logical`. Returns the
/// container handle (kept alive by the caller) and a libpq URL for the
/// host.
async fn boot_postgres()
-> (testcontainers::ContainerAsync<GenericImage>, String) {
    let image = GenericImage::new("debezium/postgres", "16-alpine")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ));
    let container = image
        .with_env_var("POSTGRES_USER", PG_USER)
        .with_env_var("POSTGRES_PASSWORD", PG_PASSWORD)
        .with_env_var("POSTGRES_DB", PG_DB)
        // debezium/postgres image is convenient because it bundles the
        // logical-decoding configuration knobs we need; we still flip
        // wal_level explicitly.
        .with_cmd([
            "postgres",
            "-c",
            "wal_level=logical",
            "-c",
            "max_replication_slots=8",
            "-c",
            "max_wal_senders=8",
        ])
        .start()
        .await
        .expect("start postgres container");
    let host = container.get_host().await.expect("container host");
    let port = container
        .get_host_port_ipv4(5432)
        .await
        .expect("container port");
    let url = format!("postgres://{PG_USER}:{PG_PASSWORD}@{host}:{port}/{PG_DB}");
    (container, url)
}

/// Wait until the in-memory sink has received at least `n` envelopes or
/// the deadline elapses. Polls on a short interval to keep the test
/// snappy.
async fn wait_for_envelopes(
    publisher: &InMemoryPublisher,
    n: usize,
    deadline: Duration,
) -> Vec<ingestion_replication_service::cdc::CdcEnvelope> {
    match timeout(deadline, async {
        loop {
            let snap = publisher.snapshot().await;
            if snap.len() >= n {
                return snap;
            }
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
    })
    .await
    {
        Ok(v) => v,
        Err(_) => {
            let snap = publisher.snapshot().await;
            panic!(
                "timed out waiting for {n} envelopes; got {} so far: {:?}",
                snap.len(),
                snap.iter()
                    .map(|e| (e.headers.get("cdc.op").cloned(), e.headers.get("cdc.lsn").cloned()))
                    .collect::<Vec<_>>()
            );
        }
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires Docker (debezium/postgres:16-alpine)"]
async fn postgres_insert_update_delete_round_trip() {
    let (_pg, upstream_url) = boot_postgres().await;

    // Source schema: a tiny `orders` table that we mutate from the test.
    let upstream_pool = PgPoolOptions::new()
        .max_connections(2)
        .connect(&upstream_url)
        .await
        .expect("connect upstream");
    sqlx::query(
        r#"CREATE TABLE orders (
            id BIGINT PRIMARY KEY,
            status TEXT NOT NULL,
            total NUMERIC(10, 2) NOT NULL
        )"#,
    )
    .execute(&upstream_pool)
    .await
    .expect("create orders");
    // `test_decoding` only emits the column data corresponding to the
    // table's REPLICA IDENTITY. FULL is the only mode that includes every
    // column for old/new — vital for downstream consumers that need
    // before-images.
    sqlx::query("ALTER TABLE orders REPLICA IDENTITY FULL")
        .execute(&upstream_pool)
        .await
        .expect("set replica identity");

    // Metadata DB: the worker reuses the same Postgres for simplicity (a
    // separate schema would be the production pattern; we apply the
    // ingestion-replication-service migrations against the DB here).
    let metadata_pool = upstream_pool.clone();
    sqlx::query(
        r#"CREATE TABLE IF NOT EXISTS ingestion_checkpoints (
            subscription_id UUID PRIMARY KEY,
            slot_name TEXT NOT NULL,
            publication_name TEXT NOT NULL,
            last_lsn TEXT NULL,
            last_event_at TIMESTAMPTZ NULL,
            records_observed BIGINT NOT NULL DEFAULT 0,
            records_applied BIGINT NOT NULL DEFAULT 0,
            last_tx_id BIGINT NULL,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )"#,
    )
    .execute(&metadata_pool)
    .await
    .expect("create checkpoints");

    let subscription_id = Uuid::now_v7();
    let config = PostgresCdcConfig::with_defaults(
        subscription_id,
        upstream_url.clone(),
        "orders_slot",
        "orders_pub",
        vec!["public.orders".into()],
        "data.cdc.orders".to_string(),
    );
    let publisher = InMemoryPublisher::new();

    // ── 1. Start the worker ─────────────────────────────────────────────
    let (shutdown_tx, shutdown_rx) = tokio::sync::oneshot::channel::<()>();
    let worker_pool_meta = metadata_pool.clone();
    let worker_pool_up = upstream_pool.clone();
    let worker_publisher = publisher.clone();
    let worker_config = config.clone();
    let worker = tokio::spawn(async move {
        run(
            worker_pool_meta,
            worker_pool_up,
            worker_config,
            worker_publisher,
            Box::pin(async move {
                let _ = shutdown_rx.await;
            }),
        )
        .await
    });

    // Give the worker a beat to create the slot+publication so the
    // following INSERT is captured.
    tokio::time::sleep(Duration::from_millis(500)).await;

    // ── 2. INSERT / UPDATE / DELETE ─────────────────────────────────────
    sqlx::query("INSERT INTO orders (id, status, total) VALUES ($1, $2, $3)")
        .bind(1_i64)
        .bind("pending")
        .bind(99.5_f64)
        .execute(&upstream_pool)
        .await
        .expect("insert");
    sqlx::query("UPDATE orders SET status = $1 WHERE id = $2")
        .bind("paid")
        .bind(1_i64)
        .execute(&upstream_pool)
        .await
        .expect("update");
    sqlx::query("DELETE FROM orders WHERE id = $1")
        .bind(1_i64)
        .execute(&upstream_pool)
        .await
        .expect("delete");

    let envelopes = wait_for_envelopes(&publisher, 3, Duration::from_secs(15)).await;

    let _ = shutdown_tx.send(());
    let _ = worker.await;

    // ── 3. Envelope assertions ──────────────────────────────────────────
    assert_eq!(envelopes.len(), 3, "expected exactly 3 envelopes (insert/update/delete)");
    let ops: Vec<&str> = envelopes
        .iter()
        .map(|e| e.headers.get("cdc.op").map(String::as_str).unwrap_or(""))
        .collect();
    assert_eq!(
        ops,
        vec![
            CdcOp::Insert.as_header(),
            CdcOp::Update.as_header(),
            CdcOp::Delete.as_header()
        ],
        "operations must arrive in source order"
    );
    for env in &envelopes {
        assert_eq!(env.topic, "data.cdc.orders");
        assert!(env.headers.contains_key("cdc.lsn"), "missing cdc.lsn header");
        assert!(env.headers.contains_key("cdc.tx_id"), "missing cdc.tx_id header");
        assert_eq!(
            env.headers.get("cdc.table").map(String::as_str),
            Some("orders"),
            "test_decoding should report the table name"
        );
    }

    // LSNs are textually monotonic when normalised — confirm by parsing.
    let lsns: Vec<u64> = envelopes
        .iter()
        .map(|e| parse_lsn(e.headers.get("cdc.lsn").unwrap()))
        .collect();
    assert!(
        lsns.windows(2).all(|w| w[0] < w[1]),
        "LSNs should strictly increase across INSERT/UPDATE/DELETE: {lsns:?}"
    );

    // ── 4. Checkpoint persistence ───────────────────────────────────────
    let checkpoint = load_or_init_checkpoint(&metadata_pool, &config)
        .await
        .expect("checkpoint");
    let last_lsn = checkpoint
        .last_lsn
        .as_deref()
        .expect("worker must have persisted at least one LSN");
    // The persisted LSN is *at least* the LSN of the last published row.
    // It can be strictly greater because the worker continues polling
    // after the orders DML and advances the slot past WAL records it
    // filtered out (e.g. `ingestion_checkpoints` writes when metadata
    // and upstream share a database, as in this test).
    assert!(
        parse_lsn(last_lsn) >= *lsns.last().unwrap(),
        "checkpoint LSN {last_lsn} must be >= last published LSN"
    );
    // Records-applied counts only what we actually published downstream.
    assert_eq!(checkpoint.records_applied, 3);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires Docker (debezium/postgres:16-alpine)"]
async fn worker_resumes_from_last_lsn_after_restart() {
    let (_pg, upstream_url) = boot_postgres().await;

    let upstream_pool = PgPoolOptions::new()
        .max_connections(2)
        .connect(&upstream_url)
        .await
        .expect("connect upstream");
    sqlx::query(
        r#"CREATE TABLE events (
            id BIGSERIAL PRIMARY KEY,
            payload TEXT NOT NULL
        )"#,
    )
    .execute(&upstream_pool)
    .await
    .expect("create events");

    let metadata_pool = upstream_pool.clone();
    sqlx::query(
        r#"CREATE TABLE IF NOT EXISTS ingestion_checkpoints (
            subscription_id UUID PRIMARY KEY,
            slot_name TEXT NOT NULL,
            publication_name TEXT NOT NULL,
            last_lsn TEXT NULL,
            last_event_at TIMESTAMPTZ NULL,
            records_observed BIGINT NOT NULL DEFAULT 0,
            records_applied BIGINT NOT NULL DEFAULT 0,
            last_tx_id BIGINT NULL,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        )"#,
    )
    .execute(&metadata_pool)
    .await
    .expect("create checkpoints");

    let subscription_id = Uuid::now_v7();
    let config = PostgresCdcConfig::with_defaults(
        subscription_id,
        upstream_url.clone(),
        "events_slot",
        "events_pub",
        vec!["public.events".into()],
        "data.cdc.events".to_string(),
    );

    // ── Run 1: insert two rows, capture them, shut down ────────────────
    let publisher_a = InMemoryPublisher::new();
    let (stop_a_tx, stop_a_rx) = tokio::sync::oneshot::channel::<()>();
    let cfg_a = config.clone();
    let pub_a = publisher_a.clone();
    let meta_a = metadata_pool.clone();
    let up_a = upstream_pool.clone();
    let task_a = tokio::spawn(async move {
        run(
            meta_a,
            up_a,
            cfg_a,
            pub_a,
            Box::pin(async move {
                let _ = stop_a_rx.await;
            }),
        )
        .await
    });
    tokio::time::sleep(Duration::from_millis(500)).await;
    sqlx::query("INSERT INTO events (payload) VALUES ($1), ($2)")
        .bind("first")
        .bind("second")
        .execute(&upstream_pool)
        .await
        .expect("insert batch 1");
    let first_batch = wait_for_envelopes(&publisher_a, 2, Duration::from_secs(15)).await;
    let _ = stop_a_tx.send(());
    let _ = task_a.await;
    assert_eq!(first_batch.len(), 2);
    let last_lsn_after_run_1 = parse_lsn(first_batch.last().unwrap().headers.get("cdc.lsn").unwrap());

    // ── While the worker is down, write more data ──────────────────────
    sqlx::query("INSERT INTO events (payload) VALUES ($1)")
        .bind("third")
        .execute(&upstream_pool)
        .await
        .expect("insert while worker down");

    // ── Run 2: same subscription_id => must resume from checkpoint ─────
    let publisher_b = InMemoryPublisher::new();
    let (stop_b_tx, stop_b_rx) = tokio::sync::oneshot::channel::<()>();
    let cfg_b = config.clone();
    let pub_b = publisher_b.clone();
    let meta_b = metadata_pool.clone();
    let up_b = upstream_pool.clone();
    let task_b = tokio::spawn(async move {
        run(
            meta_b,
            up_b,
            cfg_b,
            pub_b,
            Box::pin(async move {
                let _ = stop_b_rx.await;
            }),
        )
        .await
    });
    let resumed = wait_for_envelopes(&publisher_b, 1, Duration::from_secs(15)).await;
    let _ = stop_b_tx.send(());
    let _ = task_b.await;

    // Run 2 must see exactly the rows produced after the first shutdown.
    // The "first" / "second" payloads must NOT reappear.
    assert_eq!(resumed.len(), 1, "resumed worker must not replay already-acked rows");
    let third_payload = std::str::from_utf8(&resumed[0].payload).unwrap();
    assert!(
        third_payload.contains("third"),
        "expected the post-shutdown row, got: {third_payload}"
    );
    let third_lsn = parse_lsn(resumed[0].headers.get("cdc.lsn").unwrap());
    assert!(
        third_lsn > last_lsn_after_run_1,
        "resumed LSN must be strictly greater than the last checkpoint"
    );
}

/// Standalone copy of the LSN parser — kept inline so the test does not
/// depend on a private helper of the lib crate.
fn parse_lsn(text: &str) -> u64 {
    let (hi, lo) = text.trim().split_once('/').expect("LSN must contain '/'");
    let hi_n = u64::from_str_radix(hi, 16).expect("hi half hex");
    let lo_n = u64::from_str_radix(lo, 16).expect("lo half hex");
    (hi_n << 32) | lo_n
}

/// Expose a quick stand-alone polling helper so callers that want to drive
/// a *single* iteration (e.g. for deterministic step-wise tests in the
/// future) do not have to reach into private internals. Currently unused
/// by the Docker-gated tests above; kept here so it shares the same module
/// dependencies and is built whenever `cargo test --test postgres_cdc_e2e`
/// is invoked.
#[allow(dead_code)]
async fn pump_until_empty(
    metadata_pool: &sqlx::PgPool,
    upstream_pool: &sqlx::PgPool,
    config: &PostgresCdcConfig,
    publisher: &impl EventPublisher,
) {
    while poll_once(metadata_pool, upstream_pool, config, publisher)
        .await
        .expect("poll_once")
        > 0
    {}
}
