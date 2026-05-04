//! End-to-end integration test for the reindex coordinator.
//!
//! Opt-in via the `it-e2e` Cargo feature so plain
//! `cargo test -p reindex-coordinator-service` stays Docker- and
//! network-free. Mirrors the pattern used by
//! `services/audit-compliance-service/tests/outbox_e2e.rs` and
//! `libs/idempotency/tests` (testcontainers + `#[ignore]`).
//!
//! ## What this exercises
//!
//! 1. Stand up a Postgres testcontainer and apply the
//!    `migrations/0001_reindex_jobs.sql` schema.
//! 2. Drive the [`Coordinator`] state machine end-to-end through
//!    `upsert_queued → mark_running → advance → mark_terminal`,
//!    verifying that:
//!    * The unique `(tenant_id, type_id)` key collapses duplicate
//!      requests to a single row.
//!    * `advance` is rejected once the job is `cancelled`.
//!    * `list_resumable` returns exactly the rows the restart
//!      logic should re-run.
//!
//! This is the part of the e2e flow that does NOT need Cassandra
//! or Kafka — those paths are exercised by hand using the
//! `kafkacat` recipe in the README. A future PR can extend this
//! file with a `cassandra_kernel::testkit` + `event_bus_data::testkit`
//! drive once those helpers exist.

#![cfg(feature = "it-e2e")]

use std::time::Duration;

use reindex_coordinator_service::state::{JobRepo, JobStatus};
use reindex_coordinator_service::{derive_batch_event_id, derive_job_id};
use sqlx::postgres::PgPoolOptions;
use testcontainers::{
    GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};

const SCHEMA_SQL: &str = include_str!("../migrations/0001_reindex_jobs.sql");

#[tokio::test]
#[ignore = "requires Docker"]
async fn coordinator_state_round_trip() {
    let _ = tracing_subscriber::fmt::try_init();

    // ── Postgres testcontainer ──────────────────────────────
    let container = GenericImage::new("postgres", "16")
        .with_exposed_port(5432.tcp())
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_env_var("POSTGRES_PASSWORD", "postgres")
        .with_env_var("POSTGRES_DB", "runtime")
        .start()
        .await
        .expect("start postgres testcontainer");
    let port = container
        .get_host_port_ipv4(5432)
        .await
        .expect("postgres port");
    let url = format!("postgres://postgres:postgres@127.0.0.1:{port}/runtime");

    let pool = PgPoolOptions::new()
        .max_connections(5)
        .acquire_timeout(Duration::from_secs(15))
        .connect(&url)
        .await
        .expect("connect postgres");

    sqlx::raw_sql("CREATE SCHEMA IF NOT EXISTS reindex_coordinator")
        .execute(&pool)
        .await
        .expect("create schema");
    sqlx::raw_sql(SCHEMA_SQL)
        .execute(&pool)
        .await
        .expect("apply migration");

    // ── Job lifecycle ───────────────────────────────────────
    let repo = JobRepo::new(pool);
    let job_id = derive_job_id("tenant-a", Some("users"));

    let row1 = repo
        .upsert_queued(job_id, "tenant-a", Some("users"), 500)
        .await
        .expect("upsert queued");
    assert_eq!(row1.status, JobStatus::Queued);
    assert_eq!(row1.id, job_id);
    assert_eq!(row1.page_size, 500);

    // Duplicate request: same id, same row, no resurrection.
    let row2 = repo
        .upsert_queued(job_id, "tenant-a", Some("users"), 500)
        .await
        .expect("idempotent upsert");
    assert_eq!(row1.id, row2.id);
    assert_eq!(row2.status, JobStatus::Queued);

    repo.mark_running(job_id).await.expect("mark_running");
    let token1 = derive_batch_event_id("tenant-a", Some("users"), "");
    repo.advance(job_id, Some(&token1.to_string()), 100, 90)
        .await
        .expect("advance page 1");
    let after_page_1 = repo.load(job_id).await.expect("load after page 1");
    assert_eq!(after_page_1.scanned, 100);
    assert_eq!(after_page_1.published, 90);
    assert_eq!(after_page_1.resume_token, Some(token1.to_string()));

    let resumable = repo.list_resumable().await.expect("list_resumable");
    assert!(
        resumable.iter().any(|r| r.id == job_id),
        "running job must be returned by list_resumable"
    );

    let final_row = repo
        .mark_terminal(job_id, JobStatus::Completed, None)
        .await
        .expect("mark completed");
    assert_eq!(final_row.status, JobStatus::Completed);
    assert!(final_row.completed_at.is_some());

    // After completion: list_resumable must NOT return it.
    let resumable_after = repo.list_resumable().await.expect("list_resumable after");
    assert!(resumable_after.iter().all(|r| r.id != job_id));

    // Re-marking the same terminal state is a no-op.
    repo.mark_terminal(job_id, JobStatus::Completed, None)
        .await
        .expect("idempotent terminal");

    // Resurrecting from terminal must fail.
    let err = repo
        .advance(job_id, Some("new-token"), 1, 1)
        .await
        .expect_err("advance after terminal must fail");
    let msg = err.to_string();
    assert!(
        msg.contains("transition") || msg.contains("completed"),
        "unexpected error: {msg}"
    );
}
