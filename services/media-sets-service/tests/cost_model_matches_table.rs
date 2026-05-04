//! H5 — `observability::COST_TABLE` is a verbatim mirror of the
//! Foundry "Media usage costs and limits.md" doc. This test pins the
//! table line-by-line so a doc update or a reordering can never sneak
//! through CI without an explicit rebuild of the snapshot.
//!
//! The expected list below is the canonical doc transcribed
//! programmatically; if Foundry publishes a rate change, update both
//! the doc-mirror in `libs/observability/src/cost_model.rs` AND this
//! snapshot in the same commit.
//!
//! ## Why pin both `key` and `compute_seconds_per_gb`
//!
//! `key` is the wire-form discriminator the audit envelope, the
//! Prometheus label and the runtime catalog all share. A drift in
//! either column silently rebills historical activity at the wrong
//! rate, so we assert the full row.

use observability::{COST_TABLE, SchemaKind};

#[test]
fn cost_table_matches_published_foundry_doc() {
    // Each entry: (key, schema, compute_seconds_per_gb).
    // Order MUST match the doc top-to-bottom; reordering breaks
    // visual review against the upstream rendering.
    let expected: &[(&str, SchemaKind, u32)] = &[
        // ── All ───────────────────────────────────────────────────
        ("download_stream", SchemaKind::All, 2),
        // ── Images ────────────────────────────────────────────────
        ("generate_pdf", SchemaKind::Image, 40),
        ("thumbnail", SchemaKind::Image, 40),
        ("resize", SchemaKind::Image, 40),
        ("resize_within_bounding_box", SchemaKind::Image, 40),
        ("rotate", SchemaKind::Image, 40),
        ("adjust_contrast", SchemaKind::Image, 75),
        ("annotate", SchemaKind::Image, 75),
        ("crop", SchemaKind::Image, 75),
        ("encryption_decryption", SchemaKind::Image, 75),
        ("geo_tile", SchemaKind::Image, 75),
        ("grayscale", SchemaKind::Image, 75),
        ("render_dicom_image_layer", SchemaKind::Image, 75),
        ("ocr", SchemaKind::Image, 275),
        ("embedding", SchemaKind::Image, 275),
        ("ocr_v2", SchemaKind::Image, 275),
        ("extract_layout_aware_v2", SchemaKind::Image, 275),
        // ── Audio ─────────────────────────────────────────────────
        ("chunk", SchemaKind::Audio, 75),
        ("channel_select", SchemaKind::Audio, 75),
        ("hls_stream", SchemaKind::Audio, 75),
        ("transcode", SchemaKind::Audio, 75),
        ("waveform", SchemaKind::Audio, 75),
        ("transcription", SchemaKind::Audio, 275),
        // ── Video ─────────────────────────────────────────────────
        ("scene_frames_timestamps", SchemaKind::Video, 40),
        ("extract_audio", SchemaKind::Video, 75),
        ("extract_frames", SchemaKind::Video, 75),
        ("video_chunk", SchemaKind::Video, 275),
        ("scene_frames_all", SchemaKind::Video, 275),
        ("video_hls_stream", SchemaKind::Video, 275),
        ("video_transcode", SchemaKind::Video, 275),
        // ── Documents ─────────────────────────────────────────────
        ("pdf_page_dimensions", SchemaKind::Document, 40),
        ("render_page", SchemaKind::Document, 40),
        ("render_page_bbox", SchemaKind::Document, 40),
        ("extract_text_raw", SchemaKind::Document, 75),
        ("extract_form_fields", SchemaKind::Document, 75),
        ("extract_toc", SchemaKind::Document, 75),
        ("extract_text_pages_raw", SchemaKind::Document, 75),
        ("extract_text_on_page_raw", SchemaKind::Document, 75),
        ("slice_pdf_range", SchemaKind::Document, 75),
        ("doc_ocr", SchemaKind::Document, 275),
        ("extract_text_pages_ocr", SchemaKind::Document, 275),
        ("layout_aware_v2", SchemaKind::Document, 275),
        ("vlm_extract", SchemaKind::Document, 275),
        // ── Spreadsheets ──────────────────────────────────────────
        ("render_sheet", SchemaKind::Spreadsheet, 275),
    ];

    assert_eq!(
        COST_TABLE.len(),
        expected.len(),
        "row count drift: COST_TABLE has {} entries, snapshot has {}",
        COST_TABLE.len(),
        expected.len()
    );
    for (i, (entry, want)) in COST_TABLE.iter().zip(expected.iter()).enumerate() {
        assert_eq!(
            entry.key, want.0,
            "row {i} key: COST_TABLE has `{}`, snapshot wants `{}`",
            entry.key, want.0
        );
        assert_eq!(
            entry.schema, want.1,
            "row {i} ({}) schema: COST_TABLE has {:?}, snapshot wants {:?}",
            entry.key, entry.schema, want.1
        );
        assert_eq!(
            entry.compute_seconds_per_gb, want.2,
            "row {i} ({}) rate: COST_TABLE has {}, snapshot wants {}",
            entry.key, entry.compute_seconds_per_gb, want.2
        );
    }
}
