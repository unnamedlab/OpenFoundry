package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/scheduler"
)

type fakeClock struct{ t time.Time }

func (f *fakeClock) Now() time.Time { return f.t }

// Tick happy path: a `* * * * *` workflow whose next_run_at has
// elapsed advances by exactly one minute, gets handed to Dispatcher
// once, and the transaction commits.
func TestTickDispatchesDueWorkflowAndAdvancesNextRunAt(t *testing.T) {
	t.Parallel()

	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: now}

	workflowID := uuid.New()
	expectedNext := time.Date(2026, 5, 17, 12, 1, 0, 0, time.UTC)

	pool.ExpectBegin()
	pool.ExpectQuery(`FROM workflows`).
		WithArgs(now, 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "cron_expr"}).
			AddRow(workflowID, "* * * * *"))
	pool.ExpectExec(`UPDATE workflows SET next_run_at`).
		WithArgs(workflowID, expectedNext).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	loadCalls := 0
	dispatchCalls := 0
	var dispatched *models.WorkflowDefinition

	sched := scheduler.New(
		pool,
		clock,
		func(_ context.Context, id uuid.UUID) (*models.WorkflowDefinition, error) {
			loadCalls++
			require.Equal(t, workflowID, id)
			return &models.WorkflowDefinition{ID: id, Status: "active", TriggerType: "schedule"}, nil
		},
		func(_ context.Context, w *models.WorkflowDefinition) (*models.WorkflowRun, error) {
			dispatchCalls++
			dispatched = w
			return &models.WorkflowRun{WorkflowID: w.ID}, nil
		},
		nil,
	)

	require.NoError(t, sched.Tick(context.Background()))
	assert.Equal(t, 1, loadCalls, "Load must be invoked exactly once per due workflow")
	assert.Equal(t, 1, dispatchCalls, "Dispatch must be invoked exactly once per due workflow")
	require.NotNil(t, dispatched)
	assert.Equal(t, workflowID, dispatched.ID)
	require.NoError(t, pool.ExpectationsWereMet())
}

// Empty due set must still open + commit the claim transaction
// (consistent SKIP LOCKED behaviour) but never invoke Load/Dispatch.
func TestTickIsNoOpWhenNothingDue(t *testing.T) {
	t.Parallel()

	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	clock := &fakeClock{t: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)}

	pool.ExpectBegin()
	pool.ExpectQuery(`FROM workflows`).
		WithArgs(clock.Now(), 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "cron_expr"}))
	pool.ExpectCommit()

	dispatchCalls := 0
	sched := scheduler.New(
		pool,
		clock,
		func(_ context.Context, _ uuid.UUID) (*models.WorkflowDefinition, error) {
			t.Fatal("Load must not be called when no rows are due")
			return nil, nil
		},
		func(_ context.Context, _ *models.WorkflowDefinition) (*models.WorkflowRun, error) {
			dispatchCalls++
			return nil, nil
		},
		nil,
	)

	require.NoError(t, sched.Tick(context.Background()))
	assert.Zero(t, dispatchCalls)
	require.NoError(t, pool.ExpectationsWereMet())
}

// Unparseable cron must pause the workflow (NULL next_run_at) instead
// of looping every tick on a bad expression. Dispatcher is never
// invoked.
func TestTickPausesWorkflowWithInvalidCron(t *testing.T) {
	t.Parallel()

	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{t: now}
	workflowID := uuid.New()

	pool.ExpectBegin()
	pool.ExpectQuery(`FROM workflows`).
		WithArgs(now, 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "cron_expr"}).
			AddRow(workflowID, "totally not a cron"))
	pool.ExpectExec(`UPDATE workflows SET next_run_at = NULL`).
		WithArgs(workflowID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	pool.ExpectCommit()

	sched := scheduler.New(
		pool,
		clock,
		func(_ context.Context, _ uuid.UUID) (*models.WorkflowDefinition, error) {
			t.Fatal("Load must not be called for invalid-cron workflows")
			return nil, nil
		},
		func(_ context.Context, _ *models.WorkflowDefinition) (*models.WorkflowRun, error) {
			t.Fatal("Dispatch must not be called for invalid-cron workflows")
			return nil, nil
		},
		nil,
	)

	require.NoError(t, sched.Tick(context.Background()))
	require.NoError(t, pool.ExpectationsWereMet())
}

// Run() exits cleanly when its context is cancelled — guards against
// the goroutine being orphaned on shutdown.
func TestRunExitsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer pool.Close()

	clock := &fakeClock{t: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)}

	// First tick (called eagerly inside Run) sees no due rows; the
	// ticker is wound up beyond the test's runtime so the second
	// tick never fires before cancel reaches the select.
	pool.ExpectBegin()
	pool.ExpectQuery(`FROM workflows`).
		WithArgs(clock.Now(), 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "cron_expr"}))
	pool.ExpectCommit()

	sched := scheduler.New(
		pool, clock,
		func(_ context.Context, _ uuid.UUID) (*models.WorkflowDefinition, error) { return nil, nil },
		func(_ context.Context, _ *models.WorkflowDefinition) (*models.WorkflowRun, error) { return nil, nil },
		nil,
	)
	sched.TickInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sched.Run(ctx) }()

	// Give the eager tick a moment to commit before we cancel — the
	// goroutine then parks on the ticker's select and exits.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.True(t, errors.Is(err, context.Canceled))
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s of context cancellation")
	}
	require.NoError(t, pool.ExpectationsWereMet())
}
