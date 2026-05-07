package audittrail

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func TestMiddlewarePassesResponseThroughUnchanged(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := MiddlewareWithLogger(captureLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("pong"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "pong" {
		t.Fatalf("body: got %q want %q", rec.Body.String(), "pong")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("content-type passthrough broken: %q", got)
	}
}

func TestMiddlewareEmitsAuditLogRecord(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := MiddlewareWithLogger(captureLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/widgets", nil))

	var entry map[string]any
	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&entry); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	want := map[string]any{
		"msg":         "request handled",
		"level":       "INFO",
		"category":    "audit",
		"http_method": "POST",
		"http_path":   "/api/v1/widgets",
		"http_status": float64(http.StatusCreated),
	}
	for k, v := range want {
		if entry[k] != v {
			t.Errorf("attr %s: got %v (%T) want %v", k, entry[k], entry[k], v)
		}
	}
	if _, ok := entry["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms missing or not numeric: %v", entry["duration_ms"])
	}
}

func TestMiddlewareDefaultsToStatus200WhenHandlerOmitsWriteHeader(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := MiddlewareWithLogger(captureLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	var entry map[string]any
	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&entry); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if got := entry["http_status"]; got != float64(http.StatusOK) {
		t.Fatalf("default status: got %v want 200", got)
	}
}

func TestMiddlewareCapturesErrorStatus(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	h := MiddlewareWithLogger(captureLogger(&buf))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oops", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status passthrough broken: %d", rec.Code)
	}

	var entry map[string]any
	if err := json.NewDecoder(strings.NewReader(buf.String())).Decode(&entry); err != nil {
		t.Fatalf("decode log line: %v", err)
	}
	if got := entry["http_status"]; got != float64(http.StatusInternalServerError) {
		t.Errorf("captured status: got %v want 500", got)
	}
}
