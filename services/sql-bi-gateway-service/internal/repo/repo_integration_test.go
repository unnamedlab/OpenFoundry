//go:build integration

package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/repo"
)

// TestSavedQueriesRepoRoundTrip exercises Migrate + Create + List +
// Delete against a real postgres:16-alpine container.
func TestSavedQueriesRepoRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg := testingx.BootPostgres(ctx, t)

	if err := repo.Migrate(ctx, pg.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	r := repo.NewSavedQueries(pg.Pool)
	owner := uuid.New()

	created, err := r.Create(ctx, models.SavedQuery{
		Name:        "top-tenants",
		Description: "fixture",
		SQL:         "SELECT 1",
		OwnerID:     owner,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatalf("create did not allocate id")
	}
	if created.OwnerID != owner {
		t.Fatalf("owner: got %s want %s", created.OwnerID, owner)
	}

	list, err := r.List(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("list: got %+v", list)
	}

	// search by name
	hit, err := r.List(ctx, "top", 10, 0)
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if len(hit) != 1 {
		t.Fatalf("search by name: got %d", len(hit))
	}
	miss, err := r.List(ctx, "no-match-zzz", 10, 0)
	if err != nil {
		t.Fatalf("list miss: %v", err)
	}
	if len(miss) != 0 {
		t.Fatalf("miss: got %d", len(miss))
	}

	if err := r.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.Delete(ctx, created.ID); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("delete twice: got %v want ErrNotFound", err)
	}

	left, err := r.List(ctx, "", 10, 0)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("list after delete: got %d", len(left))
	}
}

func TestSavedQueriesRepoValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg := testingx.BootPostgres(ctx, t)
	if err := repo.Migrate(ctx, pg.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	r := repo.NewSavedQueries(pg.Pool)

	if _, err := r.Create(ctx, models.SavedQuery{SQL: "SELECT 1"}); !errors.Is(err, repo.ErrValidation) {
		t.Fatalf("missing name: got %v", err)
	}
	if _, err := r.Create(ctx, models.SavedQuery{Name: "x"}); !errors.Is(err, repo.ErrValidation) {
		t.Fatalf("missing sql: got %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pg := testingx.BootPostgres(ctx, t)

	for i := 0; i < 3; i++ {
		if err := repo.Migrate(ctx, pg.Pool); err != nil {
			t.Fatalf("migrate %d: %v", i, err)
		}
	}
}
