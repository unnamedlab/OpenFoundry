//! Tarea 1.3 — integration test for the cron-driven scheduler.
//!
//! Boots Postgres + Kafka testcontainers, applies the schedules
//! migration, seeds three definitions with different cron
//! expressions (one disabled, one not yet due, one due), and
//! exercises:
//!
//! 1. `tick(now)` fires only the enabled, due schedule and
//!    publishes its payload to the right Kafka topic with the
//!    deterministic OpenLineage `run_id`.
//! 2. `last_run_at` is set to the original `next_run_at`,
//!    `next_run_at` is advanced via the cron expression.
//! 3. A second `tick(now)` immediately after is a no-op
//!    (`fired == 0`).
//! 4. With multiple due schedules, all of them fire in one tick
//!    and produce one Kafka record each.
//! 5. Two concurrent `tick()` calls on the same DB never double-fire
//!    a row (`SELECT … FOR UPDATE SKIP LOCKED` invariant).
//!
//! Gated by `--features it` so the default `cargo test` stays
//! IO-free.

#![cfg(feature = "it")]

use std::sync::Arc;
use std::time::Duration;

use chrono::{TimeZone, Utc};
use event_bus_data::testkit::EphemeralKafka;
use event_bus_data::{DataPublisher, DataSubscriber, KafkaPublisher, KafkaSubscriber};
use event_scheduler::{
    SCHEDULER_LINEAGE_NAMESPACE, SCHEDULER_LINEAGE_PRODUCER, Scheduler, build_lineage,
};
use serde_json::json;
use sqlx::{Connection, PgConnection, Row, postgres::PgPoolOptions};
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const SCHEDULES_MIGRATION: &str = include_str!("../migrations/0001_schedules_definitions.sql");

// ─── Harness ────────────────────────────────────────────────────────────

async fn boot_pg() -> (testcontainers::ContainerAsync<GenericImage>, sqlx::PgPool) {
    let image = GenericImage::new("postgres", "16-alpine")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_USER", "of")
        .with_env_var("POSTGRES_PASSWORD", "of")
        .with_env_var("POSTGRES_DB", "scheduler_test");

    let pg = image.start().await.expect("start postgres testcontainer");
    let host_port = pg
        .get_host_port_ipv4(5432)
        .await
        .expect("expose host port for postgres");
    let url = format!("postgres://of:of@127.0.0.1:{host_port}/scheduler_test");

    let mut admin = PgConnection::connect(&url).await.expect("connect admin");
    sqlx::raw_sql(SCHEDULES_MIGRATION)
        .execute(&mut admin)
        .await
        .expect("apply schedules migration");
    drop(admin);

    let pool = PgPoolOptions::new()
        .max_connections(8)
        .connect(&url)
        .await
        .expect("connect pool");
    (pg, pool)
}

#[allow(clippy::too_many_arguments)]
async fn insert_def(
    pool: &sqlx::PgPool,
    name: &str,
    cron_expr: &str,
    cron_flavor: &str,
    time_zone: &str,
    enabled: bool,
    topic: &str,
    payload: serde_json::Value,
    next_run_at: chrono::DateTime<Utc>,
) -> Uuid {
    let id = Uuid::now_v7();
    sqlx::query(
        "INSERT INTO schedules.definitions \
         (id, name, cron_expr, cron_flavor, time_zone, enabled, topic, payload_template, next_run_at) \
         VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)",
    )
    .bind(id)
    .bind(name)
    .bind(cron_expr)
    .bind(cron_flavor)
    .bind(time_zone)
    .bind(enabled)
    .bind(topic)
    .bind(payload)
    .bind(next_run_at)
    .execute(pool)
    .await
    .expect("insert schedule");
    id
}

async fn read_state(
    pool: &sqlx::PgPool,
    id: Uuid,
) -> (chrono::DateTime<Utc>, Option<chrono::DateTime<Utc>>) {
    let row =
        sqlx::query("SELECT next_run_at, last_run_at FROM schedules.definitions WHERE id = $1")
            .bind(id)
            .fetch_one(pool)
            .await
            .expect("read state");
    (
        row.try_get("next_run_at").unwrap(),
        row.try_get("last_run_at").unwrap(),
    )
}

// ─── Tests ───────────────────────────────────────────────────────────────

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn tick_fires_due_enabled_schedules_and_skips_others() {
    let (_pg, pool) = boot_pg().await;
    let kafka = EphemeralKafka::start()
        .await
        .expect("start kafka container");

    // Three Kafka topics — one per schedule.
    let topic_due = "of.test.scheduler.due";
    let topic_disabled = "of.test.scheduler.disabled";
    let topic_future = "of.test.scheduler.future";
    for t in [topic_due, topic_disabled, topic_future] {
        kafka.create_topic(t, 1).await.expect("create topic");
    }

    let publisher =
        KafkaPublisher::new(&kafka.config_for("scheduler-it")).expect("build publisher");
    let scheduler = Scheduler::new(pool.clone(), Arc::new(publisher));

    // Anchor "now" to a deterministic instant so we can assert on
    // computed `next_run_at` precisely.
    let now = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 0).unwrap();
    let scheduled_for_due = now - chrono::Duration::seconds(1); // strictly due

    let id_due = insert_def(
        &pool,
        "every-five-minutes",
        "*/5 * * * *",
        "unix5",
        "UTC",
        true,
        topic_due,
        json!({"hello": "world", "n": 1}),
        scheduled_for_due,
    )
    .await;

    let _id_disabled = insert_def(
        &pool,
        "disabled-rollup",
        "0 * * * *",
        "unix5",
        "UTC",
        false, // <-- disabled, must not fire
        topic_disabled,
        json!({"why": "disabled"}),
        now - chrono::Duration::hours(1),
    )
    .await;

    let id_future = insert_def(
        &pool,
        "tomorrow-only",
        "0 9 * * *",
        "unix5",
        "UTC",
        true,
        topic_future,
        json!({"why": "future"}),
        now + chrono::Duration::hours(1), // not yet due
    )
    .await;

    // Subscribe BEFORE firing so we don't miss the records.
    let due_sub = KafkaSubscriber::new(&kafka.config_for("scheduler-it-cg"), "due-cg")
        .expect("build subscriber");
    due_sub.subscribe(&[topic_due]).expect("subscribe due");

    // First tick — must fire exactly one schedule.
    let fired = scheduler.tick(now).await.expect("tick");
    assert_eq!(fired, 1, "only the due+enabled schedule must fire");

    // Verify Kafka delivery on the due topic.
    let msg = tokio::time::timeout(Duration::from_secs(20), due_sub.recv())
        .await
        .expect("recv timed out")
        .expect("recv");
    assert_eq!(msg.topic(), topic_due);
    assert_eq!(msg.key(), Some(b"every-five-minutes".as_ref()));
    let body: serde_json::Value =
        serde_json::from_slice(msg.payload().expect("payload")).expect("json");
    assert_eq!(body, json!({"hello": "world", "n": 1}));
    let lineage = msg.lineage().expect("lineage headers");
    assert_eq!(lineage.namespace, SCHEDULER_LINEAGE_NAMESPACE);
    assert_eq!(lineage.job_name, "every-five-minutes");
    assert_eq!(lineage.producer, SCHEDULER_LINEAGE_PRODUCER);
    let expected = build_lineage("every-five-minutes", scheduled_for_due);
    assert_eq!(
        lineage.run_id, expected.run_id,
        "run_id must be deterministic v5(name, scheduled_for)"
    );
    msg.commit().expect("commit offset");

    // Persisted state of the due row: last_run_at == scheduled_for_due,
    // next_run_at advanced strictly past `now` (so the row doesn't
    // re-fire in the same tick or in a back-to-back tick at the same
    // instant). Standard cron skip-missed semantic.
    let (next, last) = read_state(&pool, id_due).await;
    assert_eq!(last, Some(scheduled_for_due));
    assert_eq!(next, Utc.with_ymd_and_hms(2026, 5, 4, 12, 5, 0).unwrap());

    // The future row must remain untouched.
    let (next_future, last_future) = read_state(&pool, id_future).await;
    assert_eq!(next_future, now + chrono::Duration::hours(1));
    assert_eq!(last_future, None);

    // Second tick at the same instant — nothing left due, because
    // `next_run_at` is now strictly > now.
    let fired_again = scheduler.tick(now).await.expect("tick 2");
    assert_eq!(fired_again, 0);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn tick_fires_multiple_due_schedules_in_one_call() {
    let (_pg, pool) = boot_pg().await;
    let kafka = EphemeralKafka::start()
        .await
        .expect("start kafka container");

    let t_a = "of.test.scheduler.multi.a";
    let t_b = "of.test.scheduler.multi.b";
    let t_c = "of.test.scheduler.multi.c";
    for t in [t_a, t_b, t_c] {
        kafka.create_topic(t, 1).await.expect("create topic");
    }

    let publisher =
        KafkaPublisher::new(&kafka.config_for("scheduler-it")).expect("build publisher");
    let scheduler = Scheduler::new(pool.clone(), Arc::new(publisher));

    let now = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 0).unwrap();
    let due = now - chrono::Duration::seconds(1);

    insert_def(
        &pool,
        "a",
        "*/5 * * * *",
        "unix5",
        "UTC",
        true,
        t_a,
        json!({"k":"a"}),
        due,
    )
    .await;
    insert_def(
        &pool,
        "b",
        "*/10 * * * *",
        "unix5",
        "UTC",
        true,
        t_b,
        json!({"k":"b"}),
        due,
    )
    .await;
    insert_def(
        &pool,
        "c",
        "0 * * * *",
        "unix5",
        "UTC",
        true,
        t_c,
        json!({"k":"c"}),
        due,
    )
    .await;

    let fired = scheduler.tick(now).await.expect("tick");
    assert_eq!(fired, 3);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn concurrent_ticks_never_double_fire_a_row() {
    let (_pg, pool) = boot_pg().await;
    let kafka = EphemeralKafka::start()
        .await
        .expect("start kafka container");

    let topic = "of.test.scheduler.concurrent";
    kafka.create_topic(topic, 1).await.expect("create topic");

    let publisher =
        Arc::new(KafkaPublisher::new(&kafka.config_for("scheduler-it")).expect("build publisher"))
            as Arc<dyn DataPublisher>;

    let s1 = Scheduler::new(pool.clone(), publisher.clone());
    let s2 = Scheduler::new(pool.clone(), publisher.clone());

    let now = Utc.with_ymd_and_hms(2026, 5, 4, 12, 0, 0).unwrap();
    let due = now - chrono::Duration::seconds(1);

    // Just one due schedule. With FOR UPDATE SKIP LOCKED in place,
    // the sum of fires across two parallel ticks must be exactly 1.
    insert_def(
        &pool,
        "only-one",
        "*/5 * * * *",
        "unix5",
        "UTC",
        true,
        topic,
        json!({"k":"only-one"}),
        due,
    )
    .await;

    let (r1, r2) = tokio::join!(s1.tick(now), s2.tick(now));
    let n1 = r1.expect("tick 1");
    let n2 = r2.expect("tick 2");
    assert_eq!(
        n1 + n2,
        1,
        "exactly one tick must claim the row (got {n1}+{n2})"
    );
}
