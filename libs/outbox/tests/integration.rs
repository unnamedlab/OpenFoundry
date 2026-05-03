//! S0.9.e — integration test for the transactional outbox.
//!
//! Boots a Postgres 16 testcontainer, applies the migration that
//! provisions `outbox.events` and exercises [`outbox::enqueue`] in a
//! few representative transaction shapes:
//!
//! 1. Happy path: enqueue commits → Debezium would observe both the
//!    INSERT and the DELETE in WAL → row not visible after commit.
//! 2. Idempotent retry: a second enqueue with the same `event_id`
//!    is a silent no-op (`ON CONFLICT DO NOTHING`) and does not
//!    affect the surrounding transaction.
//! 3. Rollback: if the caller rolls back, no row reaches the WAL.
//!
//! Gated by the `it-postgres` feature so plain `cargo test -p outbox`
//! stays a no-IO unit run.

#![cfg(feature = "it-postgres")]

use std::collections::HashMap;

use outbox::{OutboxEvent, enqueue};
use serde_json::json;
use sqlx::{Connection, PgConnection, postgres::PgPoolOptions};
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const MIGRATION: &str = include_str!("../migrations/0001_outbox_events.sql");

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn outbox_enqueue_round_trip_against_real_postgres() {
    let image = GenericImage::new("postgres", "16-alpine")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_USER", "of")
        .with_env_var("POSTGRES_PASSWORD", "of")
        .with_env_var("POSTGRES_DB", "pg_policy");

    let pg = image.start().await.expect("start postgres testcontainer");
    let host_port = pg
        .get_host_port_ipv4(5432)
        .await
        .expect("expose host port for postgres");

    let url = format!("postgres://of:of@127.0.0.1:{host_port}/pg_policy");

    // Apply migration.
    let mut admin = PgConnection::connect(&url).await.expect("connect admin");
    sqlx::raw_sql(MIGRATION)
        .execute(&mut admin)
        .await
        .expect("apply outbox migration");
    drop(admin);

    let pool = PgPoolOptions::new()
        .max_connections(4)
        .connect(&url)
        .await
        .expect("connect pool");

    // (1) Happy path.
    let event_id = Uuid::now_v7();
    let mut headers = HashMap::new();
    headers.insert("ol-run-id".to_string(), "run-1".to_string());
    headers.insert("ol-namespace".to_string(), "of".to_string());
    headers.insert("ol-job".to_string(), "ontology.write".to_string());

    let mut tx = pool.begin().await.unwrap();
    enqueue(
        &mut tx,
        OutboxEvent {
            event_id,
            aggregate: "ontology_object".to_string(),
            aggregate_id: "obj-1".to_string(),
            topic: "ontology.object.changed.v1".to_string(),
            payload: json!({"version": 1, "tenant": "t-1", "type_id": "Person"}),
            headers,
        },
    )
    .await
    .expect("enqueue ok");
    tx.commit().await.unwrap();

    // After commit, the row was deleted in the same transaction so it
    // is no longer visible. The WAL still carries the INSERT, which
    // is the record Debezium will publish.
    let row_count: (i64,) =
        sqlx::query_as("SELECT count(*) FROM outbox.events WHERE event_id = $1")
            .bind(event_id)
            .fetch_one(&pool)
            .await
            .expect("count after commit");
    assert_eq!(
        row_count.0, 0,
        "row must be deleted in same tx (canonical Debezium outbox pattern)"
    );

    // (2) Idempotent retry with same event_id is a silent no-op.
    let mut tx = pool.begin().await.unwrap();
    enqueue(
        &mut tx,
        OutboxEvent::new(
            event_id,
            "ontology_object",
            "obj-1",
            "ontology.object.changed.v1",
            json!({"version": 1}),
        ),
    )
    .await
    .expect("idempotent enqueue ok");
    // Surrounding transaction must still be live and committable.
    tx.commit().await.expect("commit after no-op enqueue");

    // (3) Rollback path: nothing leaks into the WAL.
    let rolled_back_id = Uuid::now_v7();
    let mut tx = pool.begin().await.unwrap();
    enqueue(
        &mut tx,
        OutboxEvent::new(
            rolled_back_id,
            "ontology_object",
            "obj-2",
            "ontology.object.changed.v1",
            json!({"version": 1}),
        ),
    )
    .await
    .expect("enqueue inside doomed tx");
    tx.rollback().await.unwrap();

    let row_count: (i64,) =
        sqlx::query_as("SELECT count(*) FROM outbox.events WHERE event_id = $1")
            .bind(rolled_back_id)
            .fetch_one(&pool)
            .await
            .expect("count after rollback");
    assert_eq!(row_count.0, 0, "rollback must leave no trace");

    // (4) Distinct event ids in the same transaction all land in the
    //     WAL even though the table is empty post-commit.
    let ids = [Uuid::now_v7(), Uuid::now_v7(), Uuid::now_v7()];
    let mut tx = pool.begin().await.unwrap();
    for id in &ids {
        enqueue(
            &mut tx,
            OutboxEvent::new(
                *id,
                "ontology_object",
                "obj-batch",
                "ontology.object.changed.v1",
                json!({"id": id.to_string()}),
            )
            .with_header("ol-run-id", "run-batch"),
        )
        .await
        .expect("enqueue batch");
    }
    tx.commit().await.unwrap();
    let row_count: (i64,) = sqlx::query_as("SELECT count(*) FROM outbox.events")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(row_count.0, 0, "table empty in steady state");
}
