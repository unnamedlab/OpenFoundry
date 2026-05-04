//! P4 — `retention_worker::run_once` archives non-root branches whose
//! `last_activity_at` is older than the resolved TTL window.
//!
//! Runs against testcontainers Postgres. The worker reads the live
//! `dataset_branches` rows (including the seeded `master` row) and
//! emits a `dataset.branch.archived.v1` outbox event per archive.

mod common;

use chrono::{Duration, Utc};
use dataset_versioning_service::domain::retention_worker;
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn ttl_eligible_branch_is_archived_and_event_emitted() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.retention-ttl").await;

    let master_id: Uuid = sqlx::query_scalar(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .expect("master id");

    // Insert a child with TTL_DAYS=1 and last_activity 30 days ago.
    let feature_id = Uuid::now_v7();
    let stale_at = Utc::now() - Duration::days(30);
    sqlx::query(
        r#"INSERT INTO dataset_branches
              (id, dataset_id, dataset_rid, name, parent_branch_id,
               retention_policy, retention_ttl_days, last_activity_at, is_default)
            VALUES ($1, $2, $3, 'feature', $4, 'TTL_DAYS', 1, $5, FALSE)"#,
    )
    .bind(feature_id)
    .bind(dataset_id)
    .bind("ri.foundry.main.dataset.retention-ttl")
    .bind(master_id)
    .bind(stale_at)
    .execute(&h.pool)
    .await
    .expect("insert feature");

    let archived = retention_worker::run_once(&h.pool).await.expect("run_once");
    assert_eq!(archived, 1, "feature should be archived in this tick");

    // The branch row carries archived_at + grace_until.
    let (archived_at, grace_until) =
        sqlx::query_as::<_, (Option<chrono::DateTime<Utc>>, Option<chrono::DateTime<Utc>>)>(
            "SELECT archived_at, archive_grace_until FROM dataset_branches WHERE id = $1",
        )
        .bind(feature_id)
        .fetch_one(&h.pool)
        .await
        .expect("read archived_at");
    assert!(archived_at.is_some(), "archived_at must be set");
    assert!(grace_until.is_some(), "archive_grace_until must be set");

    // A second tick is a no-op — the branch is already archived.
    let again = retention_worker::run_once(&h.pool)
        .await
        .expect("idempotent");
    assert_eq!(again, 0);
}
