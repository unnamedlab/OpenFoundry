//! Validates that the Foundry-style media node types added in P1.4
//! parse, validate and emit a stable NodePalette.
//!
//! These tests live at the *authoring* layer — no execution runs and
//! no real `media-sets-service` instance is required, since the lib
//! exposes the validators as side-effect-free pure functions
//! (`pipeline_authoring_service::{media_nodes, expressions}`).

use pipeline_authoring_service::expressions::{self, MediaExpressionKind};
use pipeline_authoring_service::media_nodes::{
    self, ALL_MEDIA_TRANSFORM_TYPES, CONVERT_MEDIA_SET_TO_TABLE_ROWS, GET_MEDIA_REFERENCES,
    MEDIA_SET_INPUT, MEDIA_SET_OUTPUT, MEDIA_TRANSFORM,
};
use serde_json::{Value, json};

// ---------------------------------------------------------------------------
// Node palette
// ---------------------------------------------------------------------------

#[test]
fn node_palette_lists_every_media_transform_type() {
    let palette = media_nodes::node_palette();
    let arr = palette.as_array().expect("palette is a JSON array");
    let listed: std::collections::HashSet<&str> = arr
        .iter()
        .map(|node| node["type"].as_str().expect("type is string"))
        .collect();

    for kind in ALL_MEDIA_TRANSFORM_TYPES {
        assert!(
            listed.contains(kind),
            "node palette is missing entry for `{kind}`; UI will not surface it"
        );
    }
}

#[test]
fn node_palette_media_transform_lists_every_kind() {
    let palette = media_nodes::node_palette();
    let media_transform = palette
        .as_array()
        .unwrap()
        .iter()
        .find(|node| node["type"] == MEDIA_TRANSFORM)
        .expect("MEDIA_TRANSFORM entry must exist in the palette");
    let kinds: std::collections::HashSet<&str> = media_transform["kinds"]
        .as_array()
        .expect("kinds is array")
        .iter()
        .map(|k| k["id"].as_str().expect("kind id is string"))
        .collect();
    let expected = [
        "extract_text_ocr",
        "resize",
        "rotate",
        "crop",
        "transcribe_audio",
        "generate_embedding",
        "render_pdf_page",
        "extract_layout_aware",
    ];
    for kind in expected {
        assert!(
            kinds.contains(kind),
            "media_transform missing kind `{kind}`"
        );
    }
}

// ---------------------------------------------------------------------------
// Per-kind config validation
// ---------------------------------------------------------------------------

#[test]
fn media_set_input_accepts_canonical_config() {
    let errs = media_nodes::validate_media_node(
        MEDIA_SET_INPUT,
        &json!({
            "media_set_rid": "ri.foundry.main.media_set.018f0000-aaaa-bbbb-cccc-000000000010",
            "branch":        "main"
        }),
    );
    assert!(errs.is_empty(), "expected zero errors, got {errs:?}");
}

#[test]
fn media_set_input_rejects_non_rid() {
    let errs = media_nodes::validate_media_node(
        MEDIA_SET_INPUT,
        &json!({ "media_set_rid": "not-a-rid", "branch": "main" }),
    );
    assert!(
        errs.iter()
            .any(|e| e.contains("not a Foundry media-set RID")),
        "{errs:?}"
    );
}

#[test]
fn media_set_output_requires_exactly_one_target_form() {
    // Neither bound nor created → error.
    let neither = media_nodes::validate_media_node(MEDIA_SET_OUTPUT, &json!({ "branch": "main" }));
    assert!(
        neither
            .iter()
            .any(|e| e.contains("media_set_rid") && e.contains("create_if_missing")),
        "{neither:?}"
    );

    // Both → error.
    let both = media_nodes::validate_media_node(
        MEDIA_SET_OUTPUT,
        &json!({
            "media_set_rid": "ri.foundry.main.media_set.x",
            "create_if_missing": {
                "project_rid": "ri.foundry.main.project.p",
                "name":        "thumbs",
                "schema":      "IMAGE"
            }
        }),
    );
    assert!(both.iter().any(|e| e.contains("exactly one")), "{both:?}");

    // Bound only → ok.
    let ok = media_nodes::validate_media_node(
        MEDIA_SET_OUTPUT,
        &json!({ "media_set_rid": "ri.foundry.main.media_set.x" }),
    );
    assert!(ok.is_empty(), "{ok:?}");
}

#[test]
fn media_set_output_rejects_replace_on_transactionless_target() {
    // Foundry: TRANSACTIONLESS sets only support `modify` writes.
    let errs = media_nodes::validate_media_node(
        MEDIA_SET_OUTPUT,
        &json!({
            "create_if_missing": {
                "project_rid":        "ri.foundry.main.project.p",
                "name":               "thumbs",
                "schema":             "IMAGE",
                "transaction_policy": "TRANSACTIONLESS"
            },
            "write_mode": "replace"
        }),
    );
    assert!(
        errs.iter()
            .any(|e| e.contains("write_mode=replace requires a TRANSACTIONAL")),
        "{errs:?}"
    );
}

#[test]
fn media_transform_validates_required_params_per_kind() {
    let cases: &[(&str, Value, bool, &str)] = &[
        (
            "resize w/o height",
            json!({ "kind": "resize", "params": { "width": 256 } }),
            false,
            "`height`",
        ),
        (
            "resize ok",
            json!({ "kind": "resize", "params": { "width": 256, "height": 256 } }),
            true,
            "",
        ),
        (
            "rotate w/o degrees",
            json!({ "kind": "rotate", "params": {} }),
            false,
            "`degrees`",
        ),
        (
            "crop missing keys",
            json!({ "kind": "crop", "params": { "x": 0, "y": 0 } }),
            false,
            "`width`",
        ),
        (
            "transcribe ok (no params)",
            json!({ "kind": "transcribe_audio", "params": {} }),
            true,
            "",
        ),
        (
            "ocr ok (no params)",
            json!({ "kind": "extract_text_ocr" }),
            true,
            "",
        ),
        (
            "render_pdf_page ok",
            json!({ "kind": "render_pdf_page", "params": { "page": 3 } }),
            true,
            "",
        ),
        (
            "render_pdf_page w/o page",
            json!({ "kind": "render_pdf_page", "params": {} }),
            false,
            "`page`",
        ),
    ];
    for (name, cfg, should_pass, expected_substring) in cases {
        let errs = media_nodes::validate_media_node(MEDIA_TRANSFORM, cfg);
        if *should_pass {
            assert!(errs.is_empty(), "[{name}] expected ok, got {errs:?}");
        } else {
            assert!(
                errs.iter().any(|e| e.contains(expected_substring)),
                "[{name}] expected error containing `{expected_substring}`, got {errs:?}"
            );
        }
    }
}

#[test]
fn convert_media_set_to_table_rows_validates_source_rid() {
    let bad = media_nodes::validate_media_node(
        CONVERT_MEDIA_SET_TO_TABLE_ROWS,
        &json!({ "source_media_set_rid": "garbage" }),
    );
    assert!(
        bad.iter()
            .any(|e| e.contains("not a Foundry media-set RID")),
        "{bad:?}"
    );

    let ok = media_nodes::validate_media_node(
        CONVERT_MEDIA_SET_TO_TABLE_ROWS,
        &json!({
            "source_media_set_rid": "ri.foundry.main.media_set.018f-set",
            "branch":               "main"
        }),
    );
    assert!(ok.is_empty(), "{ok:?}");
}

#[test]
fn get_media_references_validates_target_rid() {
    let dataset_id = uuid::Uuid::now_v7();
    let bad = media_nodes::validate_media_node(
        GET_MEDIA_REFERENCES,
        &json!({
            "source_dataset_id":    dataset_id.to_string(),
            "target_media_set_rid": "wrong-prefix"
        }),
    );
    assert!(
        bad.iter()
            .any(|e| e.contains("not a Foundry media-set RID")),
        "{bad:?}"
    );

    let ok = media_nodes::validate_media_node(
        GET_MEDIA_REFERENCES,
        &json!({
            "source_dataset_id":    dataset_id.to_string(),
            "target_media_set_rid": "ri.foundry.main.media_set.018f-target"
        }),
    );
    assert!(ok.is_empty(), "{ok:?}");
}

// ---------------------------------------------------------------------------
// Pipeline expressions
// ---------------------------------------------------------------------------

#[test]
fn is_valid_media_reference_returns_true_for_canonical_payload() {
    let mr = json!({
        "mediaSetRid":  "ri.foundry.main.media_set.set-1",
        "mediaItemRid": "ri.foundry.main.media_item.item-1",
        "branch":       "main",
        "schema":       "IMAGE"
    })
    .to_string();
    let out = expressions::evaluate(
        MediaExpressionKind::IsValidMediaReference,
        &json!({ "input": mr }),
    )
    .expect("evaluation must not fail");
    assert_eq!(out, Value::Bool(true));
}

#[test]
fn is_valid_media_reference_returns_false_for_non_media_json() {
    let out = expressions::evaluate(
        MediaExpressionKind::IsValidMediaReference,
        &json!({ "input": "{\"unrelated\":42}" }),
    )
    .expect("evaluation must not fail");
    assert_eq!(out, Value::Bool(false));
}

#[test]
fn construct_delegated_media_gid_emits_expected_string() {
    let out = expressions::evaluate(
        MediaExpressionKind::ConstructDelegatedMediaGid,
        &json!({
            "media_set_rid":  "ri.foundry.main.media_set.alpha",
            "media_item_rid": "ri.foundry.main.media_item.beta",
            "branch":         "main",
            "schema":         "AUDIO"
        }),
    )
    .expect("evaluation must not fail");
    assert_eq!(
        out,
        Value::String(
            "gid:foundry/ri.foundry.main.media_set.alpha/main/ri.foundry.main.media_item.beta\
             #schema=AUDIO"
                .to_string()
        )
    );
}

#[test]
fn construct_delegated_media_gid_rejects_invalid_inputs_at_validate_time() {
    let errs = expressions::validate(
        MediaExpressionKind::ConstructDelegatedMediaGid,
        &json!({
            "media_set_rid":  "wrong",
            "media_item_rid": "ri.foundry.main.media_item.x",
            "schema":         "IMAGE"
        }),
    );
    assert!(errs.iter().any(|e| e.contains("media_set_rid")), "{errs:?}");
}
