// Package runtime hosts the Kafka consumer loop + per-job Coordinator
// state machine for reindex-coordinator-service. It is the Go port of
// services/reindex-coordinator-service/src/runtime.rs (FASE 4 / RC-5).
//
// One Kafka message on ontology.reindex.requested.v1 ⇒ one full job:
// the Coordinator drives the (decode → mark_running → scan →
// publish_batches → mark_complete) loop until a terminal status, then
// the consumer commits the offset. A crash mid-job leaves the offset
// uncommitted; on restart Kafka redelivers, the state machine picks
// up at the persisted resume_token, and the per-batch idempotency
// store skips already-published pages.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/libs/idempotency"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/event"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/scan"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/state"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/topics"
)

// ConsumerGroup is the Kafka consumer group used by every replica of
// the coordinator. Pinned here so a typo across replicas cannot
// silently fork rebalance state.
const ConsumerGroup = "reindex-coordinator-service"

// SubscribeTopics is the (single) topic the coordinator subscribes to.
var SubscribeTopics = []string{topics.OntologyReindexRequestedV1}

// pollErrorBackoff is the bounded sleep after a process_message error
// so a poison message that the broker keeps redelivering does not
// burn CPU before DLQ kicks in. Same magic constant as the Rust impl.
const pollErrorBackoff = 5 * time.Second

// ─────────────────────────── Throttle ───────────────────────────

// Throttle bundles the per-process rate-limit knobs. The Rust port
// makes these explicit (Temporal's implicit retry backoff is gone)
// so a full backfill cannot saturate objects_by_id. Both knobs read
// from env at startup with defaults that match the legacy Go
// worker's effective inter-page interval.
type Throttle struct {
	// PageInterval sleeps between successive Cassandra page fetches
	// for the same job. Zero ⇒ no sleep. Source:
	// OF_REINDEX_PAGE_INTERVAL_MS.
	PageInterval time.Duration
	// MaxBatchesPerSecond caps the total batches a single coordinator
	// process publishes per second across all jobs. Zero ⇒ unbounded.
	// Source: OF_REINDEX_MAX_BATCHES_PER_SECOND.
	MaxBatchesPerSecond uint32
}

// ThrottleFromEnv reads OF_REINDEX_PAGE_INTERVAL_MS and
// OF_REINDEX_MAX_BATCHES_PER_SECOND with the same parsing rules as
// the Rust impl: missing ⇒ default, unparseable ⇒ typed error.
func ThrottleFromEnv() (Throttle, error) {
	pageMs, err := parseUint64Env("OF_REINDEX_PAGE_INTERVAL_MS", 0)
	if err != nil {
		return Throttle{}, err
	}
	maxPerSec, err := parseUint32Env("OF_REINDEX_MAX_BATCHES_PER_SECOND", 0)
	if err != nil {
		return Throttle{}, err
	}
	return Throttle{
		PageInterval:        time.Duration(pageMs) * time.Millisecond,
		MaxBatchesPerSecond: maxPerSec,
	}, nil
}

// perPublishSleep returns the inter-publish delay needed to keep the
// process below MaxBatchesPerSecond. Implementation rounds upward to
// the nearest millisecond — same approximation as the Rust impl —
// which keeps the actual rate at or below the configured ceiling.
func (t Throttle) perPublishSleep() time.Duration {
	if t.MaxBatchesPerSecond == 0 {
		return 0
	}
	n := uint64(t.MaxBatchesPerSecond)
	ms := (1000 + n - 1) / n
	return time.Duration(ms) * time.Millisecond
}

// ─────────────────────────── Metrics ───────────────────────────

// Metrics wraps the four pinned coordinator counters / gauges so the
// labels match the Rust series names byte-for-byte. Dashboards and
// Prometheus rules in infra/helm/.../prometheus-rules-reindex-coordinator
// reference these names verbatim.
type Metrics struct {
	RequestsTotal *prometheus.CounterVec
	BatchesTotal  *prometheus.CounterVec
	RecordsTotal  *prometheus.CounterVec
	JobsInFlight  prometheus.Gauge
}

// NewMetrics registers the four series on reg and returns the
// resulting *Metrics. Pass observability.Metrics.Registry for the
// production wiring; pass prometheus.NewRegistry() in tests.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reindex_coordinator_requests_total",
		Help: "Reindex requests received and classified.",
	}, []string{"outcome"})
	batches := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reindex_coordinator_batches_total",
		Help: "Batches produced to ontology.reindex.v1, by outcome.",
	}, []string{"outcome"})
	records := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "reindex_coordinator_records_total",
		Help: "Records contained in published batches, by outcome.",
	}, []string{"outcome"})
	inFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "reindex_coordinator_jobs_in_flight",
		Help: "Number of jobs currently in the running state.",
	})
	if reg != nil {
		reg.MustRegister(requests, batches, records, inFlight)
	}
	return &Metrics{
		RequestsTotal: requests,
		BatchesTotal:  batches,
		RecordsTotal:  records,
		JobsInFlight:  inFlight,
	}
}

// ─────────────────── Scanner / JobStore interfaces ───────────────────

// Scanner is the surface the coordinator needs from the Cassandra
// paginated scanner. *scan.CassandraScanner satisfies this; tests
// inject a fake to avoid the Cassandra round-trip.
type Scanner interface {
	ScanPage(
		ctx context.Context,
		tenantID string,
		typeID *string,
		pageSize int32,
		resumeToken *string,
	) (*scan.PageOutcome, error)
}

// JobStore is the surface the coordinator needs from the Postgres
// JobRepo. *repo.JobRepo satisfies this; tests inject an in-memory
// fake to avoid the Postgres round-trip when the fake is enough.
type JobStore interface {
	UpsertQueued(ctx context.Context, id uuid.UUID, tenantID, typeID string, pageSize int32) (repo.JobRecord, error)
	Load(ctx context.Context, id uuid.UUID) (repo.JobRecord, error)
	ListResumable(ctx context.Context) ([]repo.JobRecord, error)
	MarkRunning(ctx context.Context, id uuid.UUID) error
	Advance(ctx context.Context, id uuid.UUID, nextResumeToken *string, scannedDelta, publishedDelta int64) error
	MarkTerminal(ctx context.Context, id uuid.UUID, next state.JobStatus, errMessage *string) (repo.JobRecord, error)
}

// ─────────────────────────── Coordinator ───────────────────────────

// Coordinator owns all long-lived state shared between the consumer
// loop and the per-job task. Construct once at process start, share
// the pointer.
type Coordinator struct {
	Jobs             JobStore
	Idempotency      idempotency.Store
	Scanner          Scanner
	Publisher        databus.Publisher
	Metrics          *Metrics
	Throttle         Throttle
	LineageNamespace string
	Log              *slog.Logger

	// sleep is injectable so tests do not have to wait for real time
	// when exercising the throttle path. Defaults to time.Sleep.
	sleep func(time.Duration)
}

// NewCoordinator validates the wiring and returns a ready-to-run
// Coordinator. Logger defaults to slog.Default; sleep to time.Sleep.
func NewCoordinator(
	jobs JobStore,
	idem idempotency.Store,
	scanner Scanner,
	publisher databus.Publisher,
	metrics *Metrics,
	throttle Throttle,
	lineageNamespace string,
	log *slog.Logger,
) *Coordinator {
	if log == nil {
		log = slog.Default()
	}
	if lineageNamespace == "" {
		lineageNamespace = "openfoundry"
	}
	return &Coordinator{
		Jobs:             jobs,
		Idempotency:      idem,
		Scanner:          scanner,
		Publisher:        publisher,
		Metrics:          metrics,
		Throttle:         throttle,
		LineageNamespace: lineageNamespace,
		Log:              log,
		sleep:            time.Sleep,
	}
}

// RunJob drives one in-flight job to a terminal status. Idempotent
// on every page boundary: a redelivery of the same Kafka message
// re-discovers the row, picks up at the persisted resume_token, and
// the per-batch idempotency store skips already-published pages.
func (c *Coordinator) RunJob(ctx context.Context, jobID uuid.UUID, requestID *string) (repo.JobRecord, error) {
	if err := c.Jobs.MarkRunning(ctx, jobID); err != nil {
		var ill *state.IllegalTransitionError
		if errors.As(err, &ill) && ill.From.IsTerminal() {
			c.Log.Info("skipping run for terminal job (duplicate requested.v1)",
				slog.String("job_id", jobID.String()),
				slog.String("status", ill.From.String()))
			return c.Jobs.Load(ctx, jobID)
		}
		return repo.JobRecord{}, err
	}
	c.Metrics.JobsInFlight.Inc()
	defer c.Metrics.JobsInFlight.Dec()
	return c.runJobInner(ctx, jobID, requestID)
}

// runJobInner is the page-by-page loop. Order of operations is
// load-of-current → record event_id → scan → publish (or skip on
// dedup) → advance row. The "advance after publish" order is the
// crash-safety contract: a crash post-publish but pre-advance leaves
// the same event_id in processed_events so the next attempt skips
// re-publishing.
func (c *Coordinator) runJobInner(ctx context.Context, jobID uuid.UUID, requestID *string) (repo.JobRecord, error) {
	current, err := c.Jobs.Load(ctx, jobID)
	if err != nil {
		return repo.JobRecord{}, err
	}
	perPublishSleep := c.Throttle.perPublishSleep()

	for {
		token := current.ResumeToken
		typeIDOpt := optionalTypeID(current.TypeID)
		batchEventID := event.DeriveBatchEventID(current.TenantID, typeIDOpt, derefOr(token, ""))

		outcome, err := c.Idempotency.CheckAndRecord(ctx, batchEventID)
		if err != nil {
			return repo.JobRecord{}, fmt.Errorf("idempotency check: %w", err)
		}

		page, err := c.Scanner.ScanPage(ctx, current.TenantID, typeIDOpt, current.PageSize, token)
		if err != nil {
			msg := err.Error()
			final, terr := c.Jobs.MarkTerminal(ctx, jobID, state.StatusFailed, &msg)
			if terr != nil {
				return repo.JobRecord{}, terr
			}
			if perr := c.publishCompleted(ctx, &final, requestID); perr != nil {
				return repo.JobRecord{}, perr
			}
			return repo.JobRecord{}, err
		}

		batchSize := len(page.Records)
		if outcome.IsFirstSeen() {
			if perr := c.publishBatch(ctx, page.Records, &current); perr != nil {
				c.Metrics.BatchesTotal.WithLabelValues("publish_error").Inc()
				msg := perr.Error()
				final, terr := c.Jobs.MarkTerminal(ctx, jobID, state.StatusFailed, &msg)
				if terr != nil {
					return repo.JobRecord{}, terr
				}
				if cerr := c.publishCompleted(ctx, &final, requestID); cerr != nil {
					return repo.JobRecord{}, cerr
				}
				return repo.JobRecord{}, perr
			}
			c.Metrics.BatchesTotal.WithLabelValues("published").Inc()
			c.Metrics.RecordsTotal.WithLabelValues("published").Add(float64(batchSize))
		} else {
			c.Metrics.BatchesTotal.WithLabelValues("deduped").Inc()
			c.Metrics.RecordsTotal.WithLabelValues("deduped").Add(float64(batchSize))
			c.Log.Info("batch already processed; skipping publish (idempotency replay)",
				slog.String("job_id", jobID.String()),
				slog.String("batch_event_id", batchEventID.String()))
		}

		// Persist row state AFTER publish: a crash here leaves the
		// next attempt re-deriving the same batchEventID, seeing
		// it in processed_events, and correctly skipping re-publish.
		var publishedDelta int64
		if outcome.IsFirstSeen() {
			publishedDelta = int64(batchSize)
		}
		if aerr := c.Jobs.Advance(ctx, jobID, page.NextToken, int64(page.Scanned), publishedDelta); aerr != nil {
			return repo.JobRecord{}, aerr
		}

		current, err = c.Jobs.Load(ctx, jobID)
		if err != nil {
			return repo.JobRecord{}, err
		}

		if page.NextToken == nil {
			final, terr := c.Jobs.MarkTerminal(ctx, jobID, state.StatusCompleted, nil)
			if terr != nil {
				return repo.JobRecord{}, terr
			}
			if cerr := c.publishCompleted(ctx, &final, requestID); cerr != nil {
				return repo.JobRecord{}, cerr
			}
			return final, nil
		}

		if perPublishSleep > 0 {
			c.sleep(perPublishSleep)
		}
		if c.Throttle.PageInterval > 0 {
			c.sleep(c.Throttle.PageInterval)
		}
	}
}

// publishBatch writes one ReindexRecord per Kafka record to
// ontology.reindex.v1. The per-record key is the same "tenant/id"
// composition the legacy Go worker used so re-indexed records hash
// to the same partition as live object.changed.v1 records — required
// for the indexer's per-object version check.
func (c *Coordinator) publishBatch(ctx context.Context, records []scan.ReindexRecord, job *repo.JobRecord) error {
	jobName := fmt.Sprintf("reindex/%s/%s", job.TenantID, defaultStr(job.TypeID, "*"))
	for i := range records {
		payload, err := json.Marshal(&records[i])
		if err != nil {
			return fmt.Errorf("encode reindex record: %w", err)
		}
		key := records[i].PartitionKey()
		lineage := databus.NewOpenLineageHeaders(c.LineageNamespace, jobName, job.ID.String(), ConsumerGroup)
		if err := c.Publisher.Publish(ctx, topics.OntologyReindexV1, []byte(key), payload, &lineage); err != nil {
			return fmt.Errorf("publish %s: %w", topics.OntologyReindexV1, err)
		}
	}
	return nil
}

// publishCompleted emits one terminal record on
// ontology.reindex.completed.v1. The Kafka key is the job UUID
// bytes so all completion records for the same job land on the
// same partition, matching the Rust producer.
func (c *Coordinator) publishCompleted(ctx context.Context, job *repo.JobRecord, requestID *string) error {
	completed := event.ReindexCompletedV1{
		JobID:     job.ID,
		TenantID:  job.TenantID,
		TypeID:    optionalTypeID(job.TypeID),
		Scanned:   job.Scanned,
		Published: job.Published,
		Status:    job.Status.String(),
		Error:     job.Error,
		RequestID: requestID,
	}
	payload, err := json.Marshal(&completed)
	if err != nil {
		return fmt.Errorf("encode completed event: %w", err)
	}
	jobName := fmt.Sprintf("reindex/%s", job.TenantID)
	lineage := databus.NewOpenLineageHeaders(c.LineageNamespace, jobName, job.ID.String(), ConsumerGroup)
	keyBytes, _ := job.ID.MarshalBinary()
	if err := c.Publisher.Publish(ctx, topics.OntologyReindexCompletedV1, keyBytes, payload, &lineage); err != nil {
		return fmt.Errorf("publish %s: %w", topics.OntologyReindexCompletedV1, err)
	}
	return nil
}

// ─────────────────────── Consumer loop entry ───────────────────────

// Run subscribes through sub and drives the at-least-once consumer
// loop until ctx is canceled. One Kafka message ⇒ one full job
// (drives pages until terminal). Offsets are committed AFTER the
// job reaches a terminal status, so a crash mid-job replays the
// message after restart and the coordinator picks up at the
// persisted resume_token.
func Run(ctx context.Context, c *Coordinator, sub databus.Subscriber) error {
	if c == nil {
		return errors.New("runtime: nil coordinator")
	}
	if sub == nil {
		return errors.New("runtime: nil subscriber")
	}

	// Resume any in-flight jobs left over from a crash before we
	// start to drain Kafka. This is the restart-safety guarantee
	// called out in the inventory doc §7.
	resumable, err := c.Jobs.ListResumable(ctx)
	if err != nil {
		return fmt.Errorf("list resumable jobs: %w", err)
	}
	if len(resumable) > 0 {
		c.Log.Info("resuming in-flight jobs from a previous run",
			slog.Int("count", len(resumable)))
		for _, job := range resumable {
			id := job.ID
			go func() {
				if err := c.runResumed(ctx, id); err != nil {
					c.Log.Error("resumed job failed",
						slog.String("job_id", id.String()),
						slog.String("error", err.Error()))
				}
			}()
		}
	}

	c.Log.Info("reindex-coordinator consumer loop started",
		slog.String("group", ConsumerGroup),
		slog.Any("topics", SubscribeTopics))

	for {
		msg, err := sub.Poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// A transient broker outage / closed reader bubbles up
			// to the supervisor, which restarts the loop from the
			// last committed offset.
			return fmt.Errorf("kafka poll: %w", err)
		}

		label, perr := processRequestMessage(ctx, c, msg)
		if perr != nil {
			c.Metrics.RequestsTotal.WithLabelValues("error").Inc()
			c.Log.Error("reindex request failed; offset uncommitted",
				slog.String("error", perr.Error()))
			// Bounded sleep so a hot loop doesn't burn CPU when
			// the broker keeps redelivering the same poison
			// message before DLQ kicks in.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollErrorBackoff):
			}
			continue
		}
		c.Metrics.RequestsTotal.WithLabelValues(label).Inc()
		if err := msg.Commit(ctx); err != nil {
			return fmt.Errorf("commit offset: %w", err)
		}
	}
}

// runResumed wraps RunJob so the spawned goroutine for a recovered
// job uses the same logging shape as the message-driven path.
func (c *Coordinator) runResumed(ctx context.Context, jobID uuid.UUID) error {
	if _, err := c.RunJob(ctx, jobID, nil); err != nil {
		return err
	}
	return nil
}

// processRequestMessage decodes one Kafka message and runs the
// corresponding job to completion. Returns the metric label that
// describes the outcome of the message ("empty_payload",
// "decode_error", "already_terminal", "completed"); an error here
// means RunJob blew up and the caller MUST NOT commit the offset.
func processRequestMessage(ctx context.Context, c *Coordinator, msg *databus.DataMessage) (string, error) {
	if len(msg.Value) == 0 {
		c.Log.Warn("skipping reindex request without payload",
			slog.String("topic", msg.Topic),
			slog.Int("partition", msg.Partition),
			slog.Int64("offset", msg.Offset))
		return "empty_payload", nil
	}
	request, err := scan.DecodeRequest(msg.Value)
	if err != nil {
		c.Log.Warn("skipping malformed reindex request",
			slog.String("error", err.Error()))
		return "decode_error", nil
	}
	jobID := event.DeriveJobID(request.TenantID, request.TypeID)
	job, err := c.Jobs.UpsertQueued(ctx, jobID, request.TenantID, derefOr(request.TypeID, ""), request.PageSize)
	if err != nil {
		return "", fmt.Errorf("upsert queued: %w", err)
	}
	c.Log.Info("reindex request accepted",
		slog.String("job_id", jobID.String()),
		slog.String("tenant", request.TenantID),
		slog.String("type_id", derefOr(request.TypeID, "")),
		slog.Int("page_size", int(request.PageSize)),
		slog.String("existing_status", job.Status.String()))
	if job.Status.IsTerminal() {
		// Re-running a finished job is the producer's responsibility;
		// surface the terminal status in metrics and move on.
		return "already_terminal", nil
	}
	if _, err := c.RunJob(ctx, jobID, request.RequestID); err != nil {
		return "", err
	}
	return "completed", nil
}

// ─────────────────────────── helpers ───────────────────────────

func parseUint64Env(key string, fallback uint64) (uint64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, &InvalidEnvError{Key: key, Value: v, Reason: "expected unsigned integer"}
	}
	return n, nil
}

func parseUint32Env(key string, fallback uint32) (uint32, error) {
	n, err := parseUint64Env(key, uint64(fallback))
	if err != nil {
		return 0, err
	}
	if n > uint64(^uint32(0)) {
		return 0, &InvalidEnvError{Key: key, Value: strconv.FormatUint(n, 10), Reason: "value exceeds uint32"}
	}
	return uint32(n), nil
}

// optionalTypeID converts the empty-string-as-all-types repo
// convention back to the *string boundary form Rust uses. Empty
// string ⇒ nil.
func optionalTypeID(typeID string) *string {
	if typeID == "" {
		return nil
	}
	return &typeID
}

// derefOr returns *p when p != nil, otherwise fallback.
func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// ─────────────────────────── Errors ───────────────────────────

// InvalidEnvError mirrors the Rust RuntimeError::InvalidEnv variant.
type InvalidEnvError struct {
	Key    string
	Value  string
	Reason string
}

func (e *InvalidEnvError) Error() string {
	return fmt.Sprintf("invalid environment variable %s=%q: %s", e.Key, e.Value, e.Reason)
}

// MissingEnvError mirrors the Rust RuntimeError::MissingEnv variant.
type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is not set", e.Key)
}

// IsRetryablePollError reports whether err is a transient broker error
// that the supervisor should restart the consumer for. Non-retryable
// errors (typed *MissingEnvError, *InvalidEnvError) must surface
// immediately so the operator notices the misconfiguration.
func IsRetryablePollError(err error) bool {
	if err == nil {
		return false
	}
	var missing *MissingEnvError
	if errors.As(err, &missing) {
		return false
	}
	var invalid *InvalidEnvError
	if errors.As(err, &invalid) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary() //nolint:staticcheck // Temporary() is still used by segmentio.
	}
	return true
}
