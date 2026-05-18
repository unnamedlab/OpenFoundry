package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/repo"
)

func TestMemoryStore_DefinitionLifecycle(t *testing.T) {
	t.Parallel()
	s := repo.NewMemoryStore()
	ctx := context.Background()
	tenant := ids.New()
	fn := &models.FunctionDefinition{
		TenantID:  tenant,
		Namespace: "billing",
		Name:      "compute_invoice",
		Runtime:   models.RuntimeTypeScript,
		Status:    models.StatusDraft,
	}
	if err := s.CreateFunction(ctx, fn); err != nil {
		t.Fatalf("create: %v", err)
	}
	if fn.ID == uuid.Nil {
		t.Fatal("expected mint of ID")
	}

	// Duplicate (tenant, namespace, name) must collide.
	dup := *fn
	dup.ID = uuid.Nil
	if err := s.CreateFunction(ctx, &dup); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	got, err := s.GetFunction(ctx, tenant, fn.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "compute_invoice" {
		t.Fatalf("unexpected name: %s", got.Name)
	}

	// Tenant isolation.
	if _, err := s.GetFunction(ctx, ids.New(), fn.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound across tenants, got %v", err)
	}
}

func TestMemoryStore_VersionAndActivation(t *testing.T) {
	t.Parallel()
	s := repo.NewMemoryStore()
	ctx := context.Background()
	tenant := ids.New()
	fn := &models.FunctionDefinition{
		TenantID:  tenant,
		Namespace: "ml",
		Name:      "embed",
		Runtime:   models.RuntimePython,
	}
	if err := s.CreateFunction(ctx, fn); err != nil {
		t.Fatalf("create: %v", err)
	}
	v1, err := s.AppendVersion(ctx, tenant, fn.ID, "inline:print('1')", "main")
	if err != nil {
		t.Fatalf("append v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected v1=1, got %d", v1.Version)
	}
	v2, err := s.AppendVersion(ctx, tenant, fn.ID, "inline:print('2')", "main")
	if err != nil {
		t.Fatalf("append v2: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected v2=2, got %d", v2.Version)
	}

	updated, err := s.UpdateFunctionStatus(ctx, tenant, fn.ID, models.StatusActive, intPtr(2))
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if updated.Status != models.StatusActive || updated.ActiveVersion == nil || *updated.ActiveVersion != 2 {
		t.Fatalf("unexpected after activate: %+v", updated)
	}
	if updated.ActivatedAt == nil {
		t.Fatal("expected ActivatedAt to be set")
	}

	versions, err := s.ListVersions(ctx, fn.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != 2 {
		t.Fatalf("ListVersions should return desc by version, got %+v", versions)
	}

	// Lookup unknown version surfaces the sentinel.
	if _, err := s.GetVersion(ctx, fn.ID, 99); !errors.Is(err, domain.ErrVersionNotFound) {
		t.Fatalf("expected ErrVersionNotFound, got %v", err)
	}
}

func TestMemoryStore_RunLifecycle(t *testing.T) {
	t.Parallel()
	s := repo.NewMemoryStore()
	ctx := context.Background()
	tenant := ids.New()
	fn := &models.FunctionDefinition{
		TenantID:  tenant,
		Namespace: "ns",
		Name:      "f",
		Runtime:   models.RuntimeTypeScript,
	}
	if err := s.CreateFunction(ctx, fn); err != nil {
		t.Fatalf("create fn: %v", err)
	}
	run := &models.FunctionRun{
		FunctionID:      fn.ID,
		FunctionVersion: 1,
		TenantID:        tenant,
		ActorID:         ids.New(),
		Status:          models.RunStatusRunning,
		Input:           []byte(`{"x":1}`),
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	finished, err := s.FinishRun(ctx, run.ID, repo.RunUpdate{
		Status:     models.RunStatusSucceeded,
		Output:     []byte(`{"ok":true}`),
		DurationMs: 12,
	})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Status != models.RunStatusSucceeded || string(finished.Output) != `{"ok":true}` || finished.DurationMs != 12 || finished.FinishedAt == nil {
		t.Fatalf("unexpected finished run: %+v", finished)
	}

	got, err := s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != models.RunStatusSucceeded {
		t.Fatalf("get run status: %s", got.Status)
	}

	list, err := s.ListRuns(ctx, repo.ListRunsFilter{TenantID: tenant, Limit: 10})
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list runs: want 1, got %d", len(list))
	}

	// Status filter narrows results.
	none, err := s.ListRuns(ctx, repo.ListRunsFilter{TenantID: tenant, Status: models.RunStatusFailed})
	if err != nil {
		t.Fatalf("filter: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected 0 failed runs, got %d", len(none))
	}
}

func intPtr(i int) *int { return &i }
