// Package costmodel mirrors the Rust libs/observability/src/cost_model.rs
// Foundry compute-seconds cost table for media-set transformations.
//
// Pinned line-by-line against the table in
// `Data formats/Media sets (unstructured data)/Media usage costs and limits.md`
// ("Transformations" section). The numbers below are the Foundry-published
// `compute-seconds per GB processed` rates; media-transform-runtime-service
// consults this package for every access-pattern invocation, multiplies by
// the input size in GB, and emits the result into:
//
//   - media_compute_seconds_total{transformation, schema} (Prometheus
//     counter declared in media-sets-service).
//   - The media_set.access_pattern_invoked audit event.
//
// Centralising the table here means a Foundry doc update touches one
// file and a single snapshot test enforces drift never sneaks past CI.
package costmodel

// SchemaKind discriminates the Foundry media-set schema. Wire form
// matches the proto enum (SCREAMING_SNAKE_CASE) used everywhere else
// in the platform.
type SchemaKind string

const (
	SchemaAll         SchemaKind = "ALL"
	SchemaImage       SchemaKind = "IMAGE"
	SchemaAudio       SchemaKind = "AUDIO"
	SchemaVideo       SchemaKind = "VIDEO"
	SchemaDocument    SchemaKind = "DOCUMENT"
	SchemaSpreadsheet SchemaKind = "SPREADSHEET"
)

// CostEntry is a single row of the published cost table.
//
// Key is the canonical wire-form transformation name shared across
// the cost meter, the audit envelope access_pattern field, and the
// transformation Prometheus label. DisplayName reproduces the Foundry
// doc verbatim so the Usage UI surfaces the same caption.
type CostEntry struct {
	Key                 string     `json:"key"`
	DisplayName         string     `json:"display_name"`
	Schema              SchemaKind `json:"schema"`
	ComputeSecondsPerGB uint32     `json:"compute_seconds_per_gb"`
}

// DownloadStream is the Foundry "All" row — applies to download /
// streaming of bytes regardless of media schema.
var DownloadStream = CostEntry{
	Key:                 "download_stream",
	DisplayName:         "Download / stream",
	Schema:              SchemaAll,
	ComputeSecondsPerGB: 2,
}

// CostTable mirrors the Rust COST_TABLE. Order matches the doc
// top-to-bottom so a screen-by-screen review against the upstream
// rendering catches a missing or reordered row instantly.
//
// DO NOT REORDER without updating the parity tests on both sides.
var CostTable = []CostEntry{
	// ── All ───────────────────────────────────────────────────────
	DownloadStream,
	// ── Images ────────────────────────────────────────────────────
	{Key: "generate_pdf", DisplayName: "Generate PDF", Schema: SchemaImage, ComputeSecondsPerGB: 40},
	{Key: "thumbnail", DisplayName: "Thumbnail (alias of Resize)", Schema: SchemaImage, ComputeSecondsPerGB: 40},
	{Key: "resize", DisplayName: "Resize", Schema: SchemaImage, ComputeSecondsPerGB: 40},
	{Key: "resize_within_bounding_box", DisplayName: "Resize within bounding box", Schema: SchemaImage, ComputeSecondsPerGB: 40},
	{Key: "rotate", DisplayName: "Rotate", Schema: SchemaImage, ComputeSecondsPerGB: 40},
	{Key: "adjust_contrast", DisplayName: "Adjust contrast", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "annotate", DisplayName: "Annotate", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "crop", DisplayName: "Crop / chip", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "encryption_decryption", DisplayName: "Encryption / decryption", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "geo_tile", DisplayName: "Geo tile", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "grayscale", DisplayName: "Grayscale", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "render_dicom_image_layer", DisplayName: "Render DICOM image layer", Schema: SchemaImage, ComputeSecondsPerGB: 75},
	{Key: "ocr", DisplayName: "Extract text (OCR)", Schema: SchemaImage, ComputeSecondsPerGB: 275},
	{Key: "embedding", DisplayName: "Generate embedding", Schema: SchemaImage, ComputeSecondsPerGB: 275},
	{Key: "ocr_v2", DisplayName: "Extract text v2 (OCR)", Schema: SchemaImage, ComputeSecondsPerGB: 275},
	{Key: "extract_layout_aware_v2", DisplayName: "Extract layout aware v2", Schema: SchemaImage, ComputeSecondsPerGB: 275},
	// ── Audio ─────────────────────────────────────────────────────
	{Key: "chunk", DisplayName: "Chunk", Schema: SchemaAudio, ComputeSecondsPerGB: 75},
	{Key: "channel_select", DisplayName: "Select channel", Schema: SchemaAudio, ComputeSecondsPerGB: 75},
	{Key: "hls_stream", DisplayName: "Stream with HLS", Schema: SchemaAudio, ComputeSecondsPerGB: 75},
	{Key: "transcode", DisplayName: "Transcode", Schema: SchemaAudio, ComputeSecondsPerGB: 75},
	{Key: "waveform", DisplayName: "Waveform generation", Schema: SchemaAudio, ComputeSecondsPerGB: 75},
	{Key: "transcription", DisplayName: "Transcription", Schema: SchemaAudio, ComputeSecondsPerGB: 275},
	// ── Video ─────────────────────────────────────────────────────
	{Key: "scene_frames_timestamps", DisplayName: "Get timestamps for scene frames", Schema: SchemaVideo, ComputeSecondsPerGB: 40},
	{Key: "extract_audio", DisplayName: "Extract audio", Schema: SchemaVideo, ComputeSecondsPerGB: 75},
	{Key: "extract_frames", DisplayName: "Extract frames at timestamp", Schema: SchemaVideo, ComputeSecondsPerGB: 75},
	{Key: "video_chunk", DisplayName: "Chunk", Schema: SchemaVideo, ComputeSecondsPerGB: 275},
	{Key: "scene_frames_all", DisplayName: "Extract all scene frames", Schema: SchemaVideo, ComputeSecondsPerGB: 275},
	{Key: "video_hls_stream", DisplayName: "Stream with HLS", Schema: SchemaVideo, ComputeSecondsPerGB: 275},
	{Key: "video_transcode", DisplayName: "Transcode", Schema: SchemaVideo, ComputeSecondsPerGB: 275},
	// ── Documents ─────────────────────────────────────────────────
	{Key: "pdf_page_dimensions", DisplayName: "Get PDF page dimensions", Schema: SchemaDocument, ComputeSecondsPerGB: 40},
	{Key: "render_page", DisplayName: "Render page as image", Schema: SchemaDocument, ComputeSecondsPerGB: 40},
	{Key: "render_page_bbox", DisplayName: "Render page as image within bounding box", Schema: SchemaDocument, ComputeSecondsPerGB: 40},
	{Key: "extract_text_raw", DisplayName: "Extract all text (raw)", Schema: SchemaDocument, ComputeSecondsPerGB: 75},
	{Key: "extract_form_fields", DisplayName: "Extract form fields", Schema: SchemaDocument, ComputeSecondsPerGB: 75},
	{Key: "extract_toc", DisplayName: "Extract table of contents", Schema: SchemaDocument, ComputeSecondsPerGB: 75},
	{Key: "extract_text_pages_raw", DisplayName: "Extract text from pages to array (raw)", Schema: SchemaDocument, ComputeSecondsPerGB: 75},
	{Key: "extract_text_on_page_raw", DisplayName: "Extract text on page (raw)", Schema: SchemaDocument, ComputeSecondsPerGB: 75},
	{Key: "slice_pdf_range", DisplayName: "Slice PDF range", Schema: SchemaDocument, ComputeSecondsPerGB: 75},
	{Key: "doc_ocr", DisplayName: "Extract text (OCR)", Schema: SchemaDocument, ComputeSecondsPerGB: 275},
	{Key: "extract_text_pages_ocr", DisplayName: "Extract text from pages to array (OCR)", Schema: SchemaDocument, ComputeSecondsPerGB: 275},
	{Key: "layout_aware_v2", DisplayName: "Extract layout aware text (raw / OCR)", Schema: SchemaDocument, ComputeSecondsPerGB: 275},
	{Key: "vlm_extract", DisplayName: "Extract text using VLM", Schema: SchemaDocument, ComputeSecondsPerGB: 275},
	// ── Spreadsheets ──────────────────────────────────────────────
	{Key: "render_sheet", DisplayName: "Convert sheet to JSON", Schema: SchemaSpreadsheet, ComputeSecondsPerGB: 275},
}

// RatePerGB returns the compute-seconds-per-GB rate for a transformation
// key, or (0, false) if the key is not in the published table — callers
// must refuse to charge for an off-table transformation rather than
// guess. The runtime catalog is the single source of truth for which
// keys are billable.
func RatePerGB(key string) (uint32, bool) {
	for i := range CostTable {
		if CostTable[i].Key == key {
			return CostTable[i].ComputeSecondsPerGB, true
		}
	}
	return 0, false
}

// ChargeComputeSeconds returns the compute-seconds charged for processing
// `bytes` through the transformation identified by `key`. Rounds **up**
// to the nearest whole compute-second so a 1-byte invocation still
// charges at least one compute-second. Returns (0, false) if `key` is
// not in the table.
func ChargeComputeSeconds(key string, bytes uint64) (uint64, bool) {
	rate, ok := RatePerGB(key)
	if !ok {
		return 0, false
	}
	const gb uint64 = 1024 * 1024 * 1024
	rate64 := uint64(rate)
	// Saturating multiply matches the Rust impl — clamp to MaxUint64 on
	// overflow so a multi-exabyte input still lands a finite charge.
	var prod uint64
	if rate64 != 0 && bytes > (^uint64(0))/rate64 {
		prod = ^uint64(0)
	} else {
		prod = rate64 * bytes
	}
	charged := (prod + gb - 1) / gb
	if bytes == 0 {
		return 0, true
	}
	if charged < 1 {
		charged = 1
	}
	return charged, true
}

// Entry returns the row for a key, including its display name and
// schema. Backs the /usage REST surface and the Usage UI tab.
func Entry(key string) (CostEntry, bool) {
	for i := range CostTable {
		if CostTable[i].Key == key {
			return CostTable[i], true
		}
	}
	return CostEntry{}, false
}

// EntriesFor returns every transformation belonging to a given schema
// plus the All rows. Backs the /usage JSON breakdown so the UI can
// populate the per-kind chart without knowing the table layout.
func EntriesFor(schema SchemaKind) []CostEntry {
	out := make([]CostEntry, 0, len(CostTable))
	for i := range CostTable {
		if CostTable[i].Schema == schema || CostTable[i].Schema == SchemaAll {
			out = append(out, CostTable[i])
		}
	}
	return out
}
