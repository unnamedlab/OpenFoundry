//! Foundry compute-seconds cost model for media-set transformations.
//!
//! Pinned line-by-line against the table in
//! `Data formats/Media sets (unstructured data)/Media usage costs and limits.md`
//! ("Transformations" section). The numbers below are the Foundry-published
//! `compute-seconds per GB processed` rates; the `media-transform-runtime-service`
//! consults this module for every access-pattern invocation, multiplies by the
//! input size in GB, and emits the result into:
//!
//!   * `media_compute_seconds_total{transformation, schema}`
//!     (Prometheus counter declared in `services/media-sets-service/src/metrics.rs`).
//!   * The `media_set.access_pattern_invoked` audit event
//!     ([`audit_trail::events::AuditEvent::MediaSetAccessPatternInvoked`]).
//!
//! ## Why this lives in `libs/observability`
//!
//! Multiple services need to read the same table:
//!   * `media-transform-runtime-service` — the actual worker, charges
//!     compute time per call.
//!   * `media-sets-service` — surfaces the cost in the `GET /usage`
//!     endpoint that powers the Usage UI tab.
//!   * Future planning / quota tooling (`tenancy-organizations-service`,
//!     billing exports).
//!
//! Centralising the table here means a Foundry doc update touches one
//! file and a single snapshot test (`cost_model_matches_table.rs`)
//! enforces drift never sneaks past CI.

use serde::{Deserialize, Serialize};

/// Foundry media-set schema discriminator. Mirrors the upper-snake-case
/// proto enum strings (`IMAGE`, `AUDIO`, `VIDEO`, `DOCUMENT`,
/// `SPREADSHEET`, `EMAIL`) used everywhere else in the platform.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum SchemaKind {
    All,
    Image,
    Audio,
    Video,
    Document,
    Spreadsheet,
}

impl SchemaKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::All => "ALL",
            Self::Image => "IMAGE",
            Self::Audio => "AUDIO",
            Self::Video => "VIDEO",
            Self::Document => "DOCUMENT",
            Self::Spreadsheet => "SPREADSHEET",
        }
    }
}

/// Single row of the published cost table.
///
/// `key` is the canonical wire-form transformation name we use across
/// the cost meter, the audit envelope `access_pattern` field, and the
/// `transformation` Prometheus label. `display_name` reproduces the
/// Foundry doc verbatim so the Usage UI can show the same caption an
/// operator would see in the upstream documentation.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct CostEntry {
    /// Wire-form key: lowercase, snake-cased. Stable across releases —
    /// changing it breaks the audit envelope category and the
    /// Prometheus label cardinality.
    pub key: &'static str,
    /// Foundry doc display name (verbatim).
    pub display_name: &'static str,
    pub schema: SchemaKind,
    /// Compute-seconds **per gigabyte** of input processed.
    pub compute_seconds_per_gb: u32,
}

/// Foundry "All" row — applies to download / streaming of bytes
/// regardless of media schema.
pub const DOWNLOAD_STREAM: CostEntry = CostEntry {
    key: "download_stream",
    display_name: "Download / stream",
    schema: SchemaKind::All,
    compute_seconds_per_gb: 2,
};

/// The full table. Order mirrors the doc top-to-bottom so a screen-by-
/// screen review against the upstream rendering catches a missing /
/// reordered row instantly. **DO NOT REORDER without updating the
/// `cost_model_matches_table` snapshot test.**
pub const COST_TABLE: &[CostEntry] = &[
    // ── All ───────────────────────────────────────────────────────
    DOWNLOAD_STREAM,
    // ── Images ────────────────────────────────────────────────────
    CostEntry { key: "generate_pdf",                  display_name: "Generate PDF",                  schema: SchemaKind::Image, compute_seconds_per_gb: 40 },
    // `thumbnail` is documented in `Transforming media.md` as a
    // canonical Foundry use case ("Thumbnails and previews for PDFs
    // in Workshop") but is not separately billed in the published
    // cost table — Foundry charges thumbnails at the resize rate.
    // We surface it explicitly here so the runtime catalog can
    // dispatch it natively without inventing an unbillable kind.
    CostEntry { key: "thumbnail",                     display_name: "Thumbnail (alias of Resize)",   schema: SchemaKind::Image, compute_seconds_per_gb: 40 },
    CostEntry { key: "resize",                        display_name: "Resize",                        schema: SchemaKind::Image, compute_seconds_per_gb: 40 },
    CostEntry { key: "resize_within_bounding_box",    display_name: "Resize within bounding box",    schema: SchemaKind::Image, compute_seconds_per_gb: 40 },
    CostEntry { key: "rotate",                        display_name: "Rotate",                        schema: SchemaKind::Image, compute_seconds_per_gb: 40 },
    CostEntry { key: "adjust_contrast",               display_name: "Adjust contrast",               schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "annotate",                      display_name: "Annotate",                      schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "crop",                          display_name: "Crop / chip",                   schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "encryption_decryption",         display_name: "Encryption / decryption",       schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "geo_tile",                      display_name: "Geo tile",                      schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "grayscale",                     display_name: "Grayscale",                     schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "render_dicom_image_layer",      display_name: "Render DICOM image layer",      schema: SchemaKind::Image, compute_seconds_per_gb: 75 },
    CostEntry { key: "ocr",                           display_name: "Extract text (OCR)",            schema: SchemaKind::Image, compute_seconds_per_gb: 275 },
    CostEntry { key: "embedding",                     display_name: "Generate embedding",            schema: SchemaKind::Image, compute_seconds_per_gb: 275 },
    CostEntry { key: "ocr_v2",                        display_name: "Extract text v2 (OCR)",         schema: SchemaKind::Image, compute_seconds_per_gb: 275 },
    CostEntry { key: "extract_layout_aware_v2",       display_name: "Extract layout aware v2",       schema: SchemaKind::Image, compute_seconds_per_gb: 275 },
    // ── Audio ─────────────────────────────────────────────────────
    CostEntry { key: "chunk",                         display_name: "Chunk",                         schema: SchemaKind::Audio, compute_seconds_per_gb: 75 },
    CostEntry { key: "channel_select",                display_name: "Select channel",                schema: SchemaKind::Audio, compute_seconds_per_gb: 75 },
    CostEntry { key: "hls_stream",                    display_name: "Stream with HLS",               schema: SchemaKind::Audio, compute_seconds_per_gb: 75 },
    CostEntry { key: "transcode",                     display_name: "Transcode",                     schema: SchemaKind::Audio, compute_seconds_per_gb: 75 },
    CostEntry { key: "waveform",                      display_name: "Waveform generation",           schema: SchemaKind::Audio, compute_seconds_per_gb: 75 },
    CostEntry { key: "transcription",                 display_name: "Transcription",                 schema: SchemaKind::Audio, compute_seconds_per_gb: 275 },
    // ── Video ─────────────────────────────────────────────────────
    CostEntry { key: "scene_frames_timestamps",       display_name: "Get timestamps for scene frames", schema: SchemaKind::Video, compute_seconds_per_gb: 40 },
    CostEntry { key: "extract_audio",                 display_name: "Extract audio",                 schema: SchemaKind::Video, compute_seconds_per_gb: 75 },
    CostEntry { key: "extract_frames",                display_name: "Extract frames at timestamp",   schema: SchemaKind::Video, compute_seconds_per_gb: 75 },
    CostEntry { key: "video_chunk",                   display_name: "Chunk",                         schema: SchemaKind::Video, compute_seconds_per_gb: 275 },
    CostEntry { key: "scene_frames_all",              display_name: "Extract all scene frames",      schema: SchemaKind::Video, compute_seconds_per_gb: 275 },
    CostEntry { key: "video_hls_stream",              display_name: "Stream with HLS",               schema: SchemaKind::Video, compute_seconds_per_gb: 275 },
    CostEntry { key: "video_transcode",               display_name: "Transcode",                     schema: SchemaKind::Video, compute_seconds_per_gb: 275 },
    // ── Documents ─────────────────────────────────────────────────
    CostEntry { key: "pdf_page_dimensions",           display_name: "Get PDF page dimensions",       schema: SchemaKind::Document, compute_seconds_per_gb: 40 },
    CostEntry { key: "render_page",                   display_name: "Render page as image",          schema: SchemaKind::Document, compute_seconds_per_gb: 40 },
    CostEntry { key: "render_page_bbox",              display_name: "Render page as image within bounding box", schema: SchemaKind::Document, compute_seconds_per_gb: 40 },
    CostEntry { key: "extract_text_raw",              display_name: "Extract all text (raw)",        schema: SchemaKind::Document, compute_seconds_per_gb: 75 },
    CostEntry { key: "extract_form_fields",           display_name: "Extract form fields",           schema: SchemaKind::Document, compute_seconds_per_gb: 75 },
    CostEntry { key: "extract_toc",                   display_name: "Extract table of contents",     schema: SchemaKind::Document, compute_seconds_per_gb: 75 },
    CostEntry { key: "extract_text_pages_raw",        display_name: "Extract text from pages to array (raw)", schema: SchemaKind::Document, compute_seconds_per_gb: 75 },
    CostEntry { key: "extract_text_on_page_raw",      display_name: "Extract text on page (raw)",    schema: SchemaKind::Document, compute_seconds_per_gb: 75 },
    CostEntry { key: "slice_pdf_range",               display_name: "Slice PDF range",               schema: SchemaKind::Document, compute_seconds_per_gb: 75 },
    CostEntry { key: "doc_ocr",                       display_name: "Extract text (OCR)",            schema: SchemaKind::Document, compute_seconds_per_gb: 275 },
    CostEntry { key: "extract_text_pages_ocr",        display_name: "Extract text from pages to array (OCR)", schema: SchemaKind::Document, compute_seconds_per_gb: 275 },
    CostEntry { key: "layout_aware_v2",               display_name: "Extract layout aware text (raw / OCR)", schema: SchemaKind::Document, compute_seconds_per_gb: 275 },
    CostEntry { key: "vlm_extract",                   display_name: "Extract text using VLM",        schema: SchemaKind::Document, compute_seconds_per_gb: 275 },
    // ── Spreadsheets ──────────────────────────────────────────────
    CostEntry { key: "render_sheet",                  display_name: "Convert sheet to JSON",         schema: SchemaKind::Spreadsheet, compute_seconds_per_gb: 275 },
];

/// Lookup the rate for a transformation key. Returns `None` if the key
/// is not in the published table — callers should refuse to charge for
/// an off-table transformation rather than guess. The runtime catalog
/// is the single source of truth for which keys are billable.
pub fn rate_per_gb(key: &str) -> Option<u32> {
    COST_TABLE
        .iter()
        .find(|entry| entry.key == key)
        .map(|entry| entry.compute_seconds_per_gb)
}

/// Compute-seconds charged for processing `bytes` through the
/// transformation identified by `key`. Rounds **up** to the nearest
/// whole compute-second so a 1-byte invocation still charges at least
/// the entry's GB rate × byte fraction (rounded up). Returns `None` if
/// `key` is not in the table.
pub fn charge_compute_seconds(key: &str, bytes: u64) -> Option<u64> {
    let rate = rate_per_gb(key)? as u64;
    // Compute-seconds = rate × (bytes / GB). Use 64-bit math throughout
    // to keep multi-GB inputs precise.
    const GB: u64 = 1024 * 1024 * 1024;
    let charged = (rate.saturating_mul(bytes) + GB - 1) / GB;
    // Tiny inputs still incur measurable cost (the table publishes the
    // GB rate; we expose at minimum 1 compute-second for any non-zero
    // request so the meter never reports a 0-charge invocation).
    Some(charged.max(if bytes == 0 { 0 } else { 1 }))
}

/// Convenience: lookup the entry for a key, including its display name
/// and schema. Used by the `/usage` REST surface and the Usage UI tab.
pub fn entry(key: &str) -> Option<CostEntry> {
    COST_TABLE.iter().copied().find(|entry| entry.key == key)
}

/// Iterator over every transformation belonging to a given schema
/// (plus the `All` rows). Backs the `/usage` JSON breakdown so the UI
/// can populate the per-kind chart without knowing the table layout.
pub fn entries_for(schema: SchemaKind) -> impl Iterator<Item = &'static CostEntry> {
    COST_TABLE
        .iter()
        .filter(move |entry| entry.schema == schema || entry.schema == SchemaKind::All)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn download_stream_rate_matches_doc() {
        assert_eq!(rate_per_gb("download_stream"), Some(2));
    }

    #[test]
    fn ocr_charges_275_per_gb_in_image_schema() {
        assert_eq!(rate_per_gb("ocr"), Some(275));
        assert_eq!(entry("ocr").unwrap().schema, SchemaKind::Image);
    }

    #[test]
    fn unknown_key_is_not_billable() {
        assert_eq!(rate_per_gb("not_a_real_transformation"), None);
        assert_eq!(charge_compute_seconds("not_a_real_transformation", 1024), None);
    }

    #[test]
    fn charge_rounds_up_and_respects_minimum() {
        // 1 GB through `download_stream` = 2 compute-seconds exactly.
        assert_eq!(
            charge_compute_seconds("download_stream", 1024 * 1024 * 1024),
            Some(2)
        );
        // 1 byte must still register a non-zero charge (rounded to 1).
        assert_eq!(charge_compute_seconds("download_stream", 1), Some(1));
        // 0 bytes processes nothing.
        assert_eq!(charge_compute_seconds("download_stream", 0), Some(0));
    }

    #[test]
    fn entries_for_image_includes_all_schema_rows() {
        // `All` rows (download_stream) must appear under every schema
        // so the per-kind UI breakdown is self-contained.
        let keys: Vec<&str> = entries_for(SchemaKind::Image)
            .map(|e| e.key)
            .collect();
        assert!(keys.contains(&"download_stream"), "All rows must be visible");
        assert!(keys.contains(&"ocr"), "Image rows must be visible");
        assert!(!keys.contains(&"transcription"), "Audio rows must NOT leak");
    }
}
