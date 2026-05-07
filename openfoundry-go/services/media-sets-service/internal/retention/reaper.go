// Package retention hosts the retention reaper. Mirrors
// services/media-sets-service/src/domain/retention.rs verbatim.
//
// Contract (`Advanced media set settings.md`):
//
//   - Items past their retention window become inaccessible and are
//     never restored, even if retention is later expanded or set to
//     "forever". The schema enforces "expansion does not restore" by
//     keeping deleted_at sticky; this package only ever flips
//     NULL → NOW().
//   - Retention reduction is immediate: the PATCH handler runs a
//     one-shot ReapMediaSet on the affected set, and the periodic
//     loop re-scans every set on Interval.
package retention

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediapath"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/metrics"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
)

// Reaper runs the periodic retention sweep + the per-set on-demand
// reap that PATCH handlers call. The byte cleanup is best-effort and
// is logged but never blocks the SQL update.
type Reaper struct {
	Pool     *pgxpool.Pool
	Storage  storage.Backend
	Metrics  *metrics.Metrics
	Logger   *slog.Logger
	Interval time.Duration
}

// New constructs a Reaper with sane defaults (1-minute interval,
// stdlib slog if Logger is nil).
func New(pool *pgxpool.Pool, store storage.Backend, m *metrics.Metrics, log *slog.Logger, interval time.Duration) *Reaper {
	if interval <= 0 {
		interval = time.Minute
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reaper{Pool: pool, Storage: store, Metrics: m, Logger: log, Interval: interval}
}

// ReapMediaSet runs one pass restricted to `mediaSetRID`. Returns the
// items it expired. Used by the PATCH handler so a retention
// reduction shows up immediately.
func (r *Reaper) ReapMediaSet(ctx context.Context, mediaSetRID string) ([]repo.ExpiredItem, error) {
	expired, err := repo.ReapMediaSet(ctx, r.Pool, mediaSetRID)
	if err != nil {
		return nil, err
	}
	r.handleExpired(ctx, expired)
	return expired, nil
}

// ReapDue runs one global pass. Returns the items expired so callers
// can introspect; production usage runs Loop, which discards the
// return value.
func (r *Reaper) ReapDue(ctx context.Context) ([]repo.ExpiredItem, error) {
	expired, err := repo.ReapDue(ctx, r.Pool)
	if err != nil {
		return nil, err
	}
	r.handleExpired(ctx, expired)
	return expired, nil
}

// Loop runs the periodic sweep until ctx is cancelled. Errors are
// logged + a zero-length pass is a no-op (matches the Rust loop). The
// first tick is skipped so the service has a moment to settle before
// the first sweep fires.
func (r *Reaper) Loop(ctx context.Context) {
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			expired, err := r.ReapDue(ctx)
			if err != nil {
				r.Logger.Error("retention reaper sweep failed", slog.String("error", err.Error()))
				continue
			}
			if len(expired) == 0 {
				r.Logger.Debug("retention reaper: nothing to expire")
				continue
			}
			r.Logger.Info("retention reaper: expired items", slog.Int("count", len(expired)))
		}
	}
}

// handleExpired drops the bytes for every expired item, emits the
// per-item audit log, and bumps the global counter. Failures are
// logged but never returned — the metadata row is the source of
// truth and the byte may already be gone.
func (r *Reaper) handleExpired(ctx context.Context, expired []repo.ExpiredItem) {
	if len(expired) == 0 {
		return
	}
	for _, it := range expired {
		// Audit emission is structured-log only, matching the Rust
		// impl: there is no `MediaItemRetentionExpired` envelope
		// today; the slog target "audit" is the durable trail.
		r.Logger.Info("media item soft-deleted by retention reaper",
			slog.String("audit_event", "media_item.retention_expired"),
			slog.String("media_item_rid", it.RID),
			slog.String("media_set_rid", it.MediaSetRID),
			slog.String("sha256", it.SHA256),
			slog.Int64("size_bytes", it.SizeBytes),
		)
		if it.SHA256 == "" {
			continue
		}
		key := mediapath.New(it.MediaSetRID, it.Branch, it.SHA256)
		if err := r.Storage.Delete(ctx, key); err != nil {
			r.Logger.Warn("retention byte cleanup failed",
				slog.String("rid", it.RID),
				slog.String("error", err.Error()),
			)
		}
	}
	if r.Metrics != nil {
		r.Metrics.MediaRetentionPurgesTotal.Add(float64(len(expired)))
	}
}
