//go:build integration

// Integration tests for the ai-sink Postgres path.
//
// Two scenarios, both gated by `-tags=integration`:
//
//   - TestInsertBatchAndQuery — seeds the four envelope kinds and
//     exercises filter/cursor pagination via the handler.
//   - TestRecordEventRoundTripJSON — write-through via RecordEvent and
//     read-back via QueryEvents proves the JSON contract.
//
// Both tests run under `go test -tags=integration -race ./services/ai-sink/...`.
package repo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/repo"
)

func mkEnvelope(t *testing.T, kind envelope.AiEventKind, runID *uuid.UUID, traceID *string, producer string, at time.Time) envelope.AiEventEnvelope {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"kind": string(kind), "producer": producer})
	require.NoError(t, err)
	return envelope.AiEventEnvelope{
		EventID:       uuid.New(),
		At:            at.UnixMicro(),
		Kind:          kind,
		RunID:         runID,
		TraceID:       traceID,
		Producer:      producer,
		SchemaVersion: 1,
		Payload:       payload,
	}
}

// TestInsertBatchAndQuery exercises the persistence + query path: seed
// rows for each kind + per-run-id, then walk a small page across the
// handler to confirm cursor pagination + filter wiring.
func TestInsertBatchAndQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))
	store := &repo.Repo{Pool: pg.Pool}

	runID := uuid.New()
	traceID := "trace-target"
	otherTraceID := "trace-other"
	base := time.Now().UTC().Add(-time.Hour)

	batch := make([]envelope.AiEventEnvelope, 0, 80)
	for i := 0; i < 50; i++ {
		batch = append(batch, mkEnvelope(t, envelope.KindPrompt, &runID, &traceID, "openai", base.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < 15; i++ {
		batch = append(batch, mkEnvelope(t, envelope.KindResponse, nil, &otherTraceID, "anthropic", base.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < 10; i++ {
		batch = append(batch, mkEnvelope(t, envelope.KindEvaluation, nil, nil, "internal", base.Add(time.Duration(i)*time.Second)))
	}
	for i := 0; i < 5; i++ {
		batch = append(batch, mkEnvelope(t, envelope.KindTrace, nil, nil, "tracer", base.Add(time.Duration(i)*time.Second)))
	}

	inserted, err := store.InsertBatch(ctx, batch)
	require.NoError(t, err)
	require.Equal(t, len(batch), inserted)

	// Replay the first event to confirm ON CONFLICT DO NOTHING.
	dupInserted, err := store.InsertBatch(ctx, batch[:1])
	require.NoError(t, err)
	require.Equal(t, 0, dupInserted, "replay must be absorbed by ON CONFLICT DO NOTHING")

	h := &handlers.Handlers{Repo: store}

	gathered := make([]string, 0, 50)
	cursor := ""
	pages := 0
	for {
		pages++
		url := fmt.Sprintf("/api/v1/ai/events?run_id=%s&page_size=20", runID.String())
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, url, nil)
		h.QueryEvents(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "page %d body: %s", pages, rec.Body.String())

		var resp handlers.QueryEventsResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.NotEmpty(t, resp.Events, "page %d returned 0 events", pages)
		for _, e := range resp.Events {
			gathered = append(gathered, e.EventID)
			assert.Equal(t, runID.String(), e.RunID)
			assert.Equal(t, "prompt", e.Kind)
		}
		if resp.NextCursor == "" {
			break
		}
		cursor = resp.NextCursor
		require.LessOrEqual(t, pages, 5, "pagination did not terminate")
	}
	assert.Equal(t, 50, len(gathered))

	// Deduplicate to confirm no row was returned twice across pages.
	seen := make(map[string]struct{}, 50)
	for _, id := range gathered {
		_, dup := seen[id]
		assert.False(t, dup, "duplicate event id across pages: %s", id)
		seen[id] = struct{}{}
	}

	// Kind filter restricts to the right discriminator only.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ai/events?kind=evaluation&page_size=200", nil)
	h.QueryEvents(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var evalResp handlers.QueryEventsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &evalResp))
	require.Len(t, evalResp.Events, 10)
	for _, e := range evalResp.Events {
		assert.Equal(t, "evaluation", e.Kind)
	}
}

// TestRecordEventRoundTripJSON exercises the write-through endpoint and
// asserts that the round-trip preserves run_id + trace_id + payload.
func TestRecordEventRoundTripJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, pg.Pool))
	h := &handlers.Handlers{Repo: &repo.Repo{Pool: pg.Pool}}

	runID := uuid.New()
	traceID := "trace-record"
	payload := json.RawMessage(`{"prompt":"hello"}`)
	env := envelope.AiEventEnvelope{
		EventID:       uuid.New(),
		At:            time.Now().UnixMicro(),
		Kind:          envelope.KindPrompt,
		RunID:         &runID,
		TraceID:       &traceID,
		Producer:      "openai",
		SchemaVersion: 1,
		Payload:       payload,
	}
	envBytes, err := json.Marshal(env)
	require.NoError(t, err)

	body := `{"envelope":` + string(envBytes) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.RecordEvent(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "record event failed: %s", rec.Body.String())

	var rresp handlers.RecordEventResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rresp))
	require.Equal(t, env.EventID.String(), rresp.EventID)

	// Read back via QueryEvents and confirm round-trip.
	qrec := httptest.NewRecorder()
	qreq := httptest.NewRequest(http.MethodGet, "/api/v1/ai/events?run_id="+runID.String(), nil)
	h.QueryEvents(qrec, qreq)
	require.Equal(t, http.StatusOK, qrec.Code)
	var qresp handlers.QueryEventsResponse
	require.NoError(t, json.Unmarshal(qrec.Body.Bytes(), &qresp))
	require.Len(t, qresp.Events, 1)
	got := qresp.Events[0]
	assert.Equal(t, env.EventID.String(), got.EventID)
	assert.Equal(t, runID.String(), got.RunID)
	assert.Equal(t, traceID, got.TraceID)
	assert.Equal(t, "openai", got.Producer)
	assert.JSONEq(t, string(payload), string(got.Payload))
}
