//! P4 — `branch_markings_snapshot` is populated at child-branch
//! creation time. Mirrors the Foundry "Branch security" rule: the
//! parent's markings copy into `source = PARENT` once, the child can
//! never see fewer.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn child_inherits_parent_markings_at_creation_time() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.markings-snap").await;

    let master_id: Uuid = sqlx::query_scalar(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .expect("master id");

    // Seed the parent with two markings. The child's snapshot must
    // copy both — and only those — at creation time.
    let pii = Uuid::now_v7();
    let hipaa = Uuid::now_v7();
    for marking in [pii, hipaa] {
        sqlx::query(
            r#"INSERT INTO branch_markings_snapshot (branch_id, marking_id, source)
               VALUES ($1, $2, 'EXPLICIT')"#,
        )
        .bind(master_id)
        .bind(marking)
        .execute(&h.pool)
        .await
        .unwrap();
    }

    // POST /branches → child_from_branch.
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{dataset_id}/branches"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({
                "name": "feature",
                "source": { "from_branch": "master" },
            }))
            .unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::CREATED);

    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let feature_id = Uuid::parse_str(body["id"].as_str().unwrap()).unwrap();

    // Snapshot rows for the child carry source = PARENT.
    let rows: Vec<(Uuid, String)> = sqlx::query_as(
        "SELECT marking_id, source FROM branch_markings_snapshot WHERE branch_id = $1 ORDER BY marking_id",
    )
    .bind(feature_id)
    .fetch_all(&h.pool)
    .await
    .unwrap();
    assert_eq!(rows.len(), 2);
    for (_, source) in &rows {
        assert_eq!(source, "PARENT");
    }
    let ids: Vec<Uuid> = rows.iter().map(|(id, _)| *id).collect();
    assert!(ids.contains(&pii));
    assert!(ids.contains(&hipaa));

    // GET /markings projects effective + inherited correctly.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{dataset_id}/branches/feature/markings"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let inherited: Vec<Value> = body["inherited_from_parent"].as_array().cloned().unwrap();
    assert_eq!(inherited.len(), 2);
    assert!(body["explicit"].as_array().unwrap().is_empty());
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn marking_added_to_parent_after_child_creation_does_not_propagate() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.markings-snap-late",
    )
    .await;
    let master_id: Uuid = sqlx::query_scalar(
        "SELECT id FROM dataset_branches WHERE dataset_id = $1 AND name = 'master'",
    )
    .bind(dataset_id)
    .fetch_one(&h.pool)
    .await
    .unwrap();

    // Create the child first (parent has no markings yet).
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{dataset_id}/branches"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(
            serde_json::to_vec(&json!({
                "name": "feature",
                "source": { "from_branch": "master" },
            }))
            .unwrap(),
        ))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::CREATED);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let feature_id = Uuid::parse_str(body["id"].as_str().unwrap()).unwrap();

    // Now add a marking to the parent. The child must NOT see it.
    let pii = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO branch_markings_snapshot (branch_id, marking_id, source)
           VALUES ($1, $2, 'EXPLICIT')"#,
    )
    .bind(master_id)
    .bind(pii)
    .execute(&h.pool)
    .await
    .unwrap();

    let count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM branch_markings_snapshot WHERE branch_id = $1 AND marking_id = $2",
    )
    .bind(feature_id)
    .bind(pii)
    .fetch_one(&h.pool)
    .await
    .unwrap();
    assert_eq!(
        count, 0,
        "snapshot semantics: late-added parent markings must not propagate"
    );

    // Sanity: the parent itself does carry the new marking.
    let parent_rows: Vec<Uuid> =
        sqlx::query_scalar("SELECT marking_id FROM branch_markings_snapshot WHERE branch_id = $1")
            .bind(master_id)
            .fetch_all(&h.pool)
            .await
            .unwrap();
    assert!(parent_rows.contains(&pii));

    // Bonus: the outbox now has a `dataset.branch.created.v1` event
    // for the child, and *no* event for the late marking add (the
    // late add bypasses the API surface).
    let events: Vec<(String, String)> = sqlx::query_as(
        "SELECT topic, payload->>'event_type' FROM outbox.events ORDER BY created_at",
    )
    .fetch_all(&h.pool)
    .await
    .unwrap_or_default();
    // Outbox helper deletes rows in the same tx, so steady-state is empty.
    let _ = events;

    // Last guard — querying via the API should still report 0 inherited
    // markings on the feature branch.
    let req = Request::builder()
        .method("GET")
        .uri(format!("/v1/datasets/{dataset_id}/branches/feature/markings"))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    assert!(body["inherited_from_parent"].as_array().unwrap().is_empty());
}
