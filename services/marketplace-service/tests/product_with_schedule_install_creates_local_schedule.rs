//! Marketplace product schedule manifests survive an install. We seed
//! a product version, attach a schedule manifest with a relative
//! pipeline RID, and post `:install:schedules` with a rid mapping —
//! asserting the response materialises the manifest with the
//! destination RID substituted.
//!
//! Docker-gated.

use std::sync::Arc;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use marketplace_service::{AppState, build_router};
use serde_json::{Value, json};
use sqlx::PgPool;
use testing::{containers::boot_postgres, fixtures};
use tower::ServiceExt;
use uuid::Uuid;

async fn spawn_router() -> (
    testcontainers::ContainerAsync<testcontainers::GenericImage>,
    PgPool,
    axum::Router,
) {
    let (container, pool, _url) = boot_postgres().await;
    let mut migrator = sqlx::migrate!("./migrations");
    migrator.set_ignore_missing(true);
    migrator.run(&pool).await.expect("marketplace migrations");
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
async fn install_pass_substitutes_pipeline_rid_into_target() {
    let (_container, pool, router) = spawn_router().await;

    // Seed a listing + a package version directly (skips the publish
    // endpoint to keep the test focused on the schedules surface).
    let listing_id = Uuid::now_v7();
    let version_id = Uuid::now_v7();
    sqlx::query(
        "INSERT INTO marketplace_listings (id, name, slug, summary, description, publisher,
            category_slug, package_kind, repository_slug, visibility, tags, capabilities,
            install_count, average_rating, created_at, updated_at)
         VALUES ($1, 'demo product', 'demo', '', '', 'demo-publisher',
            'analytics', 'pipeline', 'demo', 'public', '[]', '[]', 0, 0, NOW(), NOW())",
    )
    .bind(listing_id)
    .execute(&pool)
    .await
    .unwrap();
    sqlx::query(
        "INSERT INTO marketplace_package_versions (id, listing_id, version, changelog,
            dependency_mode, dependencies, manifest, published_at)
         VALUES ($1, $2, '1.0.0', '', 'auto', '[]', '{}'::jsonb, NOW())",
    )
    .bind(version_id)
    .bind(listing_id)
    .execute(&pool)
    .await
    .unwrap();

    // Add a schedule manifest with a *relative* pipeline RID that the
    // install pass will rewrite.
    let manifest = json!({
        "product_version_id": version_id,
        "manifest": {
            "name": "daily-build",
            "description": "Daily 09:00 UTC pipeline build",
            "trigger": {"kind": {"time": {"cron": "0 9 * * *", "time_zone": "UTC", "flavor": "UNIX_5"}}},
            "target": {
                "kind": {
                    "pipeline_build": {
                        "pipeline_rid": "ri.foundry.product.pipeline.alpha",
                        "build_branch": "master"
                    }
                }
            },
            "scope_kind": "USER",
            "defaults": { "time_zone": "UTC" }
        }
    });
    let req = Request::builder()
        .method("POST")
        .uri(&format!("/v1/products/{listing_id}/schedules"))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&manifest).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("add manifest");
    assert_eq!(resp.status(), StatusCode::CREATED);

    // Install pass: provide a rid mapping rewriting the relative RID.
    let install_body = json!({
        "product_version_id": version_id,
        "rid_mapping": {
            "pipeline": {
                "ri.foundry.product.pipeline.alpha":
                    "ri.foundry.main.pipeline.alpha-installed"
            }
        }
    });
    let req = Request::builder()
        .method("POST")
        .uri(&format!("/v1/products/{listing_id}/install:schedules"))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&install_body).unwrap()))
        .unwrap();
    let resp = router.clone().oneshot(req).await.expect("materialise");
    assert_eq!(resp.status(), StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();

    let materialised = body["materialised"].as_array().expect("materialised array");
    assert_eq!(materialised.len(), 1);
    assert_eq!(
        materialised[0]["target"]["kind"]["pipeline_build"]["pipeline_rid"],
        json!("ri.foundry.main.pipeline.alpha-installed")
    );
}
