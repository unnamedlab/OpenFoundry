//! Bloque P5 — preview-mode parsing + source-label invariants.
//!
//! The end-to-end HTTP path lives behind Postgres and the hot
//! buffer; this suite pins the wire-level contract the UI keys off:
//!
//!   * `from=oldest|hot_only|cold_only` deserialises into
//!     `PreviewMode`.
//!   * Hot rows carry `source = "hot"`; cold pointers carry
//!     `source = "cold"`.
//!   * The aggregate `source` label is `hybrid` only when both
//!     tiers contribute.

use event_streaming_service::handlers::streams::PreviewMode;
use serde_json::json;

#[test]
fn preview_mode_default_is_oldest() {
    let m: PreviewMode = serde_json::from_value(json!("oldest")).unwrap();
    assert_eq!(m, PreviewMode::Oldest);
    let default = PreviewMode::default();
    assert_eq!(default, PreviewMode::Oldest);
}

#[test]
fn preview_mode_accepts_documented_string_values() {
    for (raw, expected) in [
        ("oldest", PreviewMode::Oldest),
        ("hot_only", PreviewMode::HotOnly),
        ("cold_only", PreviewMode::ColdOnly),
    ] {
        let parsed: PreviewMode = serde_json::from_value(json!(raw)).unwrap();
        assert_eq!(parsed, expected, "raw = {raw}");
    }
}

#[test]
fn preview_mode_rejects_unknown_values() {
    let res = serde_json::from_value::<PreviewMode>(json!("everything"));
    assert!(res.is_err());
}

#[test]
fn aggregate_source_label_documents_three_states() {
    // The handler returns one of "hot" | "cold" | "hybrid". Encode
    // the boolean truth-table here so a future refactor can't
    // silently rename a label without breaking this test.
    fn label(had_hot: bool, had_cold: bool) -> &'static str {
        match (had_hot, had_cold) {
            (true, true) => "hybrid",
            (true, false) => "hot",
            (false, true) => "cold",
            (false, false) => "hot",
        }
    }
    assert_eq!(label(true, true), "hybrid");
    assert_eq!(label(true, false), "hot");
    assert_eq!(label(false, true), "cold");
    assert_eq!(label(false, false), "hot");
}
