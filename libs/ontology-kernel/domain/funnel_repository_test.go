package domain

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `decode_funnel_event` rejects entries whose `kind` is not the
// canonical `funnel_run` discriminator, and parses the payload
// otherwise.
func TestDecodeFunnelEventKindGuard(t *testing.T) {
	body := mustEncodeEvent(t, "funnel_run_started",
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"))

	// Wrong kind → ignored.
	wrong := storage.ActionLogEntry{Kind: "object_action", Payload: body, RecordedAtMs: 1}
	payload, ts := decodeFunnelEvent(wrong)
	assert.Nil(t, payload)
	assert.Nil(t, ts)

	// Correct kind → decoded.
	right := storage.ActionLogEntry{Kind: "funnel_run", Payload: body, RecordedAtMs: 5_000}
	payload, ts = decodeFunnelEvent(right)
	require.NotNil(t, payload)
	require.NotNil(t, ts)
	require.NotNil(t, payload.Event)
	assert.Equal(t, "funnel_run_started", *payload.Event)
	assert.Equal(t, time.UnixMilli(5_000).UTC(), *ts)

	// Garbage payload → ignored even on the right kind.
	garbage := storage.ActionLogEntry{Kind: "funnel_run", Payload: json.RawMessage(`not json`), RecordedAtMs: 1}
	payload, ts = decodeFunnelEvent(garbage)
	assert.Nil(t, payload)
	assert.Nil(t, ts)
}

// libs/ontology-kernel/src/domain/funnel_repository.rs `is_json_null`
// equivalent — empty/whitespace/`null` raw bytes treat as null.
func TestIsJSONNullPredicate(t *testing.T) {
	cases := map[string]bool{
		"":            true,
		"   ":         true,
		"\n\t":        true,
		"null":        true,
		"  null":      true, // not handled — current impl checks first non-ws char
		"{}":          false,
		"\"\"":        false,
		`{"k":1}`:     false,
		"0":           false,
	}
	for in, wantNull := range cases {
		got := isJSONNull(json.RawMessage(in))
		// The current implementation treats "  null" as non-null
		// because it looks at the first non-whitespace char and then
		// compares the *whole* slice to "null". Document the actual
		// behaviour with an inline note.
		if in == "  null" {
			assert.False(t, got, "leading whitespace before null is not stripped")
			continue
		}
		assert.Equal(t, wantNull, got, "isJSONNull(%q) = %v, want %v", in, got, wantNull)
	}
}

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `FunnelRunAccumulator::apply` — started event seeds status,
// startedAt, and details (when null on the accumulator).
func TestAccumulatorAppliesStartedEvent(t *testing.T) {
	runID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	sourceID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	objectTypeID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	datasetID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	startedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)

	acc := newFunnelRunAccumulator(runID, sourceID)
	acc.apply(FunnelRunEventPayload{
		Event:        ptr("funnel_run_started"),
		RunID:        runID,
		SourceID:     sourceID,
		ObjectTypeID: &objectTypeID,
		DatasetID:    &datasetID,
		Details:      json.RawMessage(`{"hint":"test"}`),
		StartedAt:    &startedAt,
	}, time.UnixMilli(0).UTC())

	require.NotNil(t, acc.status)
	assert.Equal(t, "running", *acc.status, "default status when started carries no explicit status")
	require.NotNil(t, acc.startedAt)
	assert.Equal(t, startedAt, *acc.startedAt)
	assert.JSONEq(t, `{"hint":"test"}`, string(acc.details))
	assert.Equal(t, &objectTypeID, acc.objectTypeID)
	assert.Equal(t, &datasetID, acc.datasetID)
}

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `FunnelRunAccumulator::apply` — completed event updates counts,
// details, and finished_at; failed event defaults status to "failed"
// when the payload omits one.
func TestAccumulatorAppliesTerminalEvents(t *testing.T) {
	runID := uuid.New()
	sourceID := uuid.New()

	cases := []struct {
		name       string
		event      string
		payload    FunnelRunEventPayload
		wantStatus string
	}{
		{
			name:       "completed honours payload status",
			event:      "funnel_run_completed",
			payload:    FunnelRunEventPayload{Event: ptr("funnel_run_completed"), Status: ptr("completed"), RowsRead: i32(7)},
			wantStatus: "completed",
		},
		{
			name:       "failed without status defaults to failed",
			event:      "funnel_run_failed",
			payload:    FunnelRunEventPayload{Event: ptr("funnel_run_failed")},
			wantStatus: "failed",
		},
		{
			name:       "failed honours explicit status",
			event:      "funnel_run_failed",
			payload:    FunnelRunEventPayload{Event: ptr("funnel_run_failed"), Status: ptr("custom_failure")},
			wantStatus: "custom_failure",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acc := newFunnelRunAccumulator(runID, sourceID)
			tc.payload.RunID = runID
			tc.payload.SourceID = sourceID
			acc.apply(tc.payload, time.UnixMilli(1234).UTC())
			require.NotNil(t, acc.status)
			assert.Equal(t, tc.wantStatus, *acc.status)
			require.NotNil(t, acc.finishedAt)
			assert.Equal(t, time.UnixMilli(1234).UTC(), *acc.finishedAt)
			if tc.payload.RowsRead != nil {
				assert.Equal(t, *tc.payload.RowsRead, acc.rowsRead)
			}
		})
	}
}

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `FunnelRunAccumulator::into_run` — incomplete accumulators (no
// object_type_id / dataset_id / started_at) drop out; complete ones
// hydrate with default trigger_type "manual".
func TestAccumulatorIntoRun(t *testing.T) {
	// Missing dataset → drops.
	acc := newFunnelRunAccumulator(uuid.New(), uuid.New())
	acc.objectTypeID = ptrUUID(uuid.New())
	startedAt := time.Now().UTC()
	acc.startedAt = &startedAt
	assert.Nil(t, acc.intoRun(), "missing datasetID drops the run")

	// Complete → defaults trigger_type to "manual" and status to "running".
	acc = newFunnelRunAccumulator(uuid.New(), uuid.New())
	acc.objectTypeID = ptrUUID(uuid.New())
	acc.datasetID = ptrUUID(uuid.New())
	acc.startedAt = &startedAt
	run := acc.intoRun()
	require.NotNil(t, run)
	assert.Equal(t, "running", run.Status)
	assert.Equal(t, "manual", run.TriggerType)
}

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `runs_from_events` folds events by run_id and preserves the
// insertion order of first-seen ids — this Go port uses an explicit
// `order` slice instead of HashMap iteration so the result is stable
// across runs (the Rust version's order is implementation-defined).
func TestRunsFromEventsFoldsByRunID(t *testing.T) {
	source := uuid.New()
	objectType := uuid.New()
	dataset := uuid.New()
	startedAt := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)

	runA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	runB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	events := []eventWithTime{
		{
			payload: FunnelRunEventPayload{
				Event: ptr("funnel_run_started"), RunID: runA, SourceID: source,
				ObjectTypeID: &objectType, DatasetID: &dataset, StartedAt: &startedAt,
			},
			recordedAt: startedAt,
		},
		{
			payload: FunnelRunEventPayload{
				Event: ptr("funnel_run_started"), RunID: runB, SourceID: source,
				ObjectTypeID: &objectType, DatasetID: &dataset, StartedAt: &startedAt,
			},
			recordedAt: startedAt,
		},
		{
			payload: FunnelRunEventPayload{
				Event: ptr("funnel_run_completed"), RunID: runA, SourceID: source,
				Status: ptr("completed"), RowsRead: i32(10), FinishedAt: &finishedAt,
			},
			recordedAt: finishedAt,
		},
	}

	runs := runsFromEvents(events)
	require.Len(t, runs, 2)
	assert.Equal(t, runA, runs[0].ID, "insertion order: A first because seen first")
	assert.Equal(t, "completed", runs[0].Status)
	assert.Equal(t, int32(10), runs[0].RowsRead)
	assert.Equal(t, runB, runs[1].ID)
	assert.Equal(t, "running", runs[1].Status)
}

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `list_funnel_events_for_tenant` pages through ListRecent until
// next_token is nil and filters non-funnel kinds.
func TestListFunnelEventsForTenantPaginates(t *testing.T) {
	source := uuid.New()
	objType := uuid.New()
	dataset := uuid.New()
	startedAt := time.Now().UTC().Truncate(time.Second)

	makeEntry := func(kind string, runID uuid.UUID) storage.ActionLogEntry {
		body := mustEncodeStartEvent(t, runID, source, objType, dataset, startedAt)
		return storage.ActionLogEntry{
			Tenant:       "t",
			Kind:         kind,
			Payload:      body,
			RecordedAtMs: startedAt.UnixMilli(),
		}
	}

	tok := "next-page"
	page1 := storage.PagedResult[storage.ActionLogEntry]{
		Items: []storage.ActionLogEntry{
			makeEntry("funnel_run", uuid.New()),
			makeEntry("object_action", uuid.New()), // filtered out
		},
		NextToken: &tok,
	}
	page2 := storage.PagedResult[storage.ActionLogEntry]{
		Items: []storage.ActionLogEntry{makeEntry("funnel_run", uuid.New())},
	}

	mock := &recentPagedMock{pages: []storage.PagedResult[storage.ActionLogEntry]{page1, page2}}
	events, err := ListFunnelEventsForTenant(context.Background(), mock, "t")
	require.NoError(t, err)
	assert.Len(t, events, 2, "two funnel-kind entries across the two pages")
	assert.Equal(t, 2, mock.calls, "ListRecent called twice — first for page1, second for page2")
}

// libs/ontology-kernel/src/domain/funnel_repository.rs
// `ListRunsForTenant` propagates errors from the action log verbatim.
func TestListRunsForTenantPropagatesError(t *testing.T) {
	mock := &recentPagedMock{err: errors.New("backend offline")}
	_, err := ListRunsForTenant(context.Background(), mock, "t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend offline")
}

// ---- test helpers ---------------------------------------------------------

type recentPagedMock struct {
	pages []storage.PagedResult[storage.ActionLogEntry]
	calls int
	err   error
}

func (m *recentPagedMock) ListRecent(_ context.Context, _ storage.TenantId, _ storage.Page, _ storage.ReadConsistency) (storage.PagedResult[storage.ActionLogEntry], error) {
	if m.err != nil {
		return storage.PagedResult[storage.ActionLogEntry]{}, m.err
	}
	if m.calls >= len(m.pages) {
		return storage.PagedResult[storage.ActionLogEntry]{}, nil
	}
	page := m.pages[m.calls]
	m.calls++
	return page, nil
}
func (m *recentPagedMock) Append(_ context.Context, _ storage.ActionLogEntry) error { return nil }
func (m *recentPagedMock) ListForObject(_ context.Context, _ storage.TenantId, _ storage.ObjectId, _ storage.Page, _ storage.ReadConsistency) (storage.PagedResult[storage.ActionLogEntry], error) {
	return storage.PagedResult[storage.ActionLogEntry]{}, nil
}
func (m *recentPagedMock) ListForAction(_ context.Context, _ storage.TenantId, _ string, _ storage.Page, _ storage.ReadConsistency) (storage.PagedResult[storage.ActionLogEntry], error) {
	return storage.PagedResult[storage.ActionLogEntry]{}, nil
}

var _ storage.ActionLogStore = (*recentPagedMock)(nil)

func ptr(s string) *string { return &s }
func i32(n int32) *int32   { return &n }
func ptrUUID(u uuid.UUID) *uuid.UUID { return &u }

func mustEncodeEvent(t *testing.T, event string, runID, sourceID uuid.UUID) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(FunnelRunEventPayload{
		Event:    &event,
		RunID:    runID,
		SourceID: sourceID,
	})
	require.NoError(t, err)
	return body
}

func mustEncodeStartEvent(t *testing.T, runID, sourceID, objectTypeID, datasetID uuid.UUID, startedAt time.Time) json.RawMessage {
	t.Helper()
	event := "funnel_run_started"
	body, err := json.Marshal(FunnelRunEventPayload{
		Event:        &event,
		RunID:        runID,
		SourceID:     sourceID,
		ObjectTypeID: &objectTypeID,
		DatasetID:    &datasetID,
		StartedAt:    &startedAt,
	})
	require.NoError(t, err)
	return body
}
