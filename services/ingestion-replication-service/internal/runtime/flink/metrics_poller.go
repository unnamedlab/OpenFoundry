package flink

// metrics_poller ports event_streaming::runtime::flink::metrics_poller.
//
// A single supervisor lists topologies with runtime_kind='flink' and a
// non-null flink_deployment_name, then spawns one polling goroutine per
// topology. Each goroutine hits the JobManager REST API every
// FlinkRuntimeConfig.MetricsPollIntervalMS and records a row with the
// canonical KPI vector via the injected RunRecorder.
//
// Mapped KPIs (matches Rust 1:1):
//
//   TopologyRunMetrics field   Flink metric
//   ------------------------   ----------------------------------------
//   InputEvents                numRecordsInPerSecond  × interval
//   OutputEvents               numRecordsOutPerSecond × interval
//   AvgLatencyMS               latency.mean (deferred — set to 0)
//   P95LatencyMS               latency.p95  (deferred — set to 0)
//   ThroughputPerSecond        numRecordsOutPerSecond
//   DroppedEvents              numLateRecordsDropped
//   BackpressureRatio          backPressuredTimeMsPerSecond / 1000

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain"
)

// PollerErrorKind classifies the variants surfaced by the poller. The
// shape mirrors the Rust thiserror enum with HTTP / Status / Body arms.
type PollerErrorKind int

const (
	PollerErrUnknown PollerErrorKind = iota
	PollerErrHTTP
	PollerErrStatus
	PollerErrBody
)

// PollerError mirrors event_streaming::runtime::flink::metrics_poller::PollerError.
type PollerError struct {
	Kind    PollerErrorKind
	Status  int
	Message string
	Cause   error
}

func (e *PollerError) Error() string {
	switch e.Kind {
	case PollerErrHTTP:
		return fmt.Sprintf("http: %v", e.Cause)
	case PollerErrStatus:
		return fmt.Sprintf("flink returned status %d", e.Status)
	case PollerErrBody:
		return fmt.Sprintf("invalid response: %s", e.Message)
	default:
		if e.Message != "" {
			return e.Message
		}
		return "unknown poller error"
	}
}

func (e *PollerError) Unwrap() error { return e.Cause }

// FlinkTopologyTarget is the per-topology context the poller needs to
// reach the JobManager. Mirrors the Rust FlinkTopology row struct.
type FlinkTopologyTarget struct {
	ID                  uuid.UUID
	FlinkDeploymentName string
	FlinkNamespace      string
	FlinkJobID          *string
}

// TopologyLister yields the active Flink topologies that the supervisor
// should poll. The query the Rust source runs is
//
//	SELECT id, flink_deployment_name, flink_namespace, flink_job_id
//	  FROM streaming_topologies
//	 WHERE runtime_kind = 'flink'
//	   AND status = 'running'
//	   AND flink_deployment_name IS NOT NULL
//	   AND flink_namespace IS NOT NULL
//
// Implementations should mirror that filter exactly so the same set of
// topologies is polled in both runtimes.
type TopologyLister interface {
	ListFlinkTopologies(ctx context.Context) ([]FlinkTopologyTarget, error)
}

// RunRecorder persists a row into streaming_topology_runs. Mirrors the
// Rust persist_run helper. Wired here as an interface so the supervisor
// can be unit-tested without a Postgres pool.
type RunRecorder interface {
	RecordTopologyRun(ctx context.Context, topologyID uuid.UUID, run TopologyRun) error
}

// TopologyRun is the projection the poller writes per tick. Matches the
// columns the Rust persist_run helper binds.
type TopologyRun struct {
	ID                   uuid.UUID
	Status               string
	Metrics              domain.TopologyRunMetrics
	StateSnapshot        domain.StateStoreSnapshot
	BackpressureSnapshot domain.BackpressureSnapshot
	StartedAt            time.Time
	CompletedAt          time.Time
}

// MetricsPollerSupervisor mirrors the Rust supervisor. It owns one
// goroutine per topology and a cancellation channel so callers can
// trigger a clean shutdown.
type MetricsPollerSupervisor struct {
	cfg      FlinkRuntimeConfig
	lister   TopologyLister
	recorder RunRecorder
	client   HTTPDoer
	logger   *slog.Logger
	now      func() time.Time

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SupervisorOptions wires the optional knobs of MetricsPollerSupervisor.
// Mirrors the Rust spawn() signature, which only takes (db, cfg).
type SupervisorOptions struct {
	// Client overrides the default 10 s timeout HTTP client. Tests pass
	// httptest.Server.Client() here.
	Client HTTPDoer
	// Logger receives the same warn/error events as the Rust tracing
	// macros. Defaults to slog.Default().
	Logger *slog.Logger
	// Now is the clock used to stamp persisted rows. Defaults to
	// time.Now().UTC(). Tests inject a fake clock to keep assertions
	// deterministic.
	Now func() time.Time
}

// NewMetricsPollerSupervisor wires a supervisor with the given dependencies.
func NewMetricsPollerSupervisor(cfg FlinkRuntimeConfig, lister TopologyLister, recorder RunRecorder, opts SupervisorOptions) *MetricsPollerSupervisor {
	client := opts.Client
	if client == nil {
		client = DefaultHTTPClient()
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MetricsPollerSupervisor{
		cfg:      cfg,
		lister:   lister,
		recorder: recorder,
		client:   client,
		logger:   logger,
		now:      now,
	}
}

// Spawn mirrors MetricsPollerSupervisor::spawn. It loads the active
// Flink topologies and starts one polling goroutine per topology. The
// returned error is non-nil only when the initial DB lookup fails — the
// per-topology goroutines log scrape failures and keep running.
func (s *MetricsPollerSupervisor) Spawn(ctx context.Context) error {
	if s.lister == nil {
		return fmt.Errorf("metrics poller: lister is required")
	}
	topologies, err := s.lister.ListFlinkTopologies(ctx)
	if err != nil {
		return fmt.Errorf("metrics poller: list flink topologies: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return fmt.Errorf("metrics poller: already running")
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	for _, topo := range topologies {
		s.wg.Add(1)
		go func(t FlinkTopologyTarget) {
			defer s.wg.Done()
			s.runLoop(loopCtx, t)
		}(topo)
	}
	return nil
}

// Shutdown mirrors MetricsPollerSupervisor::shutdown — cancels the
// per-topology goroutines and waits for them to drain.
func (s *MetricsPollerSupervisor) Shutdown() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

func (s *MetricsPollerSupervisor) runLoop(ctx context.Context, topo FlinkTopologyTarget) {
	intervalMS := s.cfg.MetricsPollIntervalMS
	if intervalMS < 1000 {
		intervalMS = 1000
	}
	ticker := time.NewTicker(time.Duration(intervalMS) * time.Millisecond)
	defer ticker.Stop()
	urlBase := s.cfg.JobManagerURL(topo.FlinkDeploymentName, topo.FlinkNamespace)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		metrics, err := PollOnce(ctx, s.client, urlBase, topo.FlinkJobID)
		if err != nil {
			s.logger.Warn("flink poller: scrape failed",
				slog.String("topology", topo.ID.String()),
				slog.String("deployment", topo.FlinkDeploymentName),
				slog.String("error", err.Error()),
			)
			continue
		}
		if s.recorder == nil {
			continue
		}
		run := buildTopologyRun(topo.ID, metrics, s.now())
		if err := s.recorder.RecordTopologyRun(ctx, topo.ID, run); err != nil {
			s.logger.Warn("flink poller: persist failed",
				slog.String("topology", topo.ID.String()),
				slog.String("error", err.Error()),
			)
		}
	}
}

// PollOnce mirrors metrics_poller::poll_once. Hits the JobManager once
// for the given topology and returns the canonical KPI vector. When
// jobID is nil it discovers a job id via /jobs (preferring RUNNING).
// Exposed so the runtime preview path can pull live metrics on demand
// — same signature shape as the Rust helper.
func PollOnce(ctx context.Context, client HTTPDoer, urlBase string, jobID *string) (domain.TopologyRunMetrics, error) {
	if client == nil {
		client = DefaultHTTPClient()
	}
	resolved := ""
	if jobID != nil {
		resolved = *jobID
	}
	if resolved == "" {
		discovered, err := discoverPollerJobID(ctx, client, urlBase)
		if err != nil {
			return domain.TopologyRunMetrics{}, err
		}
		resolved = discovered
	}
	url := urlBase + "/jobs/" + resolved + "/metrics?get=numRecordsInPerSecond,numRecordsOutPerSecond,backPressuredTimeMsPerSecond,numLateRecordsDropped"
	body, err := getRawJSON(ctx, client, url)
	if err != nil {
		return domain.TopologyRunMetrics{}, err
	}
	return ExtractKPIs(body), nil
}

func discoverPollerJobID(ctx context.Context, client HTTPDoer, urlBase string) (string, error) {
	body, err := getRawJSON(ctx, client, urlBase+"/jobs")
	if err != nil {
		return "", err
	}
	obj, ok := body.(map[string]any)
	if !ok {
		return "", &PollerError{Kind: PollerErrBody, Message: "missing 'jobs' array"}
	}
	jobs, ok := obj["jobs"].([]any)
	if !ok {
		return "", &PollerError{Kind: PollerErrBody, Message: "missing 'jobs' array"}
	}
	var chosen map[string]any
	for _, entry := range jobs {
		j, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if status, _ := j["status"].(string); status == "RUNNING" {
			chosen = j
			break
		}
	}
	if chosen == nil && len(jobs) > 0 {
		if first, ok := jobs[0].(map[string]any); ok {
			chosen = first
		}
	}
	if chosen == nil {
		return "", &PollerError{Kind: PollerErrBody, Message: "no jobs reported by jobmanager"}
	}
	id, ok := chosen["id"].(string)
	if !ok || id == "" {
		return "", &PollerError{Kind: PollerErrBody, Message: "job has no id"}
	}
	return id, nil
}

func getRawJSON(ctx context.Context, client HTTPDoer, url string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &PollerError{Kind: PollerErrHTTP, Cause: err}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &PollerError{Kind: PollerErrHTTP, Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &PollerError{Kind: PollerErrStatus, Status: resp.StatusCode}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &PollerError{Kind: PollerErrHTTP, Cause: err}
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, &PollerError{Kind: PollerErrBody, Message: err.Error()}
	}
	return raw, nil
}

// ExtractKPIs ports metrics_poller::extract_kpis. Pure function — maps a
// Flink /metrics response array into a TopologyRunMetrics. Multiple
// vertices may report the same metric id; we sum throughput-style values
// and divide backPressuredTimeMsPerSecond by 1000 to convert to a ratio.
//
// The Rust version sums into the same bucket for every metric id (not
// just throughput), so we replicate the behaviour byte-exactly: a metric
// appearing twice contributes the sum of both values.
func ExtractKPIs(raw any) domain.TopologyRunMetrics {
	arr, _ := raw.([]any)
	totals := make(map[string]float64, 4)
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := obj["id"].(string)
		valStr, _ := obj["value"].(string)
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			val = 0
		}
		totals[id] += val
	}
	throughput := totals["numRecordsOutPerSecond"]
	inRate := totals["numRecordsInPerSecond"]
	dropped := totals["numLateRecordsDropped"]
	backpressure := totals["backPressuredTimeMsPerSecond"] / 1000.0
	return domain.TopologyRunMetrics{
		// Multiplied by a 1 s window so the column reflects events per
		// poll tick even though Flink reports rates. The poller writes a
		// fresh row each tick so the time series stays meaningful.
		InputEvents:         int32(inRate),
		OutputEvents:        int32(throughput),
		AvgLatencyMS:        0,
		P95LatencyMS:        0,
		ThroughputPerSecond: float32(throughput),
		DroppedEvents:       int32(dropped),
		BackpressureRatio:   float32(backpressure),
		JoinOutputRows:      0,
		CepMatchCount:       0,
		StateEntries:        0,
	}
}

// buildTopologyRun packs the metrics into the TopologyRun the recorder
// persists. Mirrors the Rust persist_run() body exactly.
func buildTopologyRun(topologyID uuid.UUID, metrics domain.TopologyRunMetrics, now time.Time) TopologyRun {
	now = now.UTC()
	status := "ok"
	if metrics.BackpressureRatio > 0 {
		status = "throttled"
	}
	bp := domain.BackpressureSnapshot{
		QueueDepth:     0,
		QueueCapacity:  0,
		LagMS:          0,
		ThrottleFactor: metrics.BackpressureRatio,
		Status:         status,
	}
	state := domain.StateStoreSnapshot{
		Backend:          "flink-rocksdb",
		Namespace:        "topology/" + topologyID.String(),
		KeyCount:         metrics.StateEntries,
		DiskUsageMB:      0,
		CheckpointCount:  0,
		LastCheckpointAt: now,
	}
	return TopologyRun{
		ID:                   uuid.New(),
		Status:               "running",
		Metrics:              metrics,
		StateSnapshot:        state,
		BackpressureSnapshot: bp,
		StartedAt:            now,
		CompletedAt:          now,
	}
}
