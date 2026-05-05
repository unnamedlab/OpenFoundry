//! Worker catalog. Maps every Foundry-published access-pattern `kind`
//! to the implementation status of its runtime handler.
//!
//! The catalog is used to:
//!   * Drive the dispatch table in [`crate::handlers`].
//!   * Power the `GET /catalog` endpoint so neighbouring services
//!     (and CI) can cross-check that every billable transformation in
//!     `observability::cost_model::COST_TABLE` has a runtime mapping.
//!
//! Status is intentionally enumerated, not booleanised, so the
//! follow-up wiring of an external binary lights up gradually as
//! handlers move from `NotImplemented(reason)` → `Native` /
//! `External(binary)`.

use serde::Serialize;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum HandlerStatus {
    /// Implemented in pure Rust against `image` / `serde_json` /
    /// stdlib. Available unconditionally.
    Native,
    /// Implemented but delegates to an external binary at runtime
    /// (`ffmpeg`, `tesseract`, `pdfium`, …). The runtime returns 501
    /// when the binary is not on `PATH`; the catalog still surfaces
    /// the entry as `External` so operators see what is *eventually*
    /// supported.
    External { binary: &'static str },
    /// Not yet wired. The handler returns 501 with the canonical
    /// `reason` so callers can degrade.
    NotImplemented { reason: &'static str },
}

#[derive(Debug, Clone, Copy, Serialize)]
pub struct CatalogEntry {
    /// Wire-form key matching `observability::cost_model::COST_TABLE`.
    pub key: &'static str,
    pub status: HandlerStatus,
}

/// Every transformation the runtime knows about. Keys MUST match a
/// row in `observability::cost_model::COST_TABLE`; the
/// `cost_model_matches_table.rs` snapshot test enforces that drift.
pub const CATALOG: &[CatalogEntry] = &[
    // ── Image (pure-Rust via `image` crate) ───────────────────────
    CatalogEntry { key: "thumbnail",                  status: HandlerStatus::Native },
    CatalogEntry { key: "resize",                     status: HandlerStatus::Native },
    CatalogEntry { key: "resize_within_bounding_box", status: HandlerStatus::Native },
    CatalogEntry { key: "rotate",                     status: HandlerStatus::Native },
    CatalogEntry { key: "crop",                       status: HandlerStatus::Native },
    CatalogEntry { key: "grayscale",                  status: HandlerStatus::Native },
    CatalogEntry { key: "geo_tile",                   status: HandlerStatus::NotImplemented { reason: "Geo tile pyramids land in the geospatial-intelligence-service follow-up." } },
    // H7 — DICOM window/level rendering. Foundry's `render_dicom_image_layer`
    // (75 cs/GB) shells out to `dcmtk`'s `dcm2pnm` for per-instance pixel
    // extraction; cargo-side we surface it as External so the catalog
    // shows the intended shape without forcing dcmtk on the build host.
    CatalogEntry { key: "render_dicom_image_layer",   status: HandlerStatus::External { binary: "dcmtk" } },
    CatalogEntry { key: "ocr",                        status: HandlerStatus::External { binary: "tesseract" } },
    CatalogEntry { key: "embedding",                  status: HandlerStatus::NotImplemented { reason: "Image embeddings depend on libs/ai-kernel which is not yet wired." } },

    // ── Audio (ffmpeg) ────────────────────────────────────────────
    CatalogEntry { key: "chunk",                      status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "channel_select",             status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "hls_stream",                 status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "transcode",                  status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "waveform",                   status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "transcription",              status: HandlerStatus::NotImplemented { reason: "Transcription depends on libs/ai-kernel (Whisper / VLM) which is not yet wired." } },

    // ── Video (ffmpeg + HLS packager) ─────────────────────────────
    CatalogEntry { key: "scene_frames_timestamps",    status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "extract_audio",              status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "extract_frames",             status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "video_chunk",                status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "scene_frames_all",           status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "video_hls_stream",           status: HandlerStatus::External { binary: "ffmpeg" } },
    CatalogEntry { key: "video_transcode",            status: HandlerStatus::External { binary: "ffmpeg" } },

    // ── Document (pdfium / poppler / tesseract) ───────────────────
    CatalogEntry { key: "pdf_page_dimensions",        status: HandlerStatus::External { binary: "pdfium" } },
    CatalogEntry { key: "render_page",                status: HandlerStatus::External { binary: "pdfium" } },
    CatalogEntry { key: "extract_text_raw",           status: HandlerStatus::External { binary: "pdftotext" } },
    CatalogEntry { key: "extract_form_fields",        status: HandlerStatus::External { binary: "pdfium" } },
    CatalogEntry { key: "extract_toc",                status: HandlerStatus::External { binary: "pdfium" } },
    CatalogEntry { key: "slice_pdf_range",            status: HandlerStatus::External { binary: "qpdf" } },
    CatalogEntry { key: "doc_ocr",                    status: HandlerStatus::External { binary: "tesseract" } },
    CatalogEntry { key: "layout_aware_v2",            status: HandlerStatus::NotImplemented { reason: "Layout-aware extraction depends on libs/ai-kernel which is not yet wired." } },
    CatalogEntry { key: "vlm_extract",                status: HandlerStatus::NotImplemented { reason: "VLM extraction depends on libs/ai-kernel which is not yet wired." } },

    // ── Spreadsheet ───────────────────────────────────────────────
    CatalogEntry { key: "render_sheet",               status: HandlerStatus::NotImplemented { reason: "Spreadsheet rendering depends on services/spreadsheet-computation-service which is not yet wired into the runtime." } },
];

/// Lookup the implementation status of a key. Returns `None` if the
/// key is not in the catalog at all (caller should reject as
/// `BadRequest` rather than 501 — an unknown key is a different shape
/// of failure from a known-but-unimplemented one).
pub fn lookup(key: &str) -> Option<HandlerStatus> {
    CATALOG
        .iter()
        .find(|entry| entry.key == key)
        .map(|entry| entry.status)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn every_catalog_key_has_a_cost_row() {
        // The runtime catalog is the executable mirror of the cost
        // table — no charge without a handler, no handler without a
        // charge. The standalone snapshot test in
        // `services/media-sets-service/tests/cost_model_matches_table.rs`
        // enforces the symmetric direction (every cost row must have
        // a catalog entry).
        for entry in CATALOG {
            assert!(
                observability::rate_per_gb(entry.key).is_some(),
                "catalog key `{}` is not in observability::COST_TABLE",
                entry.key
            );
        }
    }

    #[test]
    fn native_handlers_cover_the_image_quick_wins() {
        for must_be_native in [
            "thumbnail", "resize", "resize_within_bounding_box", "rotate", "crop", "grayscale",
        ] {
            assert!(
                matches!(lookup(must_be_native), Some(HandlerStatus::Native)),
                "expected `{must_be_native}` to be a native (image-crate) handler"
            );
        }
    }
}
