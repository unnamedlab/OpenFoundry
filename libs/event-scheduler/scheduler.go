package eventscheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	schedulingcron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

// LineageNamespace is the OpenLineage namespace under which every
// emitted event is reported. Surfaced as a constant so consumers can
// subscribe by namespace.
const LineageNamespace = "of://schedules"

// LineageProducer is the producer URI advertised in the OpenLineage
// `producer` header.
const LineageProducer = "https://github.com/unnamedlab/OpenFoundry/libs/event-scheduler"

// uuidNamespaceOID mirrors Rust's `Uuid::NAMESPACE_OID` constant —
// the well-known OID namespace UUID from RFC 4122 §C. Used as the
// v5 namespace so the deterministic run_id matches the Rust runtime
// byte-for-byte.
var uuidNamespaceOID = uuid.MustParse("6ba7b812-9dad-11d1-80b4-00c04fd430c8")

// Scheduler is a cron-driven scheduler. Owns a Postgres pool and a
// Kafka publisher; stateless across [Scheduler.Tick] calls so a
// binary can build it once and invoke Tick exactly once per K8s
// CronJob run.
type Scheduler struct {
	pg        *pgxpool.Pool
	publisher databus.Publisher
}

// NewScheduler builds a Scheduler from a Postgres pool and a
// Publisher.
func NewScheduler(pg *pgxpool.Pool, publisher databus.Publisher) *Scheduler {
	return &Scheduler{pg: pg, publisher: publisher}
}

// Pool returns the underlying Postgres pool, exposed for tests /
// shutdown.
func (s *Scheduler) Pool() *pgxpool.Pool { return s.pg }

// Tick runs one tick: claim every due, enabled schedule, publish its
// payload, advance its next_run_at, and stamp last_run_at.
//
// Returns the number of schedules that successfully fired. A row
// whose Kafka publish or cron-recompute fails causes the whole tick
// to abort with the corresponding [SchedulerError] so the CronJob
// pod restarts and retries; partial progress that already committed
// (because we commit per-row) is preserved.
func (s *Scheduler) Tick(ctx context.Context, now time.Time) (int, error) {
	fired := 0
	for {
		// Claim and process one row at a time so a slow Kafka
		// publish can't hold a transaction open across many rows
		// (which would also extend the SKIP LOCKED window).
		tx, err := s.pg.Begin(ctx)
		if err != nil {
			return fired, &SchedulerError{Kind: ErrDB, Cause: err}
		}

		row := tx.QueryRow(ctx,
			`SELECT id, name, cron_expr, cron_flavor, time_zone, enabled, topic,
			        payload_template, next_run_at, last_run_at
			   FROM schedules.definitions
			  WHERE enabled AND next_run_at <= $1
			  ORDER BY next_run_at
			  LIMIT 1
			  FOR UPDATE SKIP LOCKED`,
			now,
		)

		var def ScheduleDefinition
		err = row.Scan(
			&def.ID,
			&def.Name,
			&def.CronExpr,
			&def.CronFlavor,
			&def.TimeZone,
			&def.Enabled,
			&def.Topic,
			&def.PayloadTemplate,
			&def.NextRunAt,
			&def.LastRunAt,
		)
		if err == pgx.ErrNoRows {
			// Nothing more to do.
			if err := tx.Commit(ctx); err != nil {
				return fired, &SchedulerError{Kind: ErrDB, Cause: err}
			}
			return fired, nil
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fired, &SchedulerError{Kind: ErrDB, Cause: err}
		}

		// The instant for which we are firing — record both in
		// OpenLineage and in last_run_at so consumers can tell
		// apart "fired late" from "fired on time".
		scheduledFor := def.NextRunAt

		// Recompute next_run_at strictly past `now` so:
		//   * a schedule that was due exactly at `now` doesn't
		//     immediately re-fire inside the same tick loop, and
		//   * a tick that runs late (e.g. CronJob skipped a
		//     period) collapses any missed fires into one event
		//     and resumes from the next future slot — the
		//     standard cron / K8s `concurrencyPolicy=Forbid`
		//     semantic.
		// A malformed row can't slip through: if recompute fails
		// we abort the tick before publishing.
		next, err := ComputeNextFire(&def, now)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fired, err
		}

		// Publish to Kafka. We hold the row lock open across the
		// publish on purpose: if the broker is unavailable we
		// want the row to remain claimed only as long as the
		// publish attempt itself, then released untouched on
		// rollback so the next tick retries.
		lineage := BuildLineage(def.Name, scheduledFor)
		if err := s.publisher.Publish(ctx, def.Topic, []byte(def.Name), def.PayloadTemplate, &lineage); err != nil {
			_ = tx.Rollback(ctx)
			return fired, &SchedulerError{
				Kind:  ErrPublish,
				Name:  def.Name,
				Topic: def.Topic,
				Cause: err,
			}
		}

		_, err = tx.Exec(ctx,
			`UPDATE schedules.definitions
			    SET next_run_at = $1, last_run_at = $2, updated_at = now()
			  WHERE id = $3`,
			next, scheduledFor, def.ID,
		)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fired, &SchedulerError{Kind: ErrDB, Cause: err}
		}

		if err := tx.Commit(ctx); err != nil {
			return fired, &SchedulerError{Kind: ErrDB, Cause: err}
		}

		fired++
	}
}

// BuildLineage builds the OpenLineage headers for a single fire.
// Public so tests / downstream consumers can recompute the
// deterministic run_id for idempotent reprocessing.
//
// The run_id is a v5 UUID over the `Uuid::NAMESPACE_OID` namespace
// and the bytes of `<name>|<scheduled_for_rfc3339>` — same shape as
// the Rust implementation. The RFC3339 string is rendered with
// chrono's `SecondsFormat::AutoSi` rules (UTC offset spelled out as
// `+00:00`, fractional seconds at 0/3/6/9 digit boundaries) so the
// derived UUID matches the Rust runtime byte-for-byte.
func BuildLineage(name string, scheduledFor time.Time) databus.OpenLineageHeaders {
	key := fmt.Sprintf("%s|%s", name, formatChronoRFC3339(scheduledFor))
	runID := uuid.NewSHA1(uuidNamespaceOID, []byte(key)).String()
	// NewOpenLineageHeaders defaults EventTime to time.Now(); we
	// override with `scheduled_for` so consumers see the instant
	// for which the schedule fired, not the instant we built the
	// headers.
	return databus.NewOpenLineageHeaders(LineageNamespace, name, runID, LineageProducer).
		WithEventTime(scheduledFor)
}

// formatChronoRFC3339 renders `t` exactly like
// `chrono::DateTime::to_rfc3339()` with `SecondsFormat::AutoSi` —
// UTC offset is spelled out as `+00:00` and fractional seconds snap
// to 0, 3, 6 or 9 digit precision (whichever is the smallest that
// preserves the value losslessly).
func formatChronoRFC3339(t time.Time) string {
	// Date + time without offset.
	stem := t.Format("2006-01-02T15:04:05")
	// Offset spelled out as ±HH:MM (matches chrono's UTC = `+00:00`,
	// not `Z`).
	_, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	offsetStr := fmt.Sprintf("%s%02d:%02d", sign, offset/3600, (offset%3600)/60)

	nanos := t.Nanosecond()
	switch {
	case nanos == 0:
		return stem + offsetStr
	case nanos%1_000_000 == 0:
		return fmt.Sprintf("%s.%03d%s", stem, nanos/1_000_000, offsetStr)
	case nanos%1_000 == 0:
		return fmt.Sprintf("%s.%06d%s", stem, nanos/1_000, offsetStr)
	default:
		return fmt.Sprintf("%s.%09d%s", stem, nanos, offsetStr)
	}
}

// ComputeNextFire returns the next UTC instant strictly after
// scheduledFor at which `def` should fire. Public so tests and admin
// tools can reuse the same logic without going through Tick.
func ComputeNextFire(def *ScheduleDefinition, scheduledFor time.Time) (time.Time, error) {
	flavor, err := def.tryFlavor()
	if err != nil {
		return time.Time{}, err
	}
	tz, err := def.tryTZ()
	if err != nil {
		return time.Time{}, err
	}
	schedule, err := schedulingcron.ParseCron(def.CronExpr, flavor, tz)
	if err != nil {
		return time.Time{}, &SchedulerError{
			Kind:  ErrInvalidCron,
			Name:  def.Name,
			Cause: err,
		}
	}
	next, ok := schedulingcron.NextFireAfter(&schedule, scheduledFor)
	if !ok {
		return time.Time{}, &SchedulerError{
			Kind:     ErrNoFutureFire,
			Name:     def.Name,
			CronExpr: def.CronExpr,
		}
	}
	return next, nil
}
