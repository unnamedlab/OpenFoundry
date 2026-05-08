// Unit tests for the Coordinator state machine. Uses in-memory
// fakes for JobStore / Scanner / Publisher / Idempotency so the
// run-to-terminal path is exercised without Postgres, Cassandra, or
// Kafka. The Kafka testcontainer integration test lives in
// coordinator_integration_test.go (build tag: integration).
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/libs/idempotency"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/event"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/scan"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/state"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/topics"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ─────────────────────────── Fakes ───────────────────────────

type memJobStore struct {
	mu      sync.Mutex
	rows    map[uuid.UUID]*repo.JobRecord
	advance []advanceCall
}

type advanceCall struct {
	id             uuid.UUID
	resumeToken    *string
	scannedDelta   int64
	publishedDelta int64
}

func newMemJobStore() *memJobStore {
	return &memJobStore{rows: map[uuid.UUID]*repo.JobRecord{}}
}

func (m *memJobStore) UpsertQueued(_ context.Context, id uuid.UUID, tenant, typeID string, page int32) (repo.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.rows[id]; ok {
		return *existing, nil
	}
	now := time.Now()
	rec := &repo.JobRecord{
		ID: id, TenantID: tenant, TypeID: typeID, Status: state.StatusQueued,
		PageSize: page, StartedAt: now, UpdatedAt: now,
	}
	m.rows[id] = rec
	return *rec, nil
}

func (m *memJobStore) Load(_ context.Context, id uuid.UUID) (repo.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.rows[id]
	if !ok {
		return repo.JobRecord{}, repo.ErrJobNotFound
	}
	return *rec, nil
}

func (m *memJobStore) ListResumable(_ context.Context) ([]repo.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []repo.JobRecord{}
	for _, rec := range m.rows {
		if rec.Status == state.StatusQueued || rec.Status == state.StatusRunning {
			out = append(out, *rec)
		}
	}
	return out, nil
}

func (m *memJobStore) MarkRunning(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.rows[id]
	if !ok {
		return repo.ErrJobNotFound
	}
	if rec.Status.IsTerminal() {
		return &state.IllegalTransitionError{From: rec.Status, To: state.StatusRunning}
	}
	rec.Status = state.StatusRunning
	rec.Error = nil
	rec.UpdatedAt = time.Now()
	return nil
}

func (m *memJobStore) Advance(_ context.Context, id uuid.UUID, nextResumeToken *string, scannedDelta, publishedDelta int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.rows[id]
	if !ok {
		return repo.ErrJobNotFound
	}
	if rec.Status != state.StatusRunning {
		return &state.IllegalTransitionError{From: rec.Status, To: state.StatusRunning}
	}
	rec.ResumeToken = nextResumeToken
	rec.Scanned += scannedDelta
	rec.Published += publishedDelta
	rec.UpdatedAt = time.Now()
	m.advance = append(m.advance, advanceCall{id: id, resumeToken: nextResumeToken, scannedDelta: scannedDelta, publishedDelta: publishedDelta})
	return nil
}

func (m *memJobStore) MarkTerminal(_ context.Context, id uuid.UUID, next state.JobStatus, errMessage *string) (repo.JobRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.rows[id]
	if !ok {
		return repo.JobRecord{}, repo.ErrJobNotFound
	}
	if !next.IsTerminal() {
		return repo.JobRecord{}, &state.IllegalTransitionError{From: rec.Status, To: next}
	}
	if rec.Status == next {
		return *rec, nil
	}
	if err := rec.Status.EnsureTransition(next); err != nil {
		return repo.JobRecord{}, err
	}
	rec.Status = next
	rec.Error = errMessage
	now := time.Now()
	rec.UpdatedAt = now
	rec.CompletedAt = &now
	return *rec, nil
}

// scriptedScanner returns the supplied pages in order. After the last
// page, NextToken is nil so the coordinator marks the job complete.
type scriptedScanner struct {
	pages []scan.PageOutcome
	idx   int
	err   error
}

func (s *scriptedScanner) ScanPage(_ context.Context, _ string, _ *string, _ int32, _ *string) (*scan.PageOutcome, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.idx >= len(s.pages) {
		empty := scan.PageOutcome{}
		return &empty, nil
	}
	page := s.pages[s.idx]
	s.idx++
	return &page, nil
}

// memPublisher records every Publish call. errOn(topic) injects an
// error so we can exercise the publish-error → mark_failed path.
type memPublisher struct {
	mu       sync.Mutex
	records  []recordedPublish
	failOn   string
	failErr  error
	flushErr error
}

type recordedPublish struct {
	Topic   string
	Key     []byte
	Payload []byte
	Lineage *databus.OpenLineageHeaders
}

func (p *memPublisher) Publish(_ context.Context, topic string, key, payload []byte, lineage *databus.OpenLineageHeaders) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failOn == topic {
		return p.failErr
	}
	p.records = append(p.records, recordedPublish{Topic: topic, Key: append([]byte(nil), key...), Payload: append([]byte(nil), payload...), Lineage: lineage})
	return nil
}

func (p *memPublisher) Flush(context.Context) error { return p.flushErr }
func (p *memPublisher) Close() error                { return nil }

func (p *memPublisher) calls() []recordedPublish {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]recordedPublish, len(p.records))
	copy(out, p.records)
	return out
}

// ─────────────────────────── Throttle ───────────────────────────

func TestThrottlePerPublishSleepUnbounded(t *testing.T) {
	tt := Throttle{}
	assert.Equal(t, time.Duration(0), tt.perPublishSleep())
}

func TestThrottlePerPublishSleepRoundsUpward(t *testing.T) {
	// 100 ⇒ 10ms exact; 3 ⇒ 334ms (≤3/s); 7 ⇒ 143ms (≤7/s).
	cases := []struct {
		max  uint32
		want time.Duration
	}{
		{100, 10 * time.Millisecond},
		{3, 334 * time.Millisecond},
		{7, 143 * time.Millisecond},
		{1, 1000 * time.Millisecond},
	}
	for _, c := range cases {
		got := Throttle{MaxBatchesPerSecond: c.max}.perPublishSleep()
		assert.Equalf(t, c.want, got, "max=%d", c.max)
	}
}

func TestThrottleFromEnvDefaults(t *testing.T) {
	t.Setenv("OF_REINDEX_PAGE_INTERVAL_MS", "")
	t.Setenv("OF_REINDEX_MAX_BATCHES_PER_SECOND", "")
	tt, err := ThrottleFromEnv()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), tt.PageInterval)
	assert.Equal(t, uint32(0), tt.MaxBatchesPerSecond)
}

func TestThrottleFromEnvParses(t *testing.T) {
	t.Setenv("OF_REINDEX_PAGE_INTERVAL_MS", "250")
	t.Setenv("OF_REINDEX_MAX_BATCHES_PER_SECOND", "120")
	tt, err := ThrottleFromEnv()
	require.NoError(t, err)
	assert.Equal(t, 250*time.Millisecond, tt.PageInterval)
	assert.Equal(t, uint32(120), tt.MaxBatchesPerSecond)
}

func TestThrottleFromEnvRejectsGarbage(t *testing.T) {
	t.Setenv("OF_REINDEX_PAGE_INTERVAL_MS", "not-an-int")
	_, err := ThrottleFromEnv()
	require.Error(t, err)
	var inv *InvalidEnvError
	require.True(t, errors.As(err, &inv))
	assert.Equal(t, "OF_REINDEX_PAGE_INTERVAL_MS", inv.Key)
}

// ─────────────────────────── Metrics ───────────────────────────

func TestNewMetricsRegistersAllSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	require.NotNil(t, m)

	// Bump every series so Gather returns each family.
	m.RequestsTotal.WithLabelValues("completed").Inc()
	m.BatchesTotal.WithLabelValues("published").Inc()
	m.RecordsTotal.WithLabelValues("published").Add(3)
	m.JobsInFlight.Set(1)

	families, err := reg.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(families))
	for _, fam := range families {
		names = append(names, fam.GetName())
	}
	assert.Contains(t, names, "reindex_coordinator_requests_total")
	assert.Contains(t, names, "reindex_coordinator_batches_total")
	assert.Contains(t, names, "reindex_coordinator_records_total")
	assert.Contains(t, names, "reindex_coordinator_jobs_in_flight")
}

// ─────────────────────────── State machine ───────────────────────────

func newTestCoordinator(t *testing.T, jobs JobStore, sc Scanner, pub databus.Publisher) *Coordinator {
	t.Helper()
	c := NewCoordinator(jobs, idempotency.NewMemStore(), sc, pub, NewMetrics(prometheus.NewRegistry()), Throttle{}, "openfoundry-test", quietLogger())
	c.sleep = func(time.Duration) {} // avoid real sleeps in tests
	return c
}

func mustScanPage(records []scan.ReindexRecord, scanned int, nextToken *string) scan.PageOutcome {
	return scan.PageOutcome{Records: records, Scanned: scanned, NextToken: nextToken}
}

func TestRunJobMultiPagePublishesAndCompletes(t *testing.T) {
	jobs := newMemJobStore()
	tok := "page-1"
	scn := &scriptedScanner{pages: []scan.PageOutcome{
		mustScanPage([]scan.ReindexRecord{
			{Tenant: "tenant-a", ID: "obj-1", TypeID: "users", Version: 1, Payload: json.RawMessage(`{}`)},
			{Tenant: "tenant-a", ID: "obj-2", TypeID: "users", Version: 1, Payload: json.RawMessage(`{}`)},
		}, 2, &tok),
		mustScanPage([]scan.ReindexRecord{
			{Tenant: "tenant-a", ID: "obj-3", TypeID: "users", Version: 1, Payload: json.RawMessage(`{}`)},
		}, 1, nil),
	}}
	pub := &memPublisher{}
	c := newTestCoordinator(t, jobs, scn, pub)

	jobID := event.DeriveJobID("tenant-a", strPtr("users"))
	_, err := jobs.UpsertQueued(context.Background(), jobID, "tenant-a", "users", 1000)
	require.NoError(t, err)

	final, err := c.RunJob(context.Background(), jobID, strPtr("req-1"))
	require.NoError(t, err)
	assert.Equal(t, state.StatusCompleted, final.Status)
	assert.Equal(t, int64(3), final.Scanned)
	assert.Equal(t, int64(3), final.Published)
	require.NotNil(t, final.CompletedAt)

	calls := pub.calls()
	// 3 batch records + 1 completed event = 4 publishes.
	require.Len(t, calls, 4)
	require.Equal(t, topics.OntologyReindexCompletedV1, calls[len(calls)-1].Topic)
	for _, c := range calls[:3] {
		assert.Equal(t, topics.OntologyReindexV1, c.Topic)
		require.NotNil(t, c.Lineage)
		assert.Equal(t, "openfoundry-test", c.Lineage.Namespace)
		assert.True(t, strings.HasPrefix(c.Lineage.JobName, "reindex/tenant-a/users"))
	}

	var completed event.ReindexCompletedV1
	require.NoError(t, json.Unmarshal(calls[len(calls)-1].Payload, &completed))
	assert.Equal(t, jobID, completed.JobID)
	assert.Equal(t, "completed", completed.Status)
	require.NotNil(t, completed.RequestID)
	assert.Equal(t, "req-1", *completed.RequestID)
	assert.Equal(t, int64(3), completed.Scanned)
	assert.Equal(t, int64(3), completed.Published)
}

func TestRunJobSkipsPublishOnIdempotencyDedup(t *testing.T) {
	jobs := newMemJobStore()
	scn := &scriptedScanner{pages: []scan.PageOutcome{
		mustScanPage([]scan.ReindexRecord{
			{Tenant: "tenant-a", ID: "obj-1", TypeID: "users", Version: 1, Payload: json.RawMessage(`{}`)},
		}, 1, nil),
	}}
	pub := &memPublisher{}
	idem := idempotency.NewMemStore()
	c := NewCoordinator(jobs, idem, scn, pub, NewMetrics(prometheus.NewRegistry()), Throttle{}, "ns", quietLogger())
	c.sleep = func(time.Duration) {}

	// Pre-record the first-page event_id so the coordinator's
	// CheckAndRecord returns AlreadyProcessed and the publisher
	// must NOT see this batch on the data-plane topic.
	jobID := event.DeriveJobID("tenant-a", strPtr("users"))
	preID := event.DeriveBatchEventID("tenant-a", strPtr("users"), "")
	_, err := idem.CheckAndRecord(context.Background(), preID)
	require.NoError(t, err)

	_, err = jobs.UpsertQueued(context.Background(), jobID, "tenant-a", "users", 1000)
	require.NoError(t, err)
	final, err := c.RunJob(context.Background(), jobID, nil)
	require.NoError(t, err)
	assert.Equal(t, state.StatusCompleted, final.Status)
	assert.Equal(t, int64(0), final.Published, "deduped batch must NOT advance published counter")
	assert.Equal(t, int64(1), final.Scanned, "scanned counter still rolls forward")

	calls := pub.calls()
	// Only the completed event is on the wire — the batch was deduped.
	require.Len(t, calls, 1)
	assert.Equal(t, topics.OntologyReindexCompletedV1, calls[0].Topic)
}

func TestRunJobMarksFailedOnScanError(t *testing.T) {
	jobs := newMemJobStore()
	scn := &scriptedScanner{err: &scan.ScanError{Kind: "driver", Reason: "boom"}}
	pub := &memPublisher{}
	c := newTestCoordinator(t, jobs, scn, pub)

	jobID := event.DeriveJobID("tenant-x", nil)
	_, err := jobs.UpsertQueued(context.Background(), jobID, "tenant-x", "", 1000)
	require.NoError(t, err)

	_, err = c.RunJob(context.Background(), jobID, nil)
	require.Error(t, err)

	rec, _ := jobs.Load(context.Background(), jobID)
	assert.Equal(t, state.StatusFailed, rec.Status)
	require.NotNil(t, rec.Error)
	assert.Contains(t, *rec.Error, "boom")

	// Failed job still emits one completed event (status=failed).
	calls := pub.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, topics.OntologyReindexCompletedV1, calls[0].Topic)
	var completed event.ReindexCompletedV1
	require.NoError(t, json.Unmarshal(calls[0].Payload, &completed))
	assert.Equal(t, "failed", completed.Status)
}

func TestRunJobSkipsTerminalDuplicate(t *testing.T) {
	jobs := newMemJobStore()
	scn := &scriptedScanner{}
	pub := &memPublisher{}
	c := newTestCoordinator(t, jobs, scn, pub)

	jobID := event.DeriveJobID("tenant-y", strPtr("assets"))
	_, err := jobs.UpsertQueued(context.Background(), jobID, "tenant-y", "assets", 1000)
	require.NoError(t, err)
	require.NoError(t, jobs.MarkRunning(context.Background(), jobID))
	_, err = jobs.MarkTerminal(context.Background(), jobID, state.StatusCompleted, nil)
	require.NoError(t, err)

	rec, err := c.RunJob(context.Background(), jobID, nil)
	require.NoError(t, err, "duplicate requested.v1 for terminal job must NOT raise")
	assert.Equal(t, state.StatusCompleted, rec.Status)
	assert.Empty(t, pub.calls(), "no publishes for a duplicate of a terminal job")
}

func TestProcessRequestMessageCommittableOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    string
	}{
		{"empty", nil, "empty_payload"},
		{"malformed-json", []byte(`{`), "decode_error"},
		{"missing-tenant", []byte(`{"type_id":"x"}`), "decode_error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jobs := newMemJobStore()
			scn := &scriptedScanner{}
			pub := &memPublisher{}
			coord := newTestCoordinator(t, jobs, scn, pub)
			msg := &databus.DataMessage{Topic: topics.OntologyReindexRequestedV1, Value: c.payload}
			label, err := processRequestMessage(context.Background(), coord, msg)
			require.NoError(t, err)
			assert.Equal(t, c.want, label)
			assert.Empty(t, pub.calls(), "skipped messages must not publish")
		})
	}
}

func TestProcessRequestMessageHappyPath(t *testing.T) {
	jobs := newMemJobStore()
	scn := &scriptedScanner{pages: []scan.PageOutcome{mustScanPage(nil, 0, nil)}}
	pub := &memPublisher{}
	coord := newTestCoordinator(t, jobs, scn, pub)

	body, err := json.Marshal(event.ReindexRequestedV1{TenantID: "tenant-z", TypeID: strPtr("users")})
	require.NoError(t, err)
	msg := &databus.DataMessage{Topic: topics.OntologyReindexRequestedV1, Value: body}

	label, err := processRequestMessage(context.Background(), coord, msg)
	require.NoError(t, err)
	assert.Equal(t, "completed", label)

	jobID := event.DeriveJobID("tenant-z", strPtr("users"))
	rec, err := jobs.Load(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, state.StatusCompleted, rec.Status)
}

func TestProcessRequestMessageAlreadyTerminal(t *testing.T) {
	jobs := newMemJobStore()
	scn := &scriptedScanner{}
	pub := &memPublisher{}
	coord := newTestCoordinator(t, jobs, scn, pub)

	jobID := event.DeriveJobID("tenant-w", nil)
	_, err := jobs.UpsertQueued(context.Background(), jobID, "tenant-w", "", 1000)
	require.NoError(t, err)
	require.NoError(t, jobs.MarkRunning(context.Background(), jobID))
	_, err = jobs.MarkTerminal(context.Background(), jobID, state.StatusCancelled, nil)
	require.NoError(t, err)

	body, err := json.Marshal(event.ReindexRequestedV1{TenantID: "tenant-w"})
	require.NoError(t, err)
	msg := &databus.DataMessage{Topic: topics.OntologyReindexRequestedV1, Value: body}

	label, err := processRequestMessage(context.Background(), coord, msg)
	require.NoError(t, err)
	assert.Equal(t, "already_terminal", label)
	assert.Empty(t, pub.calls())
}

// ─────────────────────────── Subscribe topics ───────────────────────────

func TestSubscribeTopicsPinned(t *testing.T) {
	require.Equal(t, []string{topics.OntologyReindexRequestedV1}, SubscribeTopics)
}

func TestConsumerGroupPinned(t *testing.T) {
	require.Equal(t, "reindex-coordinator-service", ConsumerGroup)
}

// ─────────────────────────── helpers ───────────────────────────

func strPtr(s string) *string { return &s }
