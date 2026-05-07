package mediafunctions

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func handle(item string) ItemHandle {
	branch := "main"
	schema := "DOCUMENT"
	return ItemHandle{
		MediaSetRID:  "ri.foundry.main.media_set.fixtures",
		MediaItemRID: item,
		Branch:       &branch,
		Schema:       &schema,
	}
}

// Mirrors `read_raw_round_trips_scripted_bytes_and_records_the_call`.
func TestReadRawRoundTripsScriptedBytesAndRecordsTheCall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rt := NewMockRuntime()
	rt.PutRaw("doc-1", []byte("%PDF-1.4 raw bytes"))

	item := handle("doc-1")
	got, err := ReadRaw(ctx, rt, item)
	if err != nil {
		t.Fatalf("ReadRaw failed: %v", err)
	}
	if string(got) != "%PDF-1.4 raw bytes" {
		t.Fatalf("unexpected bytes: %q", got)
	}

	calls := rt.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "read_raw" {
		t.Fatalf("expected read_raw, got %s", calls[0].Method)
	}
	if calls[0].Item != item {
		t.Fatalf("MediaItemHandle did not reach runtime verbatim: %+v", calls[0].Item)
	}
}

// Mirrors `ocr_returns_scripted_text`.
func TestOCRReturnsScriptedText(t *testing.T) {
	t.Parallel()
	rt := NewMockRuntime()
	rt.PutOCR("photo-1", "Aircraft tail number AF-101")
	got, err := OCR(context.Background(), rt, handle("photo-1"))
	if err != nil {
		t.Fatalf("OCR failed: %v", err)
	}
	if got != "Aircraft tail number AF-101" {
		t.Fatalf("unexpected OCR text: %q", got)
	}
}

// Mirrors `extract_text_separate_from_ocr_path`: scripting `text` but
// not `ocr` proves extract_text does not fall back to the OCR script.
func TestExtractTextSeparateFromOCRPath(t *testing.T) {
	t.Parallel()
	rt := NewMockRuntime()
	rt.PutText("doc-1", "Heading\nBody paragraph")
	got, err := ExtractText(context.Background(), rt, handle("doc-1"))
	if err != nil {
		t.Fatalf("ExtractText failed: %v", err)
	}
	if got != "Heading\nBody paragraph" {
		t.Fatal("extract_text returned unexpected text")
	}
	if _, err := OCR(context.Background(), rt, handle("doc-1")); err == nil {
		t.Fatal("OCR must remain unscripted — paths must stay independent")
	}
}

// Mirrors `transcribe_audio_round_trips_segments`.
func TestTranscribeAudioRoundTripsSegments(t *testing.T) {
	t.Parallel()
	rt := NewMockRuntime()
	rt.PutTranscript("audio-1", Transcription{
		Text: "Welcome to the briefing.",
		Segments: []TranscriptSegment{
			{Start: 0.0, End: 1.5, Text: "Welcome"},
			{Start: 1.5, End: 3.2, Text: "to the briefing."},
		},
	})

	got, err := TranscribeAudio(context.Background(), rt, handle("audio-1"))
	if err != nil {
		t.Fatalf("TranscribeAudio failed: %v", err)
	}
	if got.Text != "Welcome to the briefing." {
		t.Fatalf("unexpected text: %q", got.Text)
	}
	if len(got.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(got.Segments))
	}
	if got.Segments[0].Text != "Welcome" {
		t.Fatalf("unexpected first segment: %q", got.Segments[0].Text)
	}
	if math.Abs(got.Segments[1].End-3.2) > 1e-9 {
		t.Fatalf("end timestamp drift: %f", got.Segments[1].End)
	}
}

// Mirrors `read_metadata_returns_a_json_blob`.
func TestReadMetadataReturnsAJSONBlob(t *testing.T) {
	t.Parallel()
	rt := NewMockRuntime()
	rt.PutMetadata("doc-1", json.RawMessage(`{
        "mime_type": "application/pdf",
        "size_bytes": 4096,
        "sha256": "abc123",
        "pages": 12
    }`))
	got, err := ReadMetadata(context.Background(), rt, handle("doc-1"))
	if err != nil {
		t.Fatalf("ReadMetadata failed: %v", err)
	}

	var meta map[string]any
	if err := json.Unmarshal(got, &meta); err != nil {
		t.Fatalf("metadata is not JSON: %v", err)
	}
	if meta["mime_type"] != "application/pdf" {
		t.Fatalf("mime_type wrong: %v", meta["mime_type"])
	}
	if meta["pages"].(float64) != 12 {
		t.Fatalf("pages wrong: %v", meta["pages"])
	}
}

// Mirrors `missing_item_surfaces_not_found`.
func TestMissingItemSurfacesNotFound(t *testing.T) {
	t.Parallel()
	rt := NewMockRuntime()
	_, err := ReadRaw(context.Background(), rt, handle("ghost"))
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected NotFound, got %v", err)
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Target != "ghost" {
		t.Fatalf("unexpected error shape: %+v", err)
	}
}

// Mirrors `each_function_dispatches_to_its_own_runtime_method`.
func TestEachFunctionDispatchesToItsOwnRuntimeMethod(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rt := NewMockRuntime()
	rt.PutRaw("a", []byte{}).
		PutOCR("a", "").
		PutText("a", "").
		PutTranscript("a", Transcription{}).
		PutMetadata("a", json.RawMessage(`{}`))

	if _, err := ReadRaw(ctx, rt, handle("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := OCR(ctx, rt, handle("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractText(ctx, rt, handle("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := TranscribeAudio(ctx, rt, handle("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMetadata(ctx, rt, handle("a")); err != nil {
		t.Fatal(err)
	}

	got := rt.Calls()
	want := []string{"read_raw", "ocr", "extract_text", "transcribe_audio", "read_metadata"}
	if len(got) != len(want) {
		t.Fatalf("expected %d calls, got %d", len(want), len(got))
	}
	for i, m := range want {
		if got[i].Method != m {
			t.Fatalf("call %d: want %s got %s", i, m, got[i].Method)
		}
	}
}

// Mirrors the inline lib unit test for the unscripted-OCR fallback.
func TestOCRSurfacesNotImplementedWhenUnscripted(t *testing.T) {
	t.Parallel()
	rt := NewMockRuntime()
	_, err := OCR(context.Background(), rt, handle("missing"))
	if err == nil {
		t.Fatal("expected NotImplemented error")
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != KindNotImplemented {
		t.Fatalf("expected NotImplemented, got %+v", err)
	}
}
