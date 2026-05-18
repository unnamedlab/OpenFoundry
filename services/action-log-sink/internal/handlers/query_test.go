package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/repo"
)

// TestCursorRoundTrip pins the opaque base64 encoding so a next_cursor
// handed to clients round-trips back as a Cursor unchanged.
func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	c := repo.Cursor{AppliedAtMs: 1_700_000_000_000, EventID: "evt-42"}
	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if got != c {
		t.Errorf("round-trip cursor mismatch: got %#v want %#v", got, c)
	}
}

// TestDecodeCursorRejectsGarbage protects the API from forged cursors:
// non-base64, non-JSON, and missing-field inputs all collapse to a
// single error path.
func TestDecodeCursorRejectsGarbage(t *testing.T) {
	t.Parallel()
	cases := []string{
		"not-base64-???",
		"YWJj",            // base64("abc") — invalid JSON
		"e30",             // base64("{}") — missing fields
		"eyJlIjoiYmFkIn0", // base64({"e":"bad"}) — missing applied_at_ms
	}
	for _, raw := range cases {
		if _, err := decodeCursor(raw); err == nil {
			t.Errorf("expected decode error for %q", raw)
		}
	}
}

// TestRecordEventRejectsEmptyEnvelope verifies the write-through path
// rejects requests with no body without touching the repo.
func TestRecordEventRejectsEmptyEnvelope(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest("POST", "/api/v1/action-log/events", nil)
	rec := httptest.NewRecorder()
	h.RecordEvent(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestQueryEventsRejectsBadPageSize covers the validator on the read
// path: page_size must be a positive int when present.
func TestQueryEventsRejectsBadPageSize(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest("GET", "/api/v1/action-log/events?page_size=-3", nil)
	rec := httptest.NewRecorder()
	h.QueryEvents(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestQueryEventsRejectsBadTime locks the time-parse error path so a
// caller doesn't get a 500 for a malformed `from`.
func TestQueryEventsRejectsBadTime(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest("GET", "/api/v1/action-log/events?from=yesterday", nil)
	rec := httptest.NewRecorder()
	h.QueryEvents(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

// TestQueryEventsRejectsBadCursor verifies invalid cursor payloads get
// rejected before the repo is touched.
func TestQueryEventsRejectsBadCursor(t *testing.T) {
	t.Parallel()
	h := &Handlers{}
	req := httptest.NewRequest("GET", "/api/v1/action-log/events?cursor=not-base64-!!!", nil)
	rec := httptest.NewRecorder()
	h.QueryEvents(rec, req)
	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}
