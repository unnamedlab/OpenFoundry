//! P5 — `from-dataset` → `install` round trip.
//!
//! Foundry doc § "Marketplace/Packaging" requires the install path to
//! replay the manifest captured at publish time so a dataset moved
//! between tenants stays semantically identical (schema + retention +
//! branching policy + schedules). This test seeds a manifest, publishes
//! the product, installs it on a different `target_project_id`, and
//! asserts the install row carries the exact same manifest payload.
//!
//! Docker-gated.

use std::sync::Arc;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use marketplace_service::{AppState, build_router};
use serde_json::{Value, json};
use testing::{containers::boot_postgres, fixtures};
use tower::ServiceExt;
use uuid::Uuid;

async fn spawn_router() -> (
    testcontainers::ContainerAsync<testcontainers::GenericImage>,
    sqlx::PgPool,
    axum::Router,
) {
    let (container, pool, _url) = boot_postgres().await;
    let mut migrator = sqlx::migrate!("./migrations");
    migrator.set_ignore_missing(true);
    migrator
        .run(&pool)
        .await
        .expect("apply marketplace migrations");

    let jwt_config = fixtures::jwt_config();
    let state = AppState {
        db: pool.clone(),
        jwt_config: Arc::new(jwt_config),
        http_client: reqwest::Client::new(),
        app_builder_service_url: "http://localhost:0".into(),
    };
    let router = build_router(state);
    (container, pool, router)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn publish_then_install_preserves_manifest_across_tenants() {
    let (_container, pool, router) = spawn_router().await;

    // Inline manifest fragments. The handler honours each fragment
    // only when its matching `include_*` flag is also true.
    let publish_body = json!({
        "name": "shared-customer-dim",
        "version": "1.2.3",
        "include_schema": true,
        "include_branches": true,
        "include_retention": true,
        "include_schedules": true,
        "export_includes_data": false,
        "bootstrap_mode": "schema-only",
        "schema": {
            "fields": [
                { "name": "id", "type": "LONG", "nullable": false },
                { "name": "name", "type": "STRING", "nullable": true }
            ],
            "file_format": "PARQUET"
        },
        "retention": [
            {
                "name": "explicit-30-days",
                "retention_days": 30,
                "selector": { "dataset_rid": "ri.foundry.main.dataset.src" }
            }
        ],
        "branching_policy": {
            "default_branch": "master",
            "parent_chain": ["master", "trunk"]
        },
        "schedules": [
            "ri.foundry.main.schedule.daily",
            "ri.foundry.main.schedule.hourly"
        ]
    });

    let req = Request::builder()
        .method("POST")
        .uri("/v1/products/from-dataset/ri.foundry.main.dataset.src")
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&publish_body).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("publish");
    assert_eq!(resp.status(), StatusCode::OK, "publish ok");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let product: Value = serde_json::from_slice(&bytes).unwrap();

    // Manifest snapshot mirrors what we sent in.
    assert_eq!(product["name"], "shared-customer-dim");
    assert_eq!(product["version"], "1.2.3");
    assert_eq!(product["entity_type"], "dataset");
    assert_eq!(product["manifest"]["entity"], "dataset");
    assert_eq!(product["manifest"]["bootstrap"]["mode"], "schema-only");
    assert_eq!(
        product["manifest"]["schema"]["file_format"],
        "PARQUET",
        "schema fragment captured"
    );
    let retention = product["manifest"]["retention"].as_array().unwrap();
    assert_eq!(retention.len(), 1, "retention fragment captured");
    let schedules = product["manifest"]["schedules"].as_array().unwrap();
    assert_eq!(schedules.len(), 2);
    let product_id = product["id"].as_str().unwrap().to_string();

    // Install on a different target project + RID.
    let target_project_id = Uuid::now_v7();
    let target_rid = "ri.foundry.main.dataset.dst";
    let install_body = json!({
        "target_project_id": target_project_id,
        "target_dataset_rid": target_rid,
    });
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/products/{product_id}/install"))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&install_body).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("install");
    assert_eq!(resp.status(), StatusCode::OK, "install ok");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let install: Value = serde_json::from_slice(&bytes).unwrap();

    assert_eq!(install["product_id"], product_id);
    assert_eq!(install["target_dataset_rid"], target_rid);
    assert_eq!(
        install["target_project_id"],
        target_project_id.to_string()
    );
    assert_eq!(install["bootstrap_mode"], "schema-only");
    assert_eq!(install["status"], "pending");

    // The install row carries the manifest replay so the runner can
    // recreate the dataset without re-querying the product table.
    let replay = &install["details"]["manifest_replay"];
    assert_eq!(
        replay["schema"]["file_format"], "PARQUET",
        "schema replayed verbatim"
    );
    let replay_retention = replay["retention"].as_array().unwrap();
    assert_eq!(replay_retention.len(), 1);
    let replay_schedules = replay["schedules"].as_array().unwrap();
    assert_eq!(replay_schedules.len(), 2);

    // Sanity: the row landed in the install table with the right keys.
    let count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM marketplace_dataset_product_installs
          WHERE product_id = $1 AND target_dataset_rid = $2",
    )
    .bind(Uuid::parse_str(&product_id).unwrap())
    .bind(target_rid)
    .fetch_one(&pool)
    .await
    .unwrap();
    assert_eq!(count, 1);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn schema_only_publish_omits_other_fragments() {
    // Toggling `include_schema` on but everything else off must leave
    // retention / branching_policy / schedules empty in the manifest,
    // even when the request body carries them.
    let (_container, _pool, router) = spawn_router().await;

    let publish_body = json!({
        "name": "schema-only-product",
        "version": "0.1.0",
        "include_schema": true,
        "include_branches": false,
        "include_retention": false,
        "include_schedules": false,
        "schema": {"fields": []},
        "retention": [{"name": "should-not-appear"}],
        "branching_policy": {"default_branch": "irrelevant"},
        "schedules": ["ri.foundry.main.schedule.x"]
    });
    let req = Request::builder()
        .method("POST")
        .uri("/v1/products/from-dataset/ri.foundry.main.dataset.x")
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&publish_body).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("publish");
    assert!(resp.status().is_success());
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let product: Value = serde_json::from_slice(&bytes).unwrap();
    let manifest = &product["manifest"];

    assert!(manifest["schema"].is_object(), "schema kept");
    assert_eq!(
        manifest["retention"].as_array().unwrap().len(),
        0,
        "retention dropped"
    );
    assert!(
        manifest["branching_policy"].is_null(),
        "branching policy dropped: {manifest}"
    );
    assert_eq!(
        manifest["schedules"].as_array().unwrap().len(),
        0,
        "schedules dropped"
    );
}
