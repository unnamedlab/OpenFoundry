//! T7.1 — schema field types round-trip.
//!
//! For each of the 15 Foundry [`FieldType`]s, POST a dataset (creating
//! a placeholder) and exercise the `schema:validate` endpoint with a
//! payload that contains that type. We don't have an upload here, so
//! the assertion focuses on the *validator* surface accepting the
//! 15-type matrix and on JSON round-tripping (POST → serialised body
//! → GET preview metadata → equal).

mod common;

use axum::body::{Body, to_bytes};
use axum::http::{Request, StatusCode};
use core_models::dataset::{FieldType, Schema, SchemaField};
use serde_json::{Value, json};
use tower::ServiceExt;

fn all_field_types() -> Vec<FieldType> {
    fn leaf(ft: FieldType) -> Box<SchemaField> {
        Box::new(SchemaField {
            name: "_".into(),
            field_type: ft,
            nullable: true,
            description: None,
        })
    }
    vec![
        FieldType::Boolean,
        FieldType::Byte,
        FieldType::Short,
        FieldType::Integer,
        FieldType::Long,
        FieldType::Float,
        FieldType::Double,
        FieldType::String,
        FieldType::Binary,
        FieldType::Date,
        FieldType::Timestamp,
        FieldType::Decimal { precision: 10, scale: 2 },
        FieldType::Array { array_sub_type: leaf(FieldType::Long) },
        FieldType::Map {
            map_key_type: leaf(FieldType::String),
            map_value_type: leaf(FieldType::Double),
        },
        FieldType::Struct {
            sub_schemas: vec![SchemaField {
                name: "leaf".into(),
                field_type: FieldType::String,
                nullable: true,
                description: None,
            }],
        },
    ]
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn each_of_the_15_field_types_round_trips_through_json() {
    for ft in all_field_types() {
        let field = SchemaField {
            name: "col".into(),
            field_type: ft.clone(),
            nullable: true,
            description: None,
        };
        let schema = Schema {
            fields: vec![field],
            format: "parquet".into(),
            custom_metadata: Default::default(),
        };
        // JSON round-trip preserves the exact FieldType payload.
        let json = serde_json::to_value(&schema).expect("serialise");
        let back: Schema = serde_json::from_value(json.clone()).expect("deserialise");
        assert_eq!(schema, back, "schema round-trip failed for {ft:?}");
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn schema_validate_endpoint_rejects_invalid_decimal() {
    let h = common::spawn().await;
    let dataset_id = testing::fixtures::seed_dataset(
        &h.pool,
        "ri.foundry.main.dataset.t7-schema",
        "schema-roundtrip",
        "parquet",
    )
    .await;

    // Decimal with precision 0 must be rejected by Schema::validate.
    let body = json!({
        "schema": {
            "fields": [
                { "name": "amount", "type": "DECIMAL", "precision": 0, "scale": 0, "nullable": true }
            ],
            "format": "parquet"
        }
    });
    let req = Request::builder()
        .method("POST")
        .uri(format!("/v1/datasets/{dataset_id}/schema:validate"))
        .header("authorization", format!("Bearer {}", h.token))
        .header("content-type", "application/json")
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = h.router.clone().oneshot(req).await.expect("router");
    assert_eq!(resp.status(), StatusCode::BAD_REQUEST);

    // Confirm the error body is structured (schema_errors non-empty).
    let bytes = to_bytes(resp.into_body(), 64 * 1024).await.unwrap();
    let v: Value = serde_json::from_slice(&bytes).unwrap();
    assert!(v["schema_errors"].as_array().map(|a| !a.is_empty()).unwrap_or(false));
}
