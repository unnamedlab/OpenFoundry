//! Pure-logic unit tests for the Vespa backend.
//!
//! These do **not** spin up a real Vespa container; they only exercise
//! request/response shaping so they can run on every PR. End-to-end
//! tests against a real `vespaengine/vespa` image live under
//! `tests/vespa_integration.rs` and are `#[ignore]`d because they need
//! Docker, which is not available in every environment.

use serde_json::json;

use super::client::{VespaBackend, VespaConfig, parse_search_response};
use crate::backend::Filter;

fn backend() -> VespaBackend {
    VespaBackend::new(VespaConfig::new("http://vespa.test:8080"))
        .expect("client must build with default config")
}

#[test]
fn document_url_encodes_id_and_uses_namespace() {
    let b = backend();
    let url = b.document_url("doc/with space");
    assert_eq!(
        url,
        "http://vespa.test:8080/document/v1/default/doc/docid/doc%2Fwith%20space"
    );
}

#[test]
fn search_url_is_well_formed() {
    let b = backend();
    assert_eq!(b.search_url(), "http://vespa.test:8080/search/");
}

#[test]
fn build_search_body_includes_yql_ranking_and_tensor() {
    let b = backend();
    let body = b.build_search_body("hello", &[0.1_f32, 0.2, 0.3], &Filter::default(), 5);
    assert_eq!(body["hits"], json!(5));
    assert_eq!(body["ranking.profile"], json!("hybrid"));
    let yql = body["yql"].as_str().unwrap();
    assert!(yql.contains("text contains \"hello\""), "yql was: {yql}");
    assert!(
        yql.contains("nearestNeighbor(embedding,q_embedding)"),
        "yql was: {yql}"
    );
    let tensor = body["input.query(q_embedding)"].as_array().unwrap();
    assert_eq!(tensor.len(), 3);
}

#[test]
fn build_search_body_with_filter_appends_and_clause() {
    let b = backend();
    let body = b.build_search_body("", &[0.1_f32, 0.2], &Filter::eq("tenant_id", "acme"), 3);
    let yql = body["yql"].as_str().unwrap();
    assert!(
        yql.contains("and (tenant_id contains \"acme\")"),
        "yql was: {yql}"
    );
}

#[test]
fn build_search_body_text_only_omits_tensor_input() {
    let b = backend();
    let body = b.build_search_body("hello", &[], &Filter::default(), 4);
    assert!(body.get("input.query(q_embedding)").is_none());
    let yql = body["yql"].as_str().unwrap();
    assert!(!yql.contains("nearestNeighbor"), "yql was: {yql}");
}

#[test]
fn yql_escape_quotes_safely() {
    let b = backend();
    let body = b.build_search_body("she said \"hi\"", &[], &Filter::default(), 1);
    let yql = body["yql"].as_str().unwrap();
    assert!(
        yql.contains("text contains \"she said \\\"hi\\\"\""),
        "yql was: {yql}"
    );
}

#[test]
fn parse_search_response_extracts_id_score_and_fields() {
    let raw = json!({
        "root": {
            "children": [
                {
                    "id": "id:default:doc::abc-1",
                    "relevance": 1.42,
                    "fields": { "text": "hello world" }
                },
                {
                    "id": "id:default:doc::abc-2",
                    "relevance": 0.71,
                    "fields": {}
                }
            ]
        }
    });
    let hits = parse_search_response(&raw).unwrap();
    assert_eq!(hits.len(), 2);
    assert_eq!(hits[0].id, "abc-1");
    assert!((hits[0].score - 1.42).abs() < 1e-9);
    assert_eq!(hits[0].fields.get("text").unwrap(), &json!("hello world"));
    assert_eq!(hits[1].id, "abc-2");
}

#[test]
fn parse_search_response_handles_empty_children() {
    let raw = json!({ "root": {} });
    let hits = parse_search_response(&raw).unwrap();
    assert!(hits.is_empty());
}
