//go:build integration

// Integration coverage for the Postgres-backed function-invocations
// repository. Until the pgx-backed implementation lands (CM.10+), the
// integration build tag exercises the in-memory store through the same
// public Repository contract so the surface stays stable. Once the
// real Postgres repo lands the body can be replaced with a
// testcontainers-driven driver — the table-driven assertions below
// already work against any Repository implementation.
package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/repo"
)

func newRepo(t *testing.T) repo.Repository {
	t.Helper()
	return repo.NewMemoryRepository()
}

func TestInvocationLifecycle_Integration(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	in := function.FunctionInvocation{
		ModuleID:     uuid.New(),
		FunctionName: "echo",
		Payload:      json.RawMessage(`{"k":"v"}`),
		TenantID:     uuid.New(),
		ActorID:      uuid.New(),
		ScheduledAt:  time.Now().UTC(),
		Status:       function.StatusQueued,
	}
	row, err := r.CreateInvocation(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.Status != function.StatusQueued {
		t.Fatalf("expected queued, got %q", row.Status)
	}
	running, err := r.MarkInvocationRunning(ctx, row.ID)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if running.Status != function.StatusRunning || running.StartedAt == nil {
		t.Fatalf("running state wrong: %+v", running)
	}
	done, err := r.CompleteInvocation(ctx, row.ID, repo.InvocationCompletion{
		Status:    function.StatusSucceeded,
		Result:    []byte(`{"out":1}`),
		CostUnits: 42,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.Status != function.StatusSucceeded || done.CostUnits != 42 || done.FinishedAt == nil {
		t.Fatalf("terminal state wrong: %+v", done)
	}
	// Re-cancelling a terminal row must fail.
	if _, err := r.CancelInvocation(ctx, row.ID, uuid.New()); !errors.Is(err, function.ErrInvocationTerminal) {
		t.Fatalf("expected ErrInvocationTerminal, got %v", err)
	}
}

func TestInvocationListFilter_Integration(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()

	mod := uuid.New()
	tenant := uuid.New()
	for i := 0; i < 3; i++ {
		_, err := r.CreateInvocation(ctx, function.FunctionInvocation{
			ModuleID:     mod,
			FunctionName: "echo",
			TenantID:     tenant,
			ActorID:      uuid.New(),
			Status:       function.StatusQueued,
			ScheduledAt:  time.Now().UTC().Add(time.Duration(i) * time.Millisecond),
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	// One row for a different tenant must not leak across.
	_, err := r.CreateInvocation(ctx, function.FunctionInvocation{
		ModuleID:     mod,
		FunctionName: "echo",
		TenantID:     uuid.New(),
		ActorID:      uuid.New(),
		Status:       function.StatusQueued,
		ScheduledAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create cross-tenant: %v", err)
	}

	res, err := r.ListInvocations(ctx, repo.InvocationFilter{TenantID: &tenant}, repo.Page{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("expected 3 items scoped to tenant, got %d", len(res.Items))
	}
}
