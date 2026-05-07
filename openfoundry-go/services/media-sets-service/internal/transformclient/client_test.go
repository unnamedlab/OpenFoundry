package transformclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformOKEcho(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/transform", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body TransformRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "thumbnail", body.Kind)
		assert.Equal(t, "image/png", body.MimeType)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransformResponse{
			Status:         StatusOK,
			Kind:           body.Kind,
			OutputMimeType: body.MimeType,
			ComputeSeconds: 7,
		})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	resp, err := c.Transform(context.Background(), TransformRequest{
		Kind:        "thumbnail",
		MimeType:    "image/png",
		Schema:      "IMAGE",
		BytesBase64: "AAAA",
	})
	require.NoError(t, err)
	assert.Equal(t, StatusOK, resp.Status)
	assert.Equal(t, uint64(7), resp.ComputeSeconds)
}

func TestTransformNotImplementedNoErrorButStatusFlagged(t *testing.T) {
	t.Parallel()
	reason := "external binary `tesseract` is not wired yet"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TransformResponse{
			Status:         StatusNotImplemented,
			Kind:           "ocr",
			OutputMimeType: "image/png",
			Reason:         &reason,
		})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	resp, err := c.Transform(context.Background(), TransformRequest{Kind: "ocr"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, StatusNotImplemented, resp.Status)
	require.NotNil(t, resp.Reason)
	assert.Contains(t, *resp.Reason, "tesseract")
}

func TestTransform400SurfacedAsErrorEnvelope(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"unknown transformation kind `+"`bogus`"+`","code":"MEDIA_TRANSFORM_UNKNOWN_KIND"}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	_, err := c.Transform(context.Background(), TransformRequest{Kind: "bogus"})
	require.Error(t, err)

	env, ok := AsErrorEnvelope(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, env.StatusCode)
	assert.Equal(t, "MEDIA_TRANSFORM_UNKNOWN_KIND", env.Code)
	assert.Contains(t, env.Message, "unknown transformation kind")
}

func TestTransformContextCancel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Block long enough to let the context cancel — we set a body
		// to avoid a connection-reset race on shutdown.
		_, _ = io.WriteString(w, `{"status":"OK","kind":"thumbnail","output_mime_type":"image/png","compute_seconds":0}`)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := c.Transform(ctx, TransformRequest{Kind: "thumbnail"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "context canceled") ||
		strings.Contains(err.Error(), "context"), "expected context error, got %q", err.Error())
}
