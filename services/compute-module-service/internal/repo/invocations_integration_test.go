//go:build integration

// Integration coverage for the Postgres-backed function-invocations
// repository. Uses libs/testing.BootPostgres to spin up an ephemeral
// postgres:16-alpine container, applies the embedded migrations, and
// exercises the public Repository contract.
package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/repo"
)

func newRepo(t *testing.T) (repo.Repository, context.Context) {
	t.Helper()
	ctx := context.Background()
	h := testingx.BootPostgres(ctx, t)
	if err := repo.Migrate(ctx, h.Pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return repo.NewPgRepository(h.Pool), ctx
}

func TestInvocationLifecycle_Integration(t *testing.T) {
	r, ctx := newRepo(t)

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
	if _, err := r.CancelInvocation(ctx, row.ID, uuid.New()); !errors.Is(err, function.ErrInvocationTerminal) {
		t.Fatalf("expected ErrInvocationTerminal, got %v", err)
	}
}

func TestInvocationListFilter_Integration(t *testing.T) {
	r, ctx := newRepo(t)

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

func TestComputeModuleCRUD_Integration(t *testing.T) {
	r, ctx := newRepo(t)

	project := uuid.New()
	actor := uuid.New()
	created, err := r.Create(ctx, models.CreateParams{
		Name:          "Echo Module",
		ProjectID:     project,
		ExecutionMode: models.ExecutionModeFunction,
		Actor:         actor,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.State != models.LifecycleActive {
		t.Fatalf("expected active state, got %q", created.State)
	}

	// Same name in same folder → conflict.
	if _, err := r.Create(ctx, models.CreateParams{
		Name:          "echo module",
		ProjectID:     project,
		ExecutionMode: models.ExecutionModeFunction,
		Actor:         actor,
	}); !errors.Is(err, repo.ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict, got %v", err)
	}

	newName := "Echo v2"
	updated, err := r.UpdateMetadata(ctx, created.ID, models.UpdateMetadataParams{
		Name:  &newName,
		Actor: actor,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != newName {
		t.Fatalf("expected name %q, got %q", newName, updated.Name)
	}

	archived, err := r.Archive(ctx, created.ID, actor)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if !archived.IsArchived() {
		t.Fatalf("expected archived state, got %q", archived.State)
	}
	if _, err := r.Archive(ctx, created.ID, actor); !errors.Is(err, repo.ErrAlreadyArchived) {
		t.Fatalf("re-archive should return ErrAlreadyArchived, got %v", err)
	}

	restored, err := r.Restore(ctx, created.ID, actor)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.IsArchived() {
		t.Fatalf("expected active after restore")
	}

	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.Get(ctx, created.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
