// Package handlers serves the action-log query surface over HTTP.
//
// There is no proto service for action_log (the proto/audit package
// defines AuditService for audit-sink). The wire shape mirrors the
// ActionEnvelope contract emitted by the publisher
// (services/action-log-sink/internal/envelope/envelope.go) so the JSON
// surfaced here is symmetric with what lands on the Kafka topic.
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

	"github.com/go-chi/chi/v5"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/repo"
)

// MaxPageSize / DefaultPageSize bound the QueryEvents pagination. Same
// limits the audit-sink uses so a single client SDK can drive both.
const (
	MaxPageSize     = 1000
	DefaultPageSize = 100
)

// Handlers is the action-log-sink HTTP handler set. `Repo` may be nil
// when the service is running in Iceberg-only mode — in that case the
// /api/v1/action-log/* routes are not mounted (see server.New).
type Handlers struct {
	Repo *repo.Repo
}

// ActionEventJSON is the JSON shape returned by Query/Get/Export.
// Field names match envelope.ActionEnvelope; extra metadata
// (`kafka_ts`, `created_at`) is appended by the sink.
type ActionEventJSON struct {
	EventID              string          `json:"event_id"`
	ActionTypeID         string          `json:"action_type_id"`
	ActionName           string          `json:"action_name"`
	ObjectTypeID         string          `json:"object_type_id"`
	ObjectID             string          `json:"object_id,omitempty"`
	Tenant               string          `json:"tenant"`
	ActorSub             string          `json:"actor_sub"`
	ActorEmail           string          `json:"actor_email,omitempty"`
	OrganizationID       string          `json:"organization_id,omitempty"`
	Status               string          `json:"status"`
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	PreviousState        json.RawMessage `json:"previous_state,omitempty"`
	NewState             json.RawMessage `json:"new_state,omitempty"`
	TargetClassification string          `json:"target_classification,omitempty"`
	AppliedAtMs          int64           `json:"applied_at_ms"`
	KafkaTS              time.Time       `json:"kafka_ts"`
	CreatedAt            time.Time       `json:"created_at"`
}

func rowToJSON(r repo.ActionEventRow) ActionEventJSON {
	return ActionEventJSON{
		EventID:              r.EventID,
		ActionTypeID:         r.ActionTypeID,
		ActionName:           r.ActionName,
		ObjectTypeID:         r.ObjectTypeID,
		ObjectID:             r.ObjectID,
		Tenant:               r.Tenant,
		ActorSub:             r.ActorSub,
		ActorEmail:           r.ActorEmail,
		OrganizationID:       r.OrganizationID,
		Status:               r.Status,
		Parameters:           r.Parameters,
		PreviousState:        r.PreviousState,
		NewState:             r.NewState,
		TargetClassification: r.TargetClassification,
		AppliedAtMs:          r.AppliedAtMs,
		KafkaTS:              r.KafkaTS.UTC(),
		CreatedAt:            r.CreatedAt.UTC(),
	}
}

// QueryEventsResponse is the JSON wrapper for paginated query results.
type QueryEventsResponse struct {
	Events     []ActionEventJSON `json:"events"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// QueryEvents is GET /api/v1/action-log/events.
//
// Filters are AND-combined. Empty filter returns the most-recent rows.
// Pagination uses the opaque `cursor` query param emitted in
// `next_cursor`.
func (h *Handlers) QueryEvents(w http.ResponseWriter, r *http.Request) {
	filter, ok := parseFilter(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
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
		slog.Error("query action-log events", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "query failed")
		return
	}
	out := QueryEventsResponse{Events: make([]ActionEventJSON, 0, len(rows))}
	for i := range rows {
		out.Events = append(out.Events, rowToJSON(rows[i]))
	}
	if next != nil {
		out.NextCursor = encodeCursor(*next)
	}
	writeJSON(w, http.StatusOK, out)
}

// GetEvent is GET /api/v1/action-log/events/{event_id}.
func (h *Handlers) GetEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "event_id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "event_id is required")
		return
	}
	row, err := h.Repo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			writeError(w, http.StatusNotFound, "event not found")
			return
		}
		slog.Error("get action-log event", slog.String("error", err.Error()), slog.String("event_id", id))
		writeError(w, http.StatusInternalServerError, "get failed")
		return
	}
	writeJSON(w, http.StatusOK, rowToJSON(row))
}

// ExportEvents is GET /api/v1/action-log/events/export — streams NDJSON.
func (h *Handlers) ExportEvents(w http.ResponseWriter, r *http.Request) {
	filter, ok := parseFilter(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	err := h.Repo.Stream(r.Context(), filter, func(row repo.ActionEventRow) error {
		if err := enc.Encode(rowToJSON(row)); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if err != nil {
		slog.Error("export action-log events", slog.String("error", err.Error()))
	}
}

// RecordEventRequest is the JSON body accepted by RecordEvent. The
// payload is a full ActionEnvelope as published on the Kafka topic.
type RecordEventRequest struct {
	Envelope json.RawMessage `json:"envelope,omitempty"`
}

// RecordEventResponse mirrors the audit-sink shape so the two sinks
// surface identical JSON contracts for the write-through path.
type RecordEventResponse struct {
	EventID string `json:"event_id"`
}

// RecordEvent is POST /api/v1/action-log/events. Synchronous
// write-through escape hatch for low-volume system callers + tests.
// Production callers should publish to ontology.actions.applied.v1
// instead and let the runtime drain the topic into Postgres + Iceberg.
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
	if err := h.Repo.Insert(r.Context(), env); err != nil {
		slog.Error("record action-log event", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "record failed")
		return
	}
	writeJSON(w, http.StatusOK, RecordEventResponse{EventID: env.EventID})
}

// ─── helpers ───────────────────────────────────────────────────────

func parseFilter(w http.ResponseWriter, r *http.Request) (repo.QueryFilter, bool) {
	q := r.URL.Query()
	filter := repo.QueryFilter{
		Tenant:       q.Get("tenant"),
		ActorSub:     q.Get("actor_sub"),
		ObjectTypeID: q.Get("object_type_id"),
		ObjectID:     q.Get("object_id"),
		ActionName:   q.Get("action_name"),
		Status:       q.Get("status"),
	}
	if from, ok, err := parseTime(q.Get("from")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid from: "+err.Error())
		return repo.QueryFilter{}, false
	} else if ok {
		filter.From = &from
	}
	if to, ok, err := parseTime(q.Get("to")); err != nil {
		writeError(w, http.StatusBadRequest, "invalid to: "+err.Error())
		return repo.QueryFilter{}, false
	} else if ok {
		filter.To = &to
	}
	return filter, true
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
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), true, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
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
	if c.EventID == "" || c.AppliedAtMs == 0 {
		return repo.Cursor{}, errors.New("cursor missing fields")
	}
	return c, nil
}
