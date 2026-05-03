//! P2 — preview a TEXT (CSV) view with a non-default `|` delimiter.
//!
//! Foundry doc § "CSV parsing": the persisted `customMetadata.csv`
//! options drive how the reader splits records. This test seeds a
//! pipe-delimited file, persists a TEXT schema with `delimiter = "|"`,
//! and asserts the preview parses three columns (not one).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use bytes::Bytes;
use serde_json::Value;
use tower::ServiceExt;
use uuid::Uuid;

const FIXTURE: &str = "id|name|amount\n1|alice|10.5\n2|bob|22.0\n";

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn preview_text_csv_uses_custom_delimiter() {
    let h = common::spawn().await;
    let dataset_id = common::seed_dataset_with_master(
        &h.pool,
        "ri.foundry.main.dataset.preview-csv",
    )
    .await;

    // Seed: write the fixture through the LocalStorage backend, create a
    // committed transaction, materialise a view, and persist a TEXT
    // schema with delimiter='|'.
    let physical_path = "datasets/preview-csv/part-0.csv";
    h.storage_backend()
        .put(physical_path, Bytes::from_static(FIXTURE.as_bytes()))
        .await
        .expect("put csv fixture");

    let view_id = common::seed_committed_view_with_file(
        &h.pool,
        dataset_id,
        physical_path,
        FIXTURE.len() as i64,
    )
    .await;

    let schema_payload = serde_json::json!({
        "fields": [
            { "name": "id",     "type": "LONG",   "nullable": false },
            { "name": "name",   "type": "STRING", "nullable": true },
            { "name": "amount", "type": "DOUBLE", "nullable": true }
        ],
        "file_format": "TEXT",
        "custom_metadata": {
            "csv": {
                "delimiter": "|",
                "quote": "\"",
                "escape": "\\",
                "header": true,
                "nullValue": "",
                "charset": "UTF-8"
            }
        }
    });
    common::upsert_view_schema(&h.pool, view_id, &schema_payload, "TEXT").await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/views/{view_id}/data?limit=10"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::OK, "preview ok");
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).expect("preview json");

    assert_eq!(body["file_format"], "TEXT");
    assert_eq!(body["text_sub_format"], "csv");

    let columns = body["columns"].as_array().expect("columns");
    let names: Vec<&str> = columns.iter().map(|c| c["name"].as_str().unwrap()).collect();
    assert_eq!(
        names,
        vec!["id", "name", "amount"],
        "pipe delimiter should yield 3 columns: {body}"
    );

    let rows = body["rows"].as_array().expect("rows");
    assert_eq!(rows.len(), 2, "two data rows: {body}");
    assert_eq!(rows[0]["name"], "alice");
    assert_eq!(rows[1]["name"], "bob");

    let csv_opts = &body["csv_options"];
    assert_eq!(csv_opts["delimiter"], "|", "csv tooltip surfaces delimiter");
    assert_eq!(csv_opts["header"], true);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn preview_csv_delimiter_override_via_query_param() {
    // Even when the persisted schema is wrong (default ','), the user
    // can pass `?csv_delimiter=|` to recover. This exercises the
    // [`PreviewOverrides`] query-param plumbing in the handler.
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.preview-csv-ovr").await;
    let physical_path = "datasets/preview-csv-ovr/part-0.csv";
    h.storage_backend()
        .put(physical_path, Bytes::from_static(FIXTURE.as_bytes()))
        .await
        .expect("put csv");
    let view_id = common::seed_committed_view_with_file(
        &h.pool,
        dataset_id,
        physical_path,
        FIXTURE.len() as i64,
    )
    .await;

    // Empty schema → reader infers types over the (mis-delimited) buffer.
    let schema_payload =
        serde_json::json!({ "fields": [], "file_format": "TEXT", "custom_metadata": null });
    common::upsert_view_schema(&h.pool, view_id, &schema_payload, "TEXT").await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/views/{view_id}/data?limit=10&csv_delimiter=%7C"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert!(resp.status().is_success());
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).unwrap();
    let columns = body["columns"].as_array().unwrap();
    assert_eq!(columns.len(), 3, "delimiter override yields 3 columns: {body}");
    assert_eq!(body["schema_inferred"], true);
}

// ─── Dummy reference so `cargo` keeps the file in the test target even
//     when both tests above are filtered by `--ignore`. ─────────────────
fn _bootstrap() -> Uuid {
    Uuid::nil()
}
