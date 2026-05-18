// Package handlers serves the ai-sink query surface over HTTP. The
// underlying contract mirrors the AuditService shape from
// proto/audit/v1/audit.proto, adapted to the AiEventEnvelope fields.
package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/repo"
)

// MaxPageSize caps QueryEvents.page_size server-side.
const MaxPageSize = 1000

// DefaultPageSize is used when the request omits page_size.
const DefaultPageSize = 100

// Handlers is the ai-sink HTTP handler set.
type Handlers struct {
	Repo *repo.Repo
}

// AiEventJSON is the JSON shape returned by Query/Export. Field names
// match the envelope wire format (snake_case); `at` is rendered as
// RFC3339 (the envelope stores epoch micros).
type AiEventJSON struct {
	EventID       string          `json:"event_id"`
	At            time.Time       `json:"at"`
	Kind          string          `json:"kind"`
	RunID         string          `json:"run_id,omitempty"`
	TraceID       string          `json:"trace_id,omitempty"`
	Producer      string          `json:"producer"`
	SchemaVersion uint32          `json:"schema_version"`
	Payload       json.RawMessage `json:"payload"`
}

func rowToJSON(r repo.AiEventRow) AiEventJSON {
	payload := r.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	out := AiEventJSON{
		EventID:       r.EventID.String(),
		At:            r.At.UTC(),
		Kind:          r.Kind,
		Producer:      r.Producer,
		SchemaVersion: r.SchemaVersion,
		Payload:       payload,
	}
	if r.RunID != nil {
		out.RunID = r.RunID.String()
	}
	if r.TraceID != nil {
		out.TraceID = *r.TraceID
	}
	return out
}

// QueryEventsResponse mirrors the wire surface of audit's
// QueryEventsResponse, adapted to AI events.
type QueryEventsResponse struct {
	Events     []AiEventJSON `json:"events"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// QueryEvents is GET /api/v1/ai/events.
//
// Filters are AND-combined. Empty filter returns the most-recent rows.
// Pagination uses the opaque `cursor` query param emitted in
// `next_cursor`.
func (h *Handlers) QueryEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter, err := buildFilter(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	size := DefaultPageSize
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "page_size must be a positive integer")
			return
		}
		size = n
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}

	var after *repo.Cursor
	if raw := q.Get("cursor"); raw != "" {
		c, err := decodeCursor(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		after = &c
	}

	rows, next, err := h.Repo.Query(r.Context(), filter, size, after)
	if err != nil {
		slog.Error("query ai events", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	out := QueryEventsResponse{Events: make([]AiEventJSON, 0, len(rows))}
	for i := range rows {
		out.Events = append(out.Events, rowToJSON(rows[i]))
	}
	if next != nil {
		out.NextCursor = encodeCursor(*next)
	}
	writeJSON(w, http.StatusOK, out)
}

// ExportEvents is GET /api/v1/ai/events/export — streams NDJSON.
func (h *Handlers) ExportEvents(w http.ResponseWriter, r *http.Request) {
	filter, err := buildFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	err = h.Repo.Stream(r.Context(), filter, func(row repo.AiEventRow) error {
		if err := enc.Encode(rowToJSON(row)); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		slog.Error("export ai events", slog.String("error", err.Error()))
	}
}

// RecordEventRequest is the JSON body accepted by RecordEvent.
type RecordEventRequest struct {
	Envelope json.RawMessage `json:"envelope,omitempty"`
}

// RecordEventResponse mirrors proto RecordEventResponse.
type RecordEventResponse struct {
	EventID string `json:"event_id"`
}

// RecordEvent is POST /api/v1/ai/events. Synchronously persists one
// envelope. Production callers should publish to ai.events.v1 via Kafka
// instead; this endpoint is the write-through escape hatch for
// low-volume system callers and tests.
func (h *Handlers) RecordEvent(w http.ResponseWriter, r *http.Request) {
	var body RecordEventRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if len(body.Envelope) == 0 {
		writeError(w, http.StatusBadRequest, "envelope is required")
		return
	}
	env, err := envelope.Decode(body.Envelope)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid envelope: "+err.Error())
		return
	}
	if _, ok := envelope.TargetTable(env.Kind); !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown kind %q", env.Kind))
		return
	}
	if err := h.Repo.Insert(r.Context(), env); err != nil {
		slog.Error("record ai event", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "record failed")
		return
	}
	writeJSON(w, http.StatusOK, RecordEventResponse{EventID: env.EventID.String()})
}

// ─── helpers ───────────────────────────────────────────────────────

func buildFilter(q map[string][]string) (repo.QueryFilter, error) {
	get := func(key string) string {
		if vals, ok := q[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}
	filter := repo.QueryFilter{
		Kind:     get("kind"),
		TraceID:  get("trace_id"),
		Producer: get("producer"),
	}
	if raw := get("run_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return repo.QueryFilter{}, fmt.Errorf("invalid run_id: %w", err)
		}
		filter.RunID = &id
	}
	if from, ok, err := parseTime(get("from")); err != nil {
		return repo.QueryFilter{}, fmt.Errorf("invalid from: %w", err)
	} else if ok {
		filter.From = &from
	}
	if to, ok, err := parseTime(get("to")); err != nil {
		return repo.QueryFilter{}, fmt.Errorf("invalid to: %w", err)
	} else if ok {
		filter.To = &to
	}
	return filter, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseTime(raw string) (time.Time, bool, error) {
	if raw == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t.UTC(), true, nil
	}
	t, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return t.UTC(), true, nil
	}
	return time.Time{}, false, errors.New("expected RFC3339 timestamp")
}

func encodeCursor(c repo.Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (repo.Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return repo.Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	var c repo.Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return repo.Cursor{}, fmt.Errorf("unmarshal cursor: %w", err)
	}
	if c.EventID == uuid.Nil || c.At.IsZero() {
		return repo.Cursor{}, errors.New("cursor missing fields")
	}
	return c, nil
}
