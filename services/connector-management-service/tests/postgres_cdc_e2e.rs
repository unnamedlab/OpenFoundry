//! Tarea 9 — E2E harness for the Postgres CDC discovery path.
//!
//! Spins up a `debezium/postgres:16-alpine` container with `wal_level=logical`,
//! provisions a publication + replication slot, and verifies that the same
//! libpq-style driver (`sqlx::PgPool`) the connector uses can read changes
//! through `pg_logical_slot_peek_changes`. Gated by the `postgres-it`
//! feature plus `#[ignore]`.
//!
//! ```text
//! cargo test -p connector-management-service --features postgres-it \
//!   --test postgres_cdc_e2e -- --ignored --nocapture
//! ```

#![cfg(feature = "postgres-it")]

use std::time::Duration;

use sqlx::postgres::PgPoolOptions;
use testcontainers::core::{IntoContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{GenericImage, ImageExt};

const PG_USER: &str = "openfoundry";
const PG_PASSWORD: &str = "openfoundry";
const PG_DB: &str = "cdc_e2e";
const PG_PORT: u16 = 5432;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker (Postgres logical replication)"]
async fn postgres_logical_slot_emits_changes_for_connector() {
    let container = GenericImage::new("debezium/postgres", "16-alpine")
        .with_exposed_port(PG_PORT.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_USER", PG_USER)
        .with_env_var("POSTGRES_PASSWORD", PG_PASSWORD)
        .with_env_var("POSTGRES_DB", PG_DB)
        .with_cmd([
            "postgres",
            "-c",
            "wal_level=logical",
            "-c",
            "max_replication_slots=4",
            "-c",
            "max_wal_senders=4",
        ])
        .start()
        .await
        .expect("postgres container must start");

    let host_port = container
        .get_host_port_ipv4(PG_PORT)
        .await
        .expect("mapped postgres port");
    let dsn = format!(
        "postgres://{PG_USER}:{PG_PASSWORD}@127.0.0.1:{host_port}/{PG_DB}"
    );

    // Wait for the server to fully accept queries (image emits the ready
    // banner once during init too).
    let pool = loop {
        match PgPoolOptions::new()
            .max_connections(2)
            .acquire_timeout(Duration::from_secs(2))
            .connect(&dsn)
            .await
        {
            Ok(pool) => break pool,
            Err(_) => tokio::time::sleep(Duration::from_millis(250)).await,
        }
    };

    sqlx::query("CREATE TABLE orders (id SERIAL PRIMARY KEY, total INT NOT NULL)")
        .execute(&pool)
        .await
        .expect("create table");
    sqlx::query("ALTER TABLE orders REPLICA IDENTITY FULL")
        .execute(&pool)
        .await
        .expect("set replica identity");
    sqlx::query("CREATE PUBLICATION orders_pub FOR TABLE orders")
        .execute(&pool)
        .await
        .expect("create publication");
    sqlx::query("SELECT pg_create_logical_replication_slot('orders_slot', 'pgoutput')")
        .execute(&pool)
        .await
        .expect("create logical slot");

    sqlx::query("INSERT INTO orders (total) VALUES (10), (20), (30)")
        .execute(&pool)
        .await
        .expect("insert rows");

    // Peek the logical slot — pgoutput frames are opaque binary, so we
    // simply assert the call succeeds and yields at least one row, which
    // means the WAL stream is producing change events.
    let rows: Vec<(String,)> = sqlx::query_as(
        "SELECT lsn::text FROM pg_logical_slot_peek_binary_changes(
             'orders_slot', NULL, NULL,
             'proto_version', '1',
             'publication_names', 'orders_pub'
         )",
    )
    .fetch_all(&pool)
    .await
    .expect("peek slot");
    assert!(
        !rows.is_empty(),
        "logical slot should contain at least one change event"
    );

    // Cleanup so the test is rerunnable against a reused container.
    sqlx::query("SELECT pg_drop_replication_slot('orders_slot')")
        .execute(&pool)
        .await
        .ok();
}
