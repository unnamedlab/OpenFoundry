// Package catalog mirrors the Rust media-transform-runtime catalog
// verbatim. Keys MUST match a row in
// observability::cost_model::COST_TABLE on the Rust side; the
// Rust-side snapshot test enforces drift.
//
// HandlerStatus is JSON-serialised with `tag="kind"` + snake_case
// rename, matching the Rust serde encoding:
//
//	{"kind":"native"}
//	{"kind":"external","binary":"ffmpeg"}
//	{"kind":"not_implemented","reason":"…"}
package catalog

import (
	"encoding/json"
	"fmt"
)

// HandlerStatusKind discriminates the variant.
type HandlerStatusKind string

const (
	StatusNative         HandlerStatusKind = "native"
	StatusExternal       HandlerStatusKind = "external"
	StatusNotImplemented HandlerStatusKind = "not_implemented"
)

// HandlerStatus is the tagged-union catalog status.
type HandlerStatus struct {
	Kind   HandlerStatusKind
	Binary string // when Kind == StatusExternal
	Reason string // when Kind == StatusNotImplemented
}

// MarshalJSON keeps the Rust-side wire shape exactly:
//
//	{"kind":"native"}
//	{"kind":"external","binary":"ffmpeg"}
//	{"kind":"not_implemented","reason":"..."}
func (s HandlerStatus) MarshalJSON() ([]byte, error) {
	switch s.Kind {
	case StatusNative:
		return []byte(`{"kind":"native"}`), nil
	case StatusExternal:
		return json.Marshal(struct {
			Kind   HandlerStatusKind `json:"kind"`
			Binary string            `json:"binary"`
		}{Kind: s.Kind, Binary: s.Binary})
	case StatusNotImplemented:
		return json.Marshal(struct {
			Kind   HandlerStatusKind `json:"kind"`
			Reason string            `json:"reason"`
		}{Kind: s.Kind, Reason: s.Reason})
	default:
		return nil, fmt.Errorf("catalog: unknown HandlerStatusKind %q", s.Kind)
	}
}

// UnmarshalJSON parses the same shape, used by tests + downstream
// consumers that round-trip through the runtime's /catalog endpoint.
func (s *HandlerStatus) UnmarshalJSON(data []byte) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	kind, _ := m["kind"].(string)
	switch HandlerStatusKind(kind) {
	case StatusNative:
		s.Kind = StatusNative
	case StatusExternal:
		s.Kind = StatusExternal
		s.Binary, _ = m["binary"].(string)
	case StatusNotImplemented:
		s.Kind = StatusNotImplemented
		s.Reason, _ = m["reason"].(string)
	default:
		return fmt.Errorf("catalog: unknown kind %q", kind)
	}
	return nil
}

// CatalogEntry is `{key, status}`.
type CatalogEntry struct {
	Key    string        `json:"key"`
	Status HandlerStatus `json:"status"`
}

// Native / External / NotImplemented constructors keep the static table
// below readable. native() lands alongside the image-handler slice in
// internal/handlers (golang.org/x/image + disintegration/imaging +
// HugoSmits86/nativewebp port).
func native() HandlerStatus {
	return HandlerStatus{Kind: StatusNative}
}
func external(binary string) HandlerStatus {
	return HandlerStatus{Kind: StatusExternal, Binary: binary}
}
func notImplemented(reason string) HandlerStatus {
	return HandlerStatus{Kind: StatusNotImplemented, Reason: reason}
}

// Catalog is the port of the Rust CATALOG. Order matches the Rust
// source so downstream consumers render the same row order.
var Catalog = []CatalogEntry{
	// ── Image (pure-Go via stdlib image + disintegration/imaging) ──
	{Key: "thumbnail", Status: native()},
	{Key: "resize", Status: native()},
	{Key: "resize_within_bounding_box", Status: native()},
	{Key: "rotate", Status: native()},
	{Key: "crop", Status: native()},
	{Key: "grayscale", Status: native()},
	{Key: "geo_tile", Status: native()},
	{Key: "render_dicom_image_layer", Status: external("dcmtk")},
	{Key: "ocr", Status: external("tesseract")},
	{Key: "embedding", Status: notImplemented("Image embeddings depend on libs/ai-kernel which is not yet wired.")},

	// ── Audio (ffmpeg) ────────────────────────────────────────────
	{Key: "chunk", Status: external("ffmpeg")},
	{Key: "channel_select", Status: external("ffmpeg")},
	{Key: "hls_stream", Status: external("ffmpeg")},
	{Key: "transcode", Status: external("ffmpeg")},
	{Key: "waveform", Status: external("ffmpeg")},
	{Key: "transcription", Status: notImplemented("Transcription depends on libs/ai-kernel (Whisper / VLM) which is not yet wired.")},

	// ── Video (ffmpeg + HLS packager) ─────────────────────────────
	{Key: "scene_frames_timestamps", Status: external("ffmpeg")},
	{Key: "extract_audio", Status: external("ffmpeg")},
	{Key: "extract_frames", Status: external("ffmpeg")},
	{Key: "video_chunk", Status: external("ffmpeg")},
	{Key: "scene_frames_all", Status: external("ffmpeg")},
	{Key: "video_hls_stream", Status: external("ffmpeg")},
	{Key: "video_transcode", Status: external("ffmpeg")},

	// ── Document (pdfium / poppler / tesseract) ───────────────────
	{Key: "pdf_page_dimensions", Status: external("pdfium")},
	{Key: "render_page", Status: external("pdfium")},
	{Key: "extract_text_raw", Status: external("pdftotext")},
	{Key: "extract_form_fields", Status: external("pdfium")},
	{Key: "extract_toc", Status: external("pdfium")},
	{Key: "slice_pdf_range", Status: external("qpdf")},
	{Key: "doc_ocr", Status: external("tesseract")},
	{Key: "layout_aware_v2", Status: notImplemented("Layout-aware extraction depends on libs/ai-kernel which is not yet wired.")},
	{Key: "vlm_extract", Status: notImplemented("VLM extraction depends on libs/ai-kernel which is not yet wired.")},

	// ── Spreadsheet ───────────────────────────────────────────────
	{Key: "render_sheet", Status: native()},
}

// Lookup returns the status for a key, or (zero, false) if the key
// is not in the catalog at all (caller rejects with 400, not 501).
func Lookup(key string) (HandlerStatus, bool) {
	for _, e := range Catalog {
		if e.Key == key {
			return e.Status, true
		}
	}
	return HandlerStatus{}, false
}
