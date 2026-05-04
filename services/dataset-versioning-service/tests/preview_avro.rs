//! P2 — preview an AVRO view; columns and values must come back from
//! the writer schema embedded in the file when the dataset's persisted
//! schema is empty (Foundry doc § "File formats" recommends Avro for
//! self-describing payloads).

mod common;

use apache_avro::types::Value as AvroValue;
use apache_avro::{Schema as AvroSchema, Writer};
use axum::body::{Body, to_bytes};
use axum::http::Request;
use bytes::Bytes;
use serde_json::Value;
use tower::ServiceExt;
use uuid::Uuid;

const AVRO_WRITER_SCHEMA: &str = r#"{
    "type": "record",
    "name": "PreviewRow",
    "fields": [
        { "name": "id",   "type": "long" },
        { "name": "name", "type": "string" },
        { "name": "ok",   "type": ["null", "boolean"], "default": null }
    ]
}"#;

fn build_avro_fixture() -> Vec<u8> {
    let schema = AvroSchema::parse_str(AVRO_WRITER_SCHEMA).expect("parse avro schema");
    let mut writer = Writer::new(&schema, Vec::new());
    for (id, name, ok) in [
        (1i64, "alice", Some(true)),
        (2, "bob", Some(false)),
        (3, "carol", None),
    ] {
        let mut record = apache_avro::types::Record::new(writer.schema()).unwrap();
        record.put("id", AvroValue::Long(id));
        record.put("name", AvroValue::String(name.into()));
        let union_value = match ok {
            Some(b) => AvroValue::Union(1, Box::new(AvroValue::Boolean(b))),
            None => AvroValue::Union(0, Box::new(AvroValue::Null)),
        };
        record.put("ok", union_value);
        writer.append(record).expect("append avro record");
    }
    writer.into_inner().expect("flush avro")
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn preview_avro_infers_columns_and_values_from_writer_schema() {
    let h = common::spawn().await;
    let dataset_id =
        common::seed_dataset_with_master(&h.pool, "ri.foundry.main.dataset.preview-avro").await;

    let bytes_vec = build_avro_fixture();
    let physical_path = "datasets/preview-avro/part-0.avro";
    h.storage_backend()
        .put(physical_path, Bytes::from(bytes_vec.clone()))
        .await
        .expect("put avro fixture");

    let view_id = common::seed_committed_view_with_file(
        &h.pool,
        dataset_id,
        physical_path,
        bytes_vec.len() as i64,
    )
    .await;
    // Empty schema → reader uses the embedded Avro writer schema.
    let schema_payload =
        serde_json::json!({ "fields": [], "file_format": "AVRO", "custom_metadata": null });
    common::upsert_view_schema(&h.pool, view_id, &schema_payload, "AVRO").await;

    let req = Request::builder()
        .method("GET")
        .uri(format!(
            "/v1/datasets/{dataset_id}/views/{view_id}/data?limit=10"
        ))
        .header("authorization", format!("Bearer {}", h.token))
        .body(Body::empty())
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), axum::http::StatusCode::OK);
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let body: Value = serde_json::from_slice(&bytes).expect("preview json");

    assert_eq!(body["file_format"], "AVRO");

    let columns = body["columns"].as_array().expect("columns");
    let names: Vec<&str> = columns
        .iter()
        .map(|c| c["name"].as_str().unwrap())
        .collect();
    assert_eq!(
        names,
        vec!["id", "name", "ok"],
        "avro writer-schema columns surface as preview columns: {body}"
    );

    let rows = body["rows"].as_array().expect("rows");
    assert_eq!(rows.len(), 3, "three records: {body}");
    assert_eq!(rows[0]["id"], 1);
    assert_eq!(rows[0]["name"], "alice");
    assert_eq!(rows[0]["ok"], true);
    assert_eq!(
        rows[2]["ok"],
        Value::Null,
        "Avro null union round-trips: {body}"
    );

    // Schema row was empty → reader fell back to inference; the UI
    // banner should fire.
    assert_eq!(body["schema_inferred"], true);
}

fn _bootstrap() -> Uuid {
    Uuid::nil()
}
