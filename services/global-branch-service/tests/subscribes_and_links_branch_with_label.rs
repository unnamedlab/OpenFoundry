//! P4 — the Postgres subscriber port auto-creates a global-branch
//! link when a `dataset.branch.created.v1` payload carries the
//! `global_branch=<rid>` label.
//!
//! This test drives the port directly with a synthetic JSON payload
//! to avoid pulling a Kafka cluster into the harness. The Kafka glue
//! lives in the binary; the unit-of-behaviour we care about
//! (label → link row) is at the port level.

mod common;

use global_branch_service::global::{
    handlers,
    model::CreateGlobalBranchRequest,
    store,
    subscriber::{PostgresSubscriber, SubscriberPort},
};
use serde_json::json;
use sqlx::Row;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn dataset_branch_created_with_label_creates_link() {
    let h = common::spawn().await;

    // 1. Seed a global branch.
    let global = store::create_branch(
        &h.pool,
        &CreateGlobalBranchRequest {
            name: "release-2026-Q3".into(),
            description: None,
            parent_global_branch: None,
        },
        "tester",
    )
    .await
    .expect("create global");

    // 2. Synthetic dataset.branch.created.v1 envelope with the label.
    let event = json!({
        "event_type": "dataset.branch.created.v1",
        "branch_rid": "ri.foundry.main.branch.42",
        "dataset_rid": "ri.foundry.main.dataset.foo",
        "labels": { "global_branch": global.rid },
    });

    let port = PostgresSubscriber { pool: h.pool.clone() };
    port.handle(&event).await.expect("handle");

    // 3. Link row landed.
    let link_count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM global_branch_resource_links WHERE global_branch_id = $1",
    )
    .bind(global.id)
    .fetch_one(&h.pool)
    .await
    .unwrap();
    assert_eq!(link_count, 1);

    let row = sqlx::query(
        "SELECT resource_type, resource_rid, branch_rid, status FROM global_branch_resource_links WHERE global_branch_id = $1",
    )
    .bind(global.id)
    .fetch_one(&h.pool)
    .await
    .unwrap();
    assert_eq!(row.get::<String, _>("resource_type"), "dataset");
    assert_eq!(row.get::<String, _>("resource_rid"), "ri.foundry.main.dataset.foo");
    assert_eq!(row.get::<String, _>("branch_rid"), "ri.foundry.main.branch.42");
    assert_eq!(row.get::<String, _>("status"), "in_sync");

    // 4. An archived event flips the link status.
    let archived = json!({
        "event_type": "dataset.branch.archived.v1",
        "branch_rid": "ri.foundry.main.branch.42",
        "dataset_rid": "ri.foundry.main.dataset.foo",
    });
    port.handle(&archived).await.expect("archived");
    let status: String = sqlx::query_scalar(
        "SELECT status FROM global_branch_resource_links WHERE global_branch_id = $1",
    )
    .bind(global.id)
    .fetch_one(&h.pool)
    .await
    .unwrap();
    assert_eq!(status, "archived");

    // Compile-time guards so the public re-exports stay reachable.
    let _ = handlers::PROMOTE_TOPIC;
    fn _assert_subscriber_port_object_safe(_p: &dyn SubscriberPort) {}
}
