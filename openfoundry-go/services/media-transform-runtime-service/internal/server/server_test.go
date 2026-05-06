package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/runtime"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(BuildRouter(observability.NewMetrics()))
}

func TestHealthzPlainText(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	b, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "ok", string(b))
}

func TestCatalogListEndpoint(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/catalog")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var entries []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&entries))
	assert.GreaterOrEqual(t, len(entries), 32, "catalog should expose all Rust entries")
	first := entries[0]
	assert.Equal(t, "thumbnail", first["key"])
	status := first["status"].(map[string]any)
	assert.Equal(t, "not_implemented", status["kind"])
	assert.Contains(t, status["reason"].(string), "follow-up slice")
}

func TestCatalogEntryByKey(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/catalog/ocr")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var entry map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&entry))
	assert.Equal(t, "ocr", entry["key"])
	status := entry["status"].(map[string]any)
	assert.Equal(t, "external", status["kind"])
	assert.Equal(t, "tesseract", status["binary"])
}

func TestCatalogEntryUnknownKindIs400(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/catalog/not-a-kind")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, runtime.CodeUnknownKind, body["code"])
}

func TestTransformExternalReturnsNotImplementedEnvelope(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	body := `{"kind":"ocr","mime_type":"image/png","schema":"IMAGE","bytes_base64":""}`
	resp, err := http.Post(srv.URL+"/transform", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "NOT_IMPLEMENTED", out["status"])
	assert.Equal(t, "ocr", out["kind"])
	assert.Equal(t, "image/png", out["output_mime_type"])
	assert.Contains(t, out["reason"], "tesseract")
}

func TestTransformNotImplementedReturnsReason(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	// "geo_tile" is NotImplemented in both Rust and Go.
	body := `{"kind":"geo_tile","mime_type":"image/png","schema":"IMAGE","bytes_base64":""}`
	resp, err := http.Post(srv.URL+"/transform", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "NOT_IMPLEMENTED", out["status"])
	assert.Contains(t, out["reason"], "geospatial-intelligence-service follow-up")
}

func TestTransformImageKeyReturnsFoundationStub(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	// Some bytes (a 1×1 PNG) — the foundation runtime won't actually
	// dispatch to a native handler so the bytes are not validated; we
	// just ensure the envelope is right.
	body := `{
		"kind": "thumbnail",
		"mime_type": "image/png",
		"schema": "IMAGE",
		"bytes_base64": "` + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}) + `"
	}`
	resp, err := http.Post(srv.URL+"/transform", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "NOT_IMPLEMENTED", out["status"])
	assert.Equal(t, "thumbnail", out["kind"])
	assert.Contains(t, out["reason"], "follow-up slice")
}

func TestTransformUnknownKindIs400(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	body := `{"kind":"not-a-kind","mime_type":"image/png","schema":"IMAGE","bytes_base64":""}`
	resp, err := http.Post(srv.URL+"/transform", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body2 map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body2))
	assert.Equal(t, runtime.CodeUnknownKind, body2["code"])
}

func TestTransformBadJSONIs400(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/transform", "application/json", strings.NewReader("not-json"))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, runtime.CodeBadInput, body["code"])
}
