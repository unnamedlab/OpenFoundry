//go:build integration

// Integration-level proof that ListEnabledABACPoliciesMatching isolates
// rows by tenant against a real Postgres. The unit-level pgxmock test
// (abac_tenant_test.go) pins the SQL shape; this test pins the schema,
// the index, and the runtime behaviour end-to-end.
//
// Gated behind the `integration` build tag because it needs Docker for
// testcontainers — same convention as libs/python-sidecar.
package repo_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
)

// TestABACPoliciesAreTenantIsolated runs the bug scenario against a
// real Postgres: tenant A creates an enabled policy; tenant B's query
// for the same (resource, action) returns nothing.
func TestABACPoliciesAreTenantIsolated(t *testing.T) {
	ctx := context.Background()
	h := testingx.BootPostgres(ctx, t)

	require.NoError(t, repo.Migrate(ctx, h.Pool))

	r := &repo.Repo{Pool: h.Pool}

	tenantA := uuid.New()
	tenantB := uuid.New()
	caller := uuid.New()

	// Tenant A authors an enabled policy on (datasets, read).
	created, err := r.CreateABACPolicy(ctx,
		&models.CreateABACPolicyRequest{
			Name:     "tenant-a-allow",
			Effect:   "allow",
			Resource: "datasets",
			Action:   "read",
		},
		tenantA, caller,
	)
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, tenantA, created.TenantID)

	// Tenant A sees its own policy.
	got, err := r.ListEnabledABACPoliciesMatching(ctx, tenantA, "datasets", "read")
	require.NoError(t, err)
	require.Len(t, got, 1, "tenant A must see its own policy")
	assert.Equal(t, created.ID, got[0].ID)

	// Tenant B must not see tenant A's policy — this is the bug fix.
	got, err = r.ListEnabledABACPoliciesMatching(ctx, tenantB, "datasets", "read")
	require.NoError(t, err)
	assert.Empty(t, got, "tenant B must not see tenant A's policy")

	// CRUD cross-tenant probes: tenant B cannot fetch, update or delete
	// the row tenant A authored.
	p, err := r.GetABACPolicy(ctx, tenantB, created.ID)
	require.NoError(t, err)
	assert.Nil(t, p, "GetABACPolicy from another tenant must return nil")

	patched, err := r.UpdateABACPolicy(ctx, tenantB, created.ID,
		&models.UpdateABACPolicyRequest{Enabled: ptrBool(false)})
	require.NoError(t, err)
	assert.Nil(t, patched, "UpdateABACPolicy from another tenant must miss")

	deleted, err := r.DeleteABACPolicy(ctx, tenantB, created.ID)
	require.NoError(t, err)
	assert.False(t, deleted, "DeleteABACPolicy from another tenant must miss")

	// Sanity: tenant A's row is still intact + enabled.
	stillThere, err := r.GetABACPolicy(ctx, tenantA, created.ID)
	require.NoError(t, err)
	require.NotNil(t, stillThere)
	assert.True(t, stillThere.Enabled)
}

// TestABACPoliciesListUsesTenantIndex pins that the composite
// (tenant_id, enabled) partial index introduced in migration 0014
// exists after Migrate runs. We do not assert the plan — only that
// `idx_abac_policies_tenant_enabled` is present so re-runs / fresh
// dev databases keep the evaluator's hot path indexed.
func TestABACPoliciesListUsesTenantIndex(t *testing.T) {
	ctx := context.Background()
	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))

	var exists bool
	err := h.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM pg_indexes
		    WHERE schemaname = current_schema()
		      AND tablename = 'abac_policies'
		      AND indexname = 'idx_abac_policies_tenant_enabled'
		 )`).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "composite index idx_abac_policies_tenant_enabled must exist")
}

// TestABACPoliciesTenantIDDefaultIsDropped — after migration 0015
// the column must NOT carry a column default. An app-level bug that
// forgets to pass tenant_id should fail loudly, not silently fall
// back to the zero UUID.
func TestABACPoliciesTenantIDDefaultIsDropped(t *testing.T) {
	ctx := context.Background()
	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))

	var def *string
	err := h.Pool.QueryRow(ctx,
		`SELECT column_default FROM information_schema.columns
		  WHERE table_schema = current_schema()
		    AND table_name = 'abac_policies'
		    AND column_name = 'tenant_id'`).Scan(&def)
	require.NoError(t, err)
	assert.Nil(t, def, "tenant_id must not carry a column default after migration 0015")
}

func ptrBool(b bool) *bool { return &b }
