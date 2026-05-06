package catalog

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerStatusJSONShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   HandlerStatus
		want string
	}{
		{"native", HandlerStatus{Kind: StatusNative}, `{"kind":"native"}`},
		{"external", HandlerStatus{Kind: StatusExternal, Binary: "ffmpeg"}, `{"kind":"external","binary":"ffmpeg"}`},
		{"not_implemented", HandlerStatus{Kind: StatusNotImplemented, Reason: "x"}, `{"kind":"not_implemented","reason":"x"}`},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(c.in)
			require.NoError(t, err)
			assert.JSONEq(t, c.want, string(b))

			var got HandlerStatus
			require.NoError(t, json.Unmarshal([]byte(c.want), &got))
			assert.Equal(t, c.in, got)
		})
	}
}

func TestCatalogContainsAllRustKeys(t *testing.T) {
	t.Parallel()
	expectedKeys := []string{
		// Image
		"thumbnail", "resize", "resize_within_bounding_box", "rotate", "crop", "grayscale",
		"geo_tile", "render_dicom_image_layer", "ocr", "embedding",
		// Audio
		"chunk", "channel_select", "hls_stream", "transcode", "waveform", "transcription",
		// Video
		"scene_frames_timestamps", "extract_audio", "extract_frames", "video_chunk",
		"scene_frames_all", "video_hls_stream", "video_transcode",
		// Document
		"pdf_page_dimensions", "render_page", "extract_text_raw", "extract_form_fields",
		"extract_toc", "slice_pdf_range", "doc_ocr", "layout_aware_v2", "vlm_extract",
		// Spreadsheet
		"render_sheet",
	}
	for _, k := range expectedKeys {
		_, ok := Lookup(k)
		assert.True(t, ok, "catalog must contain key %q", k)
	}
	assert.Equal(t, len(expectedKeys), len(Catalog), "catalog length must match Rust source")
}

func TestImageHandlerStatusInFoundation(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"thumbnail", "resize", "resize_within_bounding_box", "rotate", "crop", "grayscale"} {
		s, ok := Lookup(k)
		require.True(t, ok)
		assert.Equal(t, StatusNotImplemented, s.Kind, "%s should be not_implemented in Go foundation", k)
		assert.Contains(t, s.Reason, "follow-up slice", "reason should reference the follow-up slice")
	}
}

func TestExternalBinariesPreserved(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"ocr":                      "tesseract",
		"render_dicom_image_layer": "dcmtk",
		"chunk":                    "ffmpeg",
		"transcode":                "ffmpeg",
		"pdf_page_dimensions":      "pdfium",
		"extract_text_raw":         "pdftotext",
		"slice_pdf_range":          "qpdf",
	}
	for k, want := range cases {
		s, ok := Lookup(k)
		require.True(t, ok, "missing key %q", k)
		assert.Equal(t, StatusExternal, s.Kind, "%s should be external", k)
		assert.Equal(t, want, s.Binary, "%s binary mismatch", k)
	}
}

func TestUnknownKeyLookup(t *testing.T) {
	t.Parallel()
	_, ok := Lookup("not-a-real-kind")
	assert.False(t, ok)
}
