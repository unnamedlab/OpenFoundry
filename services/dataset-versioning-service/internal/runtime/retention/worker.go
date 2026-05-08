// Package retention ports the Foundry "Branch retention" hourly archive
// worker from Rust src/domain/retention_worker.rs.
//
// The worker scans dataset_branches for non-root, non-archived branches
// with no OPEN transaction whose effective TTL has lapsed, soft-archives
// them (and reparents their direct children to the grandparent), and
// emits a `dataset.branch.archived.v1` outbox event per archive.
package retention

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/domain/retention"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
)

// ArchiveGraceDays mirrors the Rust ARCHIVE_GRACE_DAYS constant — the
// restore window after a branch is soft-archived.
const ArchiveGraceDays = 7

// DefaultTickInterval mirrors the Rust hourly tick.
const DefaultTickInterval = time.Hour

// Clock abstracts time.Now so the worker can be driven by a fake clock
// in tests. Production wiring uses systemClock.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// SystemClock returns the production clock implementation.
func SystemClock() Clock { return systemClock{} }

// Store is the database surface the worker depends on. Production wiring
// satisfies it with *repo.Repo; tests can supply a fake.
type Store interface {
	// LoadRetentionRows returns every non-archived, non-deleted branch
	// with the metadata required by the resolver + eligibility check.
	LoadRetentionRows(ctx context.Context) ([]models.RetentionRow, error)
	// ArchiveBranch reparents direct children to the grandparent,
	// soft-archives the branch, and writes a `branch.archived` outbox
	// event with `payload` in a single transaction. Returns true when
	// the row was archived; false when an idempotency guard tripped
	// (already archived between load + archive).
	ArchiveBranch(ctx context.Context, row models.RetentionRow, graceUntil time.Time, payload models.JSONValue) (bool, error)
}

// EligibleGaugeSetter mirrors the Rust DATASET_BRANCHES_ARCHIVE_ELIGIBLE
// Prometheus gauge. Production wiring plugs in the real metric; tests
// can use a no-op.
type EligibleGaugeSetter interface {
	Set(value int)
}

// ArchivedCounter mirrors the Rust DATASET_BRANCHES_ARCHIVED_TOTAL
// counter, labelled by archive reason.
type ArchivedCounter interface {
	Inc(reason string)
}

// Worker drives the retention loop.
type Worker struct {
	Store          Store
	Clock          Clock
	TickInterval   time.Duration
	Logger         *slog.Logger
	EligibleGauge  EligibleGaugeSetter
	ArchivedTotal  ArchivedCounter
	GraceDays      int
}

// New returns a Worker with production defaults filled in. Optional
// fields (Logger, gauges) are set to no-op shims so the worker is safe
// to start without explicit wiring.
func New(store Store) *Worker {
	return &Worker{
		Store:         store,
		Clock:         SystemClock(),
		TickInterval:  DefaultTickInterval,
		Logger:        slog.Default(),
		EligibleGauge: noopGauge{},
		ArchivedTotal: noopCounter{},
		GraceDays:     ArchiveGraceDays,
	}
}

// RunOnce archives every eligible branch in the database. Returns the
// number of branches archived. Used by both the scheduled loop and the
// integration tests (which call it directly to avoid waiting).
//
// Mirrors Rust retention_worker::run_once.
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	now := w.Clock.Now()
	rows, err := w.Store.LoadRetentionRows(ctx)
	if err != nil {
		return 0, err
	}
	index := retention.IndexRows(rows)

	archived := 0
	graceDays := w.GraceDays
	if graceDays <= 0 {
		graceDays = ArchiveGraceDays
	}
	graceUntil := now.Add(time.Duration(graceDays) * 24 * time.Hour)

	for _, row := range rows {
		effective := retention.ResolveEffective(row, index)
		if !retention.IsArchiveEligible(row, effective, now) {
			continue
		}
		payload := buildArchivePayload(effective, "ttl")
		ok, err := w.Store.ArchiveBranch(ctx, row, graceUntil, payload)
		if err != nil {
			return archived, err
		}
		if ok {
			archived++
			w.ArchivedTotal.Inc("ttl")
		}
	}

	w.EligibleGauge.Set(retention.CountEligible(rows, index, now))
	return archived, nil
}

// RunLoop runs RunOnce on every TickInterval. The first immediate tick
// is skipped so a fresh restart doesn't archive everything before the
// operator has had a chance to inspect the queue (matches Rust).
//
// Returns when ctx is cancelled.
func (w *Worker) RunLoop(ctx context.Context) {
	interval := w.TickInterval
	if interval <= 0 {
		interval = DefaultTickInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			archived, err := w.RunOnce(ctx)
			if err != nil {
				w.Logger.Warn("branch retention worker error", slog.String("error", err.Error()))
				continue
			}
			w.Logger.Info("branch retention worker tick", slog.Int("archived", archived))
		}
	}
}

// buildArchivePayload mirrors the `extras` field Rust attaches to the
// `dataset.branch.archived.v1` envelope. Encoding errors fall back to
// "{}" so the outbox row never carries a malformed payload — same
// defensive choice as the Rust into_payload helper.
func buildArchivePayload(effective models.EffectiveRetention, reason string) models.JSONValue {
	body := map[string]any{
		"reason": reason,
		"policy": string(effective.Policy),
	}
	if effective.TTLDays != nil {
		body["ttl_days"] = *effective.TTLDays
	} else {
		body["ttl_days"] = nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return models.JSONValue([]byte(`{}`))
	}
	return models.JSONValue(raw)
}

type noopGauge struct{}

func (noopGauge) Set(int) {}

type noopCounter struct{}

func (noopCounter) Inc(string) {}
