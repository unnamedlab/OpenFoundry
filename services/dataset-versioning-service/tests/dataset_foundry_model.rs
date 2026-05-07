//! Foundry-parity dataset read model.
//!
//! This integration test is Docker-gated like the rest of the catalog
//! HTTP tests. It verifies that `/v1/datasets/{rid}/model` composes the
//! catalog-owned metadata with schema, files, branches, current view,
//! health, markings, inherited permissions, and lineage links.

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use http_body_util::BodyExt;
use serde_json::{Value, json};
use tower::ServiceExt;
use uuid::Uuid;

async fn json_request(
    h: &common::Harness,
    method: &str,
    uri: String,
    token: &str,
    body: Value,
) -> (StatusCode, Value) {
    let req = Request::builder()
        .method(method)
        .uri(uri)
        .header("authorization", format!("Bearer {token}"))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let status = resp.status();
    let bytes = resp.into_body().collect().await.unwrap().to_bytes();
    let value = if bytes.is_empty() {
        json!(null)
    } else {
        serde_json::from_slice(&bytes).unwrap()
    };
    (status, value)
}

async fn get_json(h: &common::Harness, uri: String, token: &str) -> (StatusCode, Value) {
    let req = Request::builder()
        .method("GET")
        .uri(uri)
        .header("authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    let status = resp.status();
    let bytes = to_bytes(resp.into_body(), 256 * 1024).await.unwrap();
    let value = if bytes.is_empty() {
        json!(null)
    } else {
        serde_json::from_slice(&bytes).unwrap()
    };
    (status, value)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn dataset_model_composes_foundry_dataset_facets() {
    let h = common::spawn().await;
    let admin_token = testing::fixtures::dev_token(
        &h.jwt_config,
        vec![
            "dataset.read".into(),
            "dataset.write".into(),
            "dataset.admin".into(),
        ],
    );

    let (status, created) = json_request(
        &h,
        "POST",
        "/v1/datasets".to_string(),
        &h.token,
        json!({
            "name": "orders",
            "description": "Curated orders",
            "format": "parquet",
            "tags": ["finance", "curated"],
            "metadata": { "project": "supply-chain" },
            "health_status": "healthy"
        }),
    )
    .await;
    assert_eq!(status, StatusCode::CREATED);
    let dataset_id = Uuid::parse_str(created["id"].as_str().unwrap()).unwrap();

    let view_id = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_views
           (id, dataset_id, name, description, sql_text, source_branch, materialized, format, current_version, storage_path, row_count, schema_fields)
           VALUES ($1, $2, 'current', 'Current production view', 'SELECT * FROM dataset', 'main', true, 'parquet', 1, 'bronze/orders/current', 10, $3)"#,
    )
    .bind(view_id)
    .bind(dataset_id)
    .bind(json!([{ "name": "order_id", "type": "STRING", "nullable": false }]))
    .execute(&h.pool)
    .await
    .unwrap();

    sqlx::query(
        r#"INSERT INTO dataset_branches (id, dataset_id, name, version, base_version, description, is_default)
           VALUES ($1, $2, 'main', 1, 1, 'default branch', true)"#,
    )
    .bind(Uuid::now_v7())
    .bind(dataset_id)
    .execute(&h.pool)
    .await
    .unwrap();

    sqlx::query(
        r#"INSERT INTO dataset_versions (id, dataset_id, version, message, size_bytes, row_count, storage_path)
           VALUES ($1, $2, 1, 'initial snapshot', 128, 10, 'bronze/orders/v1')"#,
    )
    .bind(Uuid::now_v7())
    .bind(dataset_id)
    .execute(&h.pool)
    .await
    .unwrap();

    sqlx::query(
        r#"INSERT INTO dataset_profiles (id, dataset_id, profile, score)
           VALUES ($1, $2, $3, 97.5)"#,
    )
    .bind(Uuid::now_v7())
    .bind(dataset_id)
    .bind(json!({ "row_count": 10, "column_count": 1 }))
    .execute(&h.pool)
    .await
    .unwrap();

    let (status, _) = json_request(
        &h,
        "PATCH",
        format!("/v1/datasets/{dataset_id}/metadata"),
        &h.token,
        json!({
            "current_view_id": view_id,
            "schema": [
                { "name": "order_id", "type": "STRING", "nullable": false },
                { "name": "amount", "type": "DOUBLE", "nullable": true }
            ],
            "metadata": { "project": "supply-chain", "tier": "gold" },
            "health_status": "healthy"
        }),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    let marking_id = Uuid::now_v7();
    let (status, _) = json_request(
        &h,
        "PUT",
        format!("/v1/datasets/{dataset_id}/markings"),
        &admin_token,
        json!({ "markings": [marking_id] }),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    let (status, _) = json_request(
        &h,
        "PUT",
        format!("/v1/datasets/{dataset_id}/permissions"),
        &admin_token,
        json!({
            "permissions": [
                {
                    "principal_kind": "group",
                    "principal_id": "finance-analysts",
                    "role": "viewer",
                    "actions": ["read"],
                    "source": "inherited_from_project",
                    "inherited_from": "project:supply-chain"
                }
            ]
        }),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    let (status, _) = json_request(
        &h,
        "PUT",
        format!("/v1/datasets/{dataset_id}/lineage-links"),
        &h.token,
        json!({
            "links": [
                {
                    "direction": "upstream",
                    "target_rid": "ri.foundry.main.dataset.raw-orders",
                    "relation_kind": "derives_from",
                    "pipeline_id": "pipeline.orders"
                }
            ]
        }),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    let (status, _) = json_request(
        &h,
        "PUT",
        format!("/v1/datasets/{dataset_id}/files/index"),
        &h.token,
        json!({
            "files": [
                {
                    "path": "current/orders.parquet",
                    "storage_path": "bronze/orders/current/orders.parquet",
                    "entry_type": "file",
                    "size_bytes": 128,
                    "content_type": "application/vnd.apache.parquet",
                    "metadata": { "version": 1 }
                }
            ]
        }),
    )
    .await;
    assert_eq!(status, StatusCode::OK);

    let (status, model) = get_json(&h, format!("/v1/datasets/{dataset_id}/model"), &h.token).await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(model["description"], "Curated orders");
    assert_eq!(model["format"], "parquet");
    assert_eq!(model["metadata"]["tier"], "gold");
    assert_eq!(model["schema"]["fields"].as_array().unwrap().len(), 2);
    assert_eq!(model["files"].as_array().unwrap().len(), 1);
    assert_eq!(model["branches"].as_array().unwrap().len(), 1);
    assert_eq!(model["versions"].as_array().unwrap().len(), 1);
    assert_eq!(model["current_view"]["id"], view_id.to_string());
    assert_eq!(model["health"]["status"], "healthy");
    assert_eq!(model["health"]["quality_score"], 97.5);
    assert_eq!(model["markings"][0]["id"], marking_id.to_string());
    assert_eq!(model["permissions"][0]["source"], "inherited_from_project");
    assert_eq!(
        model["lineage_links"][0]["target_rid"],
        "ri.foundry.main.dataset.raw-orders"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn dataset_model_validates_invalid_permission_inheritance() {
    let h = common::spawn().await;
    let admin_token = testing::fixtures::dev_token(
        &h.jwt_config,
        vec![
            "dataset.read".into(),
            "dataset.write".into(),
            "dataset.admin".into(),
        ],
    );
    let dataset_id = testing::fixtures::seed_dataset(
        &h.pool,
        "ri.foundry.main.dataset.invalid-permission",
        "invalid-permission",
        "parquet",
    )
    .await;

    let (status, body) = json_request(
        &h,
        "PUT",
        format!("/v1/datasets/{dataset_id}/permissions"),
        &admin_token,
        json!({
            "permissions": [
                {
                    "principal_kind": "group",
                    "principal_id": "finance-analysts",
                    "role": "viewer",
                    "actions": ["read"],
                    "source": "inherited_from_project"
                }
            ]
        }),
    )
    .await;

    assert_eq!(status, StatusCode::BAD_REQUEST);
    assert!(body["error"].as_str().unwrap().contains("inherited_from"));
}
