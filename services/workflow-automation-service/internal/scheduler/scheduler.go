// Package scheduler is the cron-driven activator for workflows whose
// trigger is a wall-clock expression. It polls the workflows table on
// a fixed cadence, picks up any rows whose `next_run_at` is due,
// recomputes the next fire instant via libs/scheduling-cron and hands
// the workflow off to the existing transactional dispatcher (the same
// path `manual` / `webhook` triggers use).
//
// The package owns NO mutation beyond `workflows.next_run_at` and is
// deliberately blind to the run/saga/approval state machines — those
// already work; this loop is the missing time-source that fires them.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	cron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

// DB is the minimal pgx surface the scheduler relies on; satisfied by
// `*pgxpool.Pool` in production and by `pgxmock.PgxPoolIface` in
// tests.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Clock is the time source the scheduler reads. Production uses
// RealClock; tests inject a fake to make the every-30s tick
// deterministic.
type Clock interface {
	Now() time.Time
}

// RealClock returns the current wall clock in UTC.
type RealClock struct{}

// Now returns the current time in UTC.
func (RealClock) Now() time.Time { return time.Now().UTC() }

// Loader resolves a workflow id to its hydrated definition. Wired to
// `handlers.LoadWorkflow` in main; isolated as a function value so
// the test does not need to import the handlers package.
type Loader func(ctx context.Context, id uuid.UUID) (*models.WorkflowDefinition, error)

// Dispatcher fires the run for a due workflow. Wired to
// `handlers.DispatchRun` in main with `triggerType = "schedule"`.
type Dispatcher func(ctx context.Context, workflow *models.WorkflowDefinition) (*models.WorkflowRun, error)

// Scheduler polls `workflows` for due crons and hands each one off
// through Dispatcher. Construct via New and run via Run.
type Scheduler struct {
	DB           DB
	Clock        Clock
	TickInterval time.Duration
	BatchLimit   int
	Load         Loader
	Dispatch     Dispatcher
	Logger       *slog.Logger
}

// New wires a Scheduler with the production defaults: a 30-second
// tick and a 100-row batch ceiling per tick.
func New(db DB, clock Clock, load Loader, dispatch Dispatcher, logger *slog.Logger) *Scheduler {
	if clock == nil {
		clock = RealClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		DB:           db,
		Clock:        clock,
		TickInterval: 30 * time.Second,
		BatchLimit:   100,
		Load:         load,
		Dispatch:     dispatch,
		Logger:       logger,
	}
}

// Run blocks until ctx is cancelled, calling Tick on the configured
// cadence. Tick errors are logged and swallowed — the next tick will
// retry, the same way the saga + condition consumers reconnect.
func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.Logger.Error("workflow scheduler tick failed", slog.String("error", err.Error()))
	}
	t := time.NewTicker(s.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.Logger.Error("workflow scheduler tick failed", slog.String("error", err.Error()))
			}
		}
	}
}

// Tick selects every workflow whose next_run_at has elapsed, advances
// their next_run_at inside one transaction (so concurrent schedulers
// in HA never double-fire — SKIP LOCKED gives us pessimistic claim
// semantics), then dispatches each match outside the lock window.
// Returning the dispatcher's slow path under the row lock would
// stall every other scheduler instance while a single workflow is
// queueing through the outbox, so we take the lock, advance the next
// fire instant, commit, and only then run the dispatch loop.
func (s *Scheduler) Tick(ctx context.Context) error {
	due, err := s.acquireDuePlans(ctx)
	if err != nil {
		return err
	}
	for _, plan := range due {
		workflow, err := s.Load(ctx, plan.id)
		if err != nil {
			s.Logger.Error("scheduler load workflow failed",
				slog.String("workflow_id", plan.id.String()),
				slog.String("error", err.Error()))
			continue
		}
		if workflow == nil {
			continue
		}
		if _, err := s.Dispatch(ctx, workflow); err != nil {
			s.Logger.Error("scheduler dispatch failed",
				slog.String("workflow_id", plan.id.String()),
				slog.String("error", err.Error()))
		}
	}
	return nil
}

type plan struct {
	id       uuid.UUID
	cronExpr string
}

func (s *Scheduler) acquireDuePlans(ctx context.Context) ([]plan, error) {
	now := s.Clock.Now()

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT id, cron_expr
		  FROM workflows
		 WHERE enabled = TRUE
		   AND cron_expr IS NOT NULL
		   AND next_run_at IS NOT NULL
		   AND next_run_at <= $1
		 ORDER BY next_run_at ASC
		 LIMIT $2
		 FOR UPDATE SKIP LOCKED`, now, s.BatchLimit)
	if err != nil {
		return nil, err
	}
	var plans []plan
	for rows.Next() {
		var p plan
		if err := rows.Scan(&p.id, &p.cronExpr); err != nil {
			rows.Close()
			return nil, err
		}
		plans = append(plans, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	due := make([]plan, 0, len(plans))
	for _, p := range plans {
		schedule, perr := cron.ParseCron(p.cronExpr, cron.Unix5, time.UTC)
		if perr != nil {
			s.Logger.Warn("scheduler parse cron failed; pausing workflow",
				slog.String("workflow_id", p.id.String()),
				slog.String("cron_expr", p.cronExpr),
				slog.String("error", perr.Error()))
			if _, err := tx.Exec(ctx,
				`UPDATE workflows SET next_run_at = NULL, updated_at = NOW() WHERE id = $1`,
				p.id); err != nil {
				return nil, err
			}
			continue
		}
		next, ok := cron.NextFireAfter(&schedule, now)
		if !ok {
			s.Logger.Warn("scheduler exhausted future fires; pausing workflow",
				slog.String("workflow_id", p.id.String()),
				slog.String("cron_expr", p.cronExpr))
			if _, err := tx.Exec(ctx,
				`UPDATE workflows SET next_run_at = NULL, updated_at = NOW() WHERE id = $1`,
				p.id); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := tx.Exec(ctx,
			`UPDATE workflows SET next_run_at = $2, updated_at = NOW() WHERE id = $1`,
			p.id, next.UTC()); err != nil {
			return nil, err
		}
		due = append(due, p)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return due, nil
}
