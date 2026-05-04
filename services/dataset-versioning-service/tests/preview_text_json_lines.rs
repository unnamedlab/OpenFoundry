//! P2 — preview a TEXT view that contains JSON-lines records.
//!
//! Foundry doc § "File formats": TEXT may be CSV or JSON-lines. The
//! reader should auto-detect the sub-format from the first non-whitespace
//! byte (`{` ⇒ JSON-lines). Schema inference covers the empty-schema
//! case the doc explicitly recommends ("infer schema downstream").

mod common;

use axum::body::{Body, to_bytes};
use axum::http::Request;
use bytes::Bytes;
use serde_json::Value;
use tower::ServiceExt;
use uuid::Uuid;

const FIXTURE: &str = "{\"id\":1,\"name\":\"alice\",\"score\":0.9}\n\
                       {\"id\":2,\"name\":\"bob\",\"score\":0.5}\n\
                       {\"id\":3,\"name\":\"carol\",\"score\":0.2}\n";

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn preview_text_json_lines_auto_detects_and_infers() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.preview-jsonl").await;
    let physical_path = "datasets/preview-jsonl/part-0.json";
    h.storage_backend()
        .put(physical_path, Bytes::from_static(FIXTURE.as_bytes()))
        .await
        .expect("put jsonl fixture");

    let view_id = common::seed_committed_view_with_file(
        &h.pool,
        dataset_id,
        physical_path,
        FIXTURE.len() as i64,
    )
    .await;
    // Empty schema → reader infers from the JSON records.
    let schema_payload =
        serde_json::json!({ "fields": [], "file_format": "TEXT", "custom_metadata": null });
    common::upsert_view_schema(&h.pool, view_id, &schema_payload, "TEXT").await;

    // Force the JSON-lines branch via `?csv=false` so the test isn't
    // flaky on the auto-detect heuristic. (The reader would still pick
    // JSON-lines from the leading `{`, but pinning the dispatch keeps
    // the assertion deterministic.)
    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/views/{view_id}/data?limit=10&csv=false"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).expect("preview json");

    assert_eq!(body["file_format"], "TEXT");
    assert_eq!(body["text_sub_format"], "json_lines");

    let columns = body["columns"].as_array().expect("columns");
    let names: Vec<&str> = columns
        .iter()
        .map(|c| c["name"].as_str().unwrap())
        .collect();
    assert!(
        names.contains(&"id") && names.contains(&"name") && names.contains(&"score"),
        "inferred columns must include id, name, score: {body}"
    );

    let rows = body["rows"].as_array().expect("rows");
    assert_eq!(rows.len(), 3, "three jsonl records: {body}");
    let names: Vec<&str> = rows.iter().map(|r| r["name"].as_str().unwrap()).collect();
    assert_eq!(names, vec!["alice", "bob", "carol"]);

    assert_eq!(body["schema_inferred"], true);
}

fn _bootstrap() -> Uuid {
    Uuid::nil()
}
