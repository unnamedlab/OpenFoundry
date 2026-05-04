//! S2.4 — End-to-end smoke test for the outbox → Debezium → Kafka backbone.
//!
//! What this test proves
//! ---------------------
//!
//! That a service-side transaction in `pg-policy` which:
//!
//!   1. inserts a domain row (here: a placeholder `audit_events` row),
//!   2. enqueues an outbox record via [`outbox::enqueue`],
//!   3. commits,
//!
//! results in a Kafka message landing on the topic named by the outbox
//! row (`audit.events.v1`) — i.e. the Debezium Postgres connector and
//! its `EventRouter` SMT correctly turn the WAL `INSERT` into a
//! routed Kafka record. This is the prerequisite the worker refactor
//! in S2.5+ assumes; if this test is green, the backbone is safe to
//! build on.
//!
//! How it is wired
//! ---------------
//!
//! * The test connects to **a real Postgres** (the `pg-policy` DB in
//!   the `lima` dev cluster, or the docker-compose dev stack at
//!   `infra/compose/docker-compose.yml`) over `OUTBOX_E2E_PG_URL`.
//! * It connects to **a real Kafka broker** over
//!   `OUTBOX_E2E_KAFKA_BOOTSTRAP` — same defaults the existing
//!   `libs/outbox/tests/e2e_debezium.sh` script uses, so the same
//!   port-forwards / compose stack works for both.
//! * It bootstraps the `outbox.events` table and the `audit_events`
//!   table itself (both `CREATE TABLE IF NOT EXISTS`) so the test is
//!   safe to run against a fresh dev DB. In a fully-bootstrapped
//!   `pg-policy` cluster this is a no-op.
//!
//! Run with
//! --------
//!
//! ```text
//! cargo test -p audit-compliance-service --features it-debezium \
//!     outbox_e2e -- --ignored --nocapture
//! ```
//!
//! Optional environment overrides:
//!
//! * `OUTBOX_E2E_PG_URL`           — defaults to
//!   `postgres://openfoundry:openfoundry@127.0.0.1:5432/pg_policy`
//! * `OUTBOX_E2E_KAFKA_BOOTSTRAP`  — defaults to `127.0.0.1:9092`
//! * `OUTBOX_E2E_TOPIC`            — defaults to `audit.events.v1`
//! * `OUTBOX_E2E_TIMEOUT_SECS`     — defaults to `60` (cold-start
//!   Debezium replication-slot creation can take 5–30 s).

#![cfg(feature = "it-debezium")]

use std::collections::HashMap;
use std::time::{Duration, Instant};

use chrono::{Duration as ChronoDuration, Utc};
use outbox::{OutboxEvent, enqueue};
use rdkafka::Message;
use rdkafka::config::ClientConfig;
use rdkafka::consumer::{Consumer, StreamConsumer};
use rdkafka::message::Headers;
use serde_json::json;
use sqlx::Connection;
use sqlx::postgres::PgPoolOptions;
use tokio::time::timeout;
use uuid::Uuid;

const DEFAULT_PG_URL: &str = "postgres://openfoundry:openfoundry@127.0.0.1:5432/pg_policy";
const DEFAULT_KAFKA_BOOTSTRAP: &str = "127.0.0.1:9092";
const DEFAULT_TOPIC: &str = "audit.events.v1";
const DEFAULT_TIMEOUT_SECS: u64 = 60;

/// `outbox.events` schema — kept in lockstep with
/// `libs/outbox/migrations/0001_outbox_events.sql` and the
/// `pg-policy` bootstrap ConfigMap. Idempotent.
const OUTBOX_BOOTSTRAP_SQL: &str = r#"
CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     uuid PRIMARY KEY,
    aggregate    text NOT NULL,
    aggregate_id text NOT NULL,
    payload      jsonb NOT NULL,
    headers      jsonb NOT NULL DEFAULT '{}'::jsonb,
    topic        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE outbox.events REPLICA IDENTITY FULL;
"#;

/// Minimum subset of the `audit_events` schema (see
/// `services/audit-compliance-service/migrations/20260422231500_audit_foundation.sql`)
/// needed to exercise the outbox transactional handshake. Idempotent
/// — a fully-migrated DB already has this table and the statement
/// is a no-op.
const AUDIT_BOOTSTRAP_SQL: &str = r#"
CREATE TABLE IF NOT EXISTS audit_events (
    id              UUID PRIMARY KEY,
    sequence        BIGINT NOT NULL UNIQUE,
    previous_hash   TEXT NOT NULL,
    entry_hash      TEXT NOT NULL,
    source_service  TEXT NOT NULL,
    channel         TEXT NOT NULL,
    actor           TEXT NOT NULL,
    action          TEXT NOT NULL,
    resource_type   TEXT NOT NULL,
    resource_id     TEXT NOT NULL,
    status          TEXT NOT NULL,
    severity        TEXT NOT NULL,
    classification  TEXT NOT NULL,
    subject_id      TEXT,
    ip_address      TEXT,
    location        TEXT,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    labels          JSONB NOT NULL DEFAULT '[]'::jsonb,
    retention_until TIMESTAMPTZ NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    ingested_at     TIMESTAMPTZ NOT NULL,
    CONSTRAINT audit_events_append_only CHECK (sequence > 0)
);
"#;

fn env_or(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires a real Postgres + Kafka backbone (lima dev cluster or infra/compose stack)"]
async fn outbox_to_kafka_round_trip_through_debezium() {
    let pg_url = env_or("OUTBOX_E2E_PG_URL", DEFAULT_PG_URL);
    let kafka_bootstrap = env_or("OUTBOX_E2E_KAFKA_BOOTSTRAP", DEFAULT_KAFKA_BOOTSTRAP);
    let topic = env_or("OUTBOX_E2E_TOPIC", DEFAULT_TOPIC);
    let timeout_secs: u64 = env_or("OUTBOX_E2E_TIMEOUT_SECS", &DEFAULT_TIMEOUT_SECS.to_string())
        .parse()
        .expect("OUTBOX_E2E_TIMEOUT_SECS must parse as u64");

    eprintln!("▶ pg_url          = {pg_url}");
    eprintln!("▶ kafka_bootstrap = {kafka_bootstrap}");
    eprintln!("▶ topic           = {topic}");
    eprintln!("▶ timeout_secs    = {timeout_secs}");

    // ── 1. Bootstrap the schemas the test relies on (idempotent). ──
    let mut admin = sqlx::PgConnection::connect(&pg_url)
        .await
        .expect("connect admin");
    sqlx::raw_sql(OUTBOX_BOOTSTRAP_SQL)
        .execute(&mut admin)
        .await
        .expect("bootstrap outbox schema");
    sqlx::raw_sql(AUDIT_BOOTSTRAP_SQL)
        .execute(&mut admin)
        .await
        .expect("bootstrap audit_events table");
    drop(admin);

    let pool = PgPoolOptions::new()
        .max_connections(2)
        .connect(&pg_url)
        .await
        .expect("connect pool");

    // ── 2. Subscribe a Kafka consumer BEFORE producing so we cannot
    //       miss the message. Unique group id keeps each run isolated;
    //       `auto.offset.reset=latest` means we only see records
    //       produced after the join+rebalance below.
    let group_id = format!("audit-compliance-outbox-e2e-{}", Uuid::now_v7());
    let consumer: StreamConsumer = ClientConfig::new()
        .set("bootstrap.servers", &kafka_bootstrap)
        .set("group.id", &group_id)
        .set("enable.auto.commit", "false")
        .set("auto.offset.reset", "latest")
        .set("session.timeout.ms", "10000")
        .create()
        .expect("kafka consumer");
    consumer
        .subscribe(&[topic.as_str()])
        .expect("subscribe to topic");
    // `subscribe` is lazy. A short poll triggers the join+rebalance
    // and gives us partition assignments before the producer side
    // commits — otherwise the very first record may slip through.
    let _ = timeout(Duration::from_secs(15), consumer.recv()).await;

    // ── 3. Application-side transaction: domain row + outbox row,
    //       atomic commit. This is the contract every Foundry-pattern
    //       handler will follow once S2.5 lands.
    let event_id = Uuid::now_v7();
    let aggregate_id = event_id.to_string(); // Kafka message key
    let run_id = format!("run-e2e-{}", Uuid::now_v7());

    // `sequence` must be NOT NULL UNIQUE and > 0 — derive a value
    // that is monotonic enough not to collide with the seed rows in
    // the migration (which use 1, 2, 3) and unique across runs.
    let sequence: i64 = Utc::now().timestamp_micros();
    assert!(sequence > 0, "wall clock must be post-epoch");

    let now = Utc::now();
    let mut tx = pool.begin().await.expect("begin tx");

    sqlx::query(
        r#"
        INSERT INTO audit_events (
            id, sequence, previous_hash, entry_hash, source_service,
            channel, actor, action, resource_type, resource_id,
            status, severity, classification,
            metadata, labels,
            retention_until, occurred_at, ingested_at
        ) VALUES (
            $1, $2, $3, $4, $5,
            $6, $7, $8, $9, $10,
            $11, $12, $13,
            '{}'::jsonb, '[]'::jsonb,
            $14, $15, $15
        )
        "#,
    )
    .bind(event_id)
    .bind(sequence)
    .bind("OUTBOX-E2E-PREV")
    .bind(format!("OUTBOX-E2E-{event_id}"))
    .bind("audit-compliance-service")
    .bind("test")
    .bind("test:outbox-e2e")
    .bind("outbox.smoke.test")
    .bind("audit_event")
    .bind(aggregate_id.as_str())
    .bind("success")
    .bind("low")
    .bind("public")
    .bind(now + ChronoDuration::days(1))
    .bind(now)
    .execute(&mut *tx)
    .await
    .expect("insert audit_events row");

    let mut headers = HashMap::new();
    headers.insert("ol-run-id".to_string(), run_id.clone());
    headers.insert("ol-namespace".to_string(), "of".to_string());
    headers.insert("ol-job".to_string(), "audit.compliance.smoke".to_string());

    enqueue(
        &mut tx,
        OutboxEvent {
            event_id,
            aggregate: "audit_event".to_string(),
            aggregate_id: aggregate_id.clone(),
            topic: topic.clone(),
            payload: json!({
                "event_id": event_id.to_string(),
                "kind": "audit.compliance.smoke",
                "source": "outbox-e2e",
            }),
            headers,
        },
    )
    .await
    .expect("enqueue outbox event");

    tx.commit().await.expect("commit tx");
    eprintln!("✓ committed audit_events + outbox row (event_id={event_id})");

    // ── 4. Poll Kafka until we observe a message whose key matches
    //       our unique aggregate_id. Cap the wait at the configured
    //       timeout — Debezium first-publish on a cold replication
    //       slot can take 5–30 s.
    let deadline = Instant::now() + Duration::from_secs(timeout_secs);
    let received = loop {
        let remaining = deadline
            .checked_duration_since(Instant::now())
            .unwrap_or(Duration::ZERO);
        if remaining.is_zero() {
            panic!(
                "no Kafka message with key={aggregate_id} on topic={topic} \
                 after {timeout_secs}s — check the `outbox-pg-policy` \
                 KafkaConnector is RUNNING (kubectl -n kafka get kafkaconnector) \
                 and that {kafka_bootstrap} reaches the same Kafka the \
                 connector publishes to"
            );
        }
        match timeout(remaining, consumer.recv()).await {
            Err(_) => continue,
            Ok(Err(err)) => panic!("kafka recv error: {err}"),
            Ok(Ok(msg)) => {
                let key = msg.key().and_then(|k| std::str::from_utf8(k).ok());
                if key == Some(aggregate_id.as_str()) {
                    break msg.detach();
                }
                eprintln!(
                    "… skipping unrelated message (key={:?}, partition={}, offset={})",
                    key,
                    msg.partition(),
                    msg.offset()
                );
            }
        }
    };

    // ── 5. Assert payload + EventRouter headers. ──────────────────
    let payload_bytes = received.payload().expect("message payload bytes");
    let payload: serde_json::Value =
        serde_json::from_slice(payload_bytes).expect("payload is JSON");
    assert_eq!(
        payload.get("event_id").and_then(|v| v.as_str()),
        Some(event_id.to_string().as_str()),
        "payload event_id must match the producer-side event_id (got {payload})",
    );

    // EventRouter SMT puts `aggregateType` and `id` (the outbox
    // `event_id`) on the Kafka headers, plus copies the producer's
    // `headers` JSONB onto the record. Verify both ends.
    let mut found_aggregate_type = false;
    let mut found_ol_run_id = false;
    if let Some(hdrs) = received.headers() {
        for header in hdrs.iter() {
            let value = header
                .value
                .and_then(|v| std::str::from_utf8(v).ok())
                .unwrap_or("");
            match header.key {
                "aggregateType" if value == "audit_event" => found_aggregate_type = true,
                "ol-run-id" if value == run_id => found_ol_run_id = true,
                _ => {}
            }
        }
    }
    assert!(
        found_aggregate_type,
        "EventRouter SMT must set aggregateType=audit_event header"
    );
    assert!(
        found_ol_run_id,
        "EventRouter SMT must propagate the ol-run-id header from outbox.events.headers"
    );

    eprintln!("✓ outbox → Debezium → Kafka round-trip verified for event_id={event_id}");
}
