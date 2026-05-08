package flink

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain"
)

// TestExtractKPIsSumsThroughputAndDividesBackpressure ports
// metrics_poller::tests::extract_kpis_sums_throughput_and_divides_backpressure.
func TestExtractKPIsSumsThroughputAndDividesBackpressure(t *testing.T) {
	raw := []any{
		map[string]any{"id": "numRecordsOutPerSecond", "value": "120.0"},
		map[string]any{"id": "numRecordsOutPerSecond", "value": "30.0"},
		map[string]any{"id": "numRecordsInPerSecond", "value": "200.0"},
		map[string]any{"id": "numLateRecordsDropped", "value": "5"},
		map[string]any{"id": "backPressuredTimeMsPerSecond", "value": "250"},
	}
	m := ExtractKPIs(raw)
	if m.OutputEvents != 150 {
		t.Fatalf("OutputEvents = %d, want 150", m.OutputEvents)
	}
	if m.InputEvents != 200 {
		t.Fatalf("InputEvents = %d, want 200", m.InputEvents)
	}
	if m.DroppedEvents != 5 {
		t.Fatalf("DroppedEvents = %d, want 5", m.DroppedEvents)
	}
	if math.Abs(float64(m.BackpressureRatio)-0.25) > 1e-6 {
		t.Fatalf("BackpressureRatio = %f, want 0.25", m.BackpressureRatio)
	}
	if m.ThroughputPerSecond != 150 {
		t.Fatalf("ThroughputPerSecond = %f, want 150", m.ThroughputPerSecond)
	}
}

// TestExtractKPIsReturnsZeroForEmptyResponse ports
// metrics_poller::tests::extract_kpis_returns_zero_for_empty_response.
func TestExtractKPIsReturnsZeroForEmptyResponse(t *testing.T) {
	m := ExtractKPIs([]any{})
	if m.OutputEvents != 0 {
		t.Fatalf("OutputEvents = %d, want 0", m.OutputEvents)
	}
	if m.ThroughputPerSecond != 0 {
		t.Fatalf("ThroughputPerSecond = %f, want 0", m.ThroughputPerSecond)
	}
}

// TestExtractKPIsRejectsNonArray covers the Rust `arr = raw.as_array().cloned().unwrap_or_default()`
// fallback — the Go translation must also yield a zero metrics struct
// when the JSON body is shaped wrong.
func TestExtractKPIsRejectsNonArray(t *testing.T) {
	m := ExtractKPIs(map[string]any{"jobs": []any{}})
	if m != (domain.TopologyRunMetrics{}) {
		t.Fatalf("expected zero-valued metrics, got %+v", m)
	}
}

func TestPollOnceUsesProvidedJobID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/job-7/metrics" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		want := "numRecordsInPerSecond,numRecordsOutPerSecond,backPressuredTimeMsPerSecond,numLateRecordsDropped"
		if got := r.URL.Query().Get("get"); got != want {
			t.Errorf("get = %q, want %q", got, want)
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": "numRecordsOutPerSecond", "value": "12.0"},
		})
	}))
	defer srv.Close()
	jobID := "job-7"
	got, err := PollOnce(context.Background(), srv.Client(), srv.URL, &jobID)
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if got.OutputEvents != 12 {
		t.Fatalf("OutputEvents = %d, want 12", got.OutputEvents)
	}
}

func TestPollOnceDiscoversRunningJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []any{
					map[string]any{"id": "old", "status": "FINISHED"},
					map[string]any{"id": "live", "status": "RUNNING"},
				},
			})
		case "/jobs/live/metrics":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "numRecordsInPerSecond", "value": "5"},
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	got, err := PollOnce(context.Background(), srv.Client(), srv.URL, nil)
	if err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
	if got.InputEvents != 5 {
		t.Fatalf("InputEvents = %d, want 5", got.InputEvents)
	}
}

func TestPollOnceFallsBackToFirstJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []any{
					map[string]any{"id": "first", "status": "FAILED"},
					map[string]any{"id": "second", "status": "FINISHED"},
				},
			})
		case "/jobs/first/metrics":
			_ = json.NewEncoder(w).Encode([]any{})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()
	if _, err := PollOnce(context.Background(), srv.Client(), srv.URL, nil); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
}

func TestPollOnceRejectsMissingJobsArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"unexpected": true})
	}))
	defer srv.Close()
	_, err := PollOnce(context.Background(), srv.Client(), srv.URL, nil)
	var pe *PollerError
	if !errors.As(err, &pe) || pe.Kind != PollerErrBody {
		t.Fatalf("expected PollerErrBody, got %v", err)
	}
}

func TestPollOnceSurfacesNon2xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	jobID := "j"
	_, err := PollOnce(context.Background(), srv.Client(), srv.URL, &jobID)
	var pe *PollerError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PollerError, got %v", err)
	}
	if pe.Kind != PollerErrStatus || pe.Status != http.StatusBadGateway {
		t.Fatalf("expected status 502 PollerError, got %+v", pe)
	}
}

type recordingLister struct{ topologies []FlinkTopologyTarget }

func (r *recordingLister) ListFlinkTopologies(_ context.Context) ([]FlinkTopologyTarget, error) {
	return r.topologies, nil
}

type recordingRecorder struct {
	mu   sync.Mutex
	runs []TopologyRun
	done chan struct{}
}

func (r *recordingRecorder) RecordTopologyRun(_ context.Context, _ uuid.UUID, run TopologyRun) error {
	r.mu.Lock()
	r.runs = append(r.runs, run)
	r.mu.Unlock()
	if r.done != nil {
		select {
		case r.done <- struct{}{}:
		default:
		}
	}
	return nil
}

func (r *recordingRecorder) snapshot() []TopologyRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TopologyRun, len(r.runs))
	copy(out, r.runs)
	return out
}

func TestSupervisorSpawnPersistsScrapedMetrics(t *testing.T) {
	topoID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/abc/metrics" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": "numRecordsOutPerSecond", "value": "8"},
			map[string]any{"id": "backPressuredTimeMsPerSecond", "value": "500"},
		})
	}))
	defer srv.Close()

	jobID := "abc"
	lister := &recordingLister{topologies: []FlinkTopologyTarget{{
		ID:                  topoID,
		FlinkDeploymentName: "dep",
		FlinkNamespace:      "ns",
		FlinkJobID:          &jobID,
	}}}
	rec := &recordingRecorder{done: make(chan struct{}, 1)}
	cfg := FlinkRuntimeConfig{
		MetricsPollIntervalMS: 1000, // run loop clamps anything below 1000 to 1000
		JobManagerURLTemplate: srv.URL,
	}
	frozen := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sup := NewMetricsPollerSupervisor(cfg, lister, rec, SupervisorOptions{
		Client: srv.Client(),
		Now:    func() time.Time { return frozen },
	})

	if err := sup.Spawn(context.Background()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(sup.Shutdown)

	select {
	case <-rec.done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for metrics tick")
	}

	runs := rec.snapshot()
	if len(runs) == 0 {
		t.Fatal("expected at least one persisted run")
	}
	first := runs[0]
	if first.Status != "running" {
		t.Fatalf("Status = %q, want running", first.Status)
	}
	if first.Metrics.OutputEvents != 8 {
		t.Fatalf("OutputEvents = %d, want 8", first.Metrics.OutputEvents)
	}
	if first.BackpressureSnapshot.Status != "throttled" {
		t.Fatalf("backpressure status = %q, want throttled", first.BackpressureSnapshot.Status)
	}
	if first.StateSnapshot.Backend != "flink-rocksdb" {
		t.Fatalf("state backend = %q, want flink-rocksdb", first.StateSnapshot.Backend)
	}
	if !first.StateSnapshot.LastCheckpointAt.Equal(frozen) {
		t.Fatalf("LastCheckpointAt = %v, want %v", first.StateSnapshot.LastCheckpointAt, frozen)
	}
}

func TestSupervisorSpawnRequiresLister(t *testing.T) {
	sup := NewMetricsPollerSupervisor(FlinkRuntimeConfig{}, nil, nil, SupervisorOptions{})
	if err := sup.Spawn(context.Background()); err == nil {
		t.Fatal("expected error when lister is nil")
	}
}

func TestBuildTopologyRunMarksOKWithoutBackpressure(t *testing.T) {
	id := uuid.New()
	frozen := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	run := buildTopologyRun(id, domain.TopologyRunMetrics{ThroughputPerSecond: 10}, frozen)
	if run.BackpressureSnapshot.Status != "ok" {
		t.Fatalf("Status = %q, want ok", run.BackpressureSnapshot.Status)
	}
	if run.StateSnapshot.Namespace != "topology/"+id.String() {
		t.Fatalf("Namespace = %q", run.StateSnapshot.Namespace)
	}
}
