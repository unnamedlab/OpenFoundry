//go:build integration

package repo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

// bootRepo spins up a Postgres testcontainer, applies the cipher-service
// migrations, and hands back a Repo wired to the resulting pool.
func bootRepo(t *testing.T) (*Repo, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	h := testingx.BootPostgres(ctx, t)
	if err := Migrate(ctx, h.Pool); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return New(h.Pool), ctx
}

// TestRepo_InsertGetList covers the happy path: create a key, fetch it
// by id, list it back, and verify cross-tenant isolation.
func TestRepo_InsertGetList(t *testing.T) {
	r, ctx := bootRepo(t)
	tenantA := uuid.New()
	tenantB := uuid.New()

	params := CreateKeyParams{
		ID:                 uuid.New(),
		TenantID:           tenantA,
		Alias:              "pii-default",
		Algorithm:          domain.AlgorithmAES256GCM,
		WrappedKeyMaterial: []byte("wrapped-dek-bytes-go-here"),
		KMSKeyRef:          "local:test",
	}
	created, err := r.InsertKey(ctx, params)
	if err != nil {
		t.Fatalf("InsertKey: %v", err)
	}
	if created.Status != domain.StatusActive || created.Version != 1 {
		t.Fatalf("created has wrong defaults: %+v", created)
	}

	got, err := r.GetKey(ctx, tenantA, created.ID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if got.Alias != "pii-default" {
		t.Fatalf("alias = %q", got.Alias)
	}

	// Cross-tenant read must surface ErrKeyNotFound, not the row.
	if _, err := r.GetKey(ctx, tenantB, created.ID); !errors.Is(err, domain.ErrKeyNotFound) {
		t.Fatalf("cross-tenant get must yield ErrKeyNotFound, got %v", err)
	}

	res, err := r.ListKeys(ctx, tenantA, ListPage{Limit: 10})
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != created.ID {
		t.Fatalf("ListKeys returned %+v", res.Items)
	}
}

// TestRepo_AliasConflict pins the (tenant, alias) uniqueness contract.
func TestRepo_AliasConflict(t *testing.T) {
	r, ctx := bootRepo(t)
	tenant := uuid.New()

	base := CreateKeyParams{
		ID:                 uuid.New(),
		TenantID:           tenant,
		Alias:              "dup",
		Algorithm:          domain.AlgorithmAES256GCM,
		WrappedKeyMaterial: []byte("v1"),
		KMSKeyRef:          "local:test",
	}
	if _, err := r.InsertKey(ctx, base); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	base.ID = uuid.New()
	_, err := r.InsertKey(ctx, base)
	if !errors.Is(err, ErrAliasConflict) {
		t.Fatalf("expected ErrAliasConflict, got %v", err)
	}
}

// TestRepo_Rotate appends versions and pins the active version pointer.
func TestRepo_Rotate(t *testing.T) {
	r, ctx := bootRepo(t)
	tenant := uuid.New()

	created, err := r.InsertKey(ctx, CreateKeyParams{
		ID: uuid.New(), TenantID: tenant, Alias: "rot",
		Algorithm: domain.AlgorithmAES256GCM, WrappedKeyMaterial: []byte("v1"), KMSKeyRef: "local:test",
	})
	if err != nil {
		t.Fatalf("InsertKey: %v", err)
	}

	rotated, err := r.RotateKey(ctx, tenant, created.ID, []byte("v2-wrapped"), "local:test")
	if err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if rotated.Version != 2 {
		t.Fatalf("version = %d, want 2", rotated.Version)
	}
	if rotated.Status != domain.StatusRotating {
		t.Fatalf("status = %s, want rotating", rotated.Status)
	}

	v1, err := r.GetVersion(ctx, tenant, created.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion v1: %v", err)
	}
	if v1.RetiredAt == nil {
		t.Fatal("v1 must be marked retired after rotation")
	}

	if err := r.MarkRotationComplete(ctx, tenant, created.ID); err != nil {
		t.Fatalf("MarkRotationComplete: %v", err)
	}
	settled, err := r.GetKey(ctx, tenant, created.ID)
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if settled.Status != domain.StatusActive {
		t.Fatalf("status after rotation completion = %s", settled.Status)
	}
}

// TestRepo_Retire flips the status flag and refuses subsequent rotations.
func TestRepo_Retire(t *testing.T) {
	r, ctx := bootRepo(t)
	tenant := uuid.New()

	created, err := r.InsertKey(ctx, CreateKeyParams{
		ID: uuid.New(), TenantID: tenant, Alias: "ret",
		Algorithm: domain.AlgorithmAES256GCM, WrappedKeyMaterial: []byte("v1"), KMSKeyRef: "local:test",
	})
	if err != nil {
		t.Fatalf("InsertKey: %v", err)
	}

	got, err := r.RetireKey(ctx, tenant, created.ID)
	if err != nil {
		t.Fatalf("RetireKey: %v", err)
	}
	if got.Status != domain.StatusRetired {
		t.Fatalf("status = %s, want retired", got.Status)
	}

	if _, err := r.RotateKey(ctx, tenant, created.ID, []byte("vN"), "local:test"); !errors.Is(err, domain.ErrKeyRetired) {
		t.Fatalf("rotate after retire must fail with ErrKeyRetired, got %v", err)
	}

	// Retiring twice is a no-op, not an error.
	if _, err := r.RetireKey(ctx, tenant, created.ID); err != nil {
		t.Fatalf("idempotent retire: %v", err)
	}
}
