//go:build integration

// Tenant-isolation invariants for the cedar_policies repo surface.
// Boots postgres:16-alpine via libs/testing.BootPostgres, applies the
// embedded migrations, and exercises the (tenantID, id) write/read
// rules documented on the repo's cedar block:
//
//   - tenant A can list/get its own rows
//   - tenant A can list global rows (tenant_id IS NULL)
//   - tenant B cannot list/get/update/delete tenant A's rows
//   - the platform (nil tenantID) view excludes tenant rows
//   - Create stamps the row with the supplied tenant exactly
//
// Opt-in via `go test -tags=integration ./...`.
package repo

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

func bootCedarRepo(t *testing.T) *Repo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, Migrate(ctx, h.Pool))
	return &Repo{Pool: h.Pool}
}

func newCreate(id string) *models.CreateCedarPolicyRequest {
	return &models.CreateCedarPolicyRequest{
		ID:     id,
		Source: `permit(principal, action, resource);`,
	}
}

func TestCedarPolicyTenantIsolation(t *testing.T) {
	ctx := context.Background()
	r := bootCedarRepo(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	caller := uuid.New()

	a1, err := r.CreateCedarPolicy(ctx, newCreate("a-policy-1"), caller, &tenantA)
	require.NoError(t, err)
	require.NotNil(t, a1.TenantID)
	assert.Equal(t, tenantA, *a1.TenantID, "create stamps the tenant from the seal, not the body")

	b1, err := r.CreateCedarPolicy(ctx, newCreate("b-policy-1"), caller, &tenantB)
	require.NoError(t, err)
	require.NotNil(t, b1.TenantID)
	assert.Equal(t, tenantB, *b1.TenantID)

	g1, err := r.CreateCedarPolicy(ctx, newCreate("global-1"), caller, nil)
	require.NoError(t, err)
	assert.Nil(t, g1.TenantID, "nil tenantID writes a global row")

	// ListCedarPolicies for tenant A returns A's rows + globals, never B's.
	listA, err := r.ListCedarPolicies(ctx, &tenantA)
	require.NoError(t, err)
	idsA := collectIDs(listA)
	assert.ElementsMatch(t, []string{"a-policy-1", "global-1"}, idsA)

	listB, err := r.ListCedarPolicies(ctx, &tenantB)
	require.NoError(t, err)
	idsB := collectIDs(listB)
	assert.ElementsMatch(t, []string{"b-policy-1", "global-1"}, idsB)

	// Platform/admin view (nil tenantID) sees only globals — never a
	// tenant row, so an unset OrgID claim can't leak data across the
	// platform boundary.
	listPlatform, err := r.ListCedarPolicies(ctx, nil)
	require.NoError(t, err)
	idsPlatform := collectIDs(listPlatform)
	assert.ElementsMatch(t, []string{"global-1"}, idsPlatform)

	// Get under the wrong tenant returns (nil, nil) — a 404, not a 403,
	// keeps cross-tenant probing from leaking the existence of a row.
	got, err := r.GetCedarPolicy(ctx, &tenantB, "a-policy-1")
	require.NoError(t, err)
	assert.Nil(t, got, "tenant B must not be able to read tenant A's row")

	got, err = r.GetCedarPolicy(ctx, &tenantA, "a-policy-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "a-policy-1", got.ID)

	got, err = r.GetCedarPolicy(ctx, &tenantA, "global-1")
	require.NoError(t, err)
	require.NotNil(t, got, "tenants must be able to read global rows")
}

func TestCedarPolicyUpdateScopedToTenant(t *testing.T) {
	ctx := context.Background()
	r := bootCedarRepo(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	caller := uuid.New()

	_, err := r.CreateCedarPolicy(ctx, newCreate("p-only-a"), caller, &tenantA)
	require.NoError(t, err)

	newSource := `permit(principal, action == Action::"read", resource);`
	patch := &models.UpdateCedarPolicyRequest{Source: &newSource}

	// Cross-tenant update: same id, wrong tenant → no-op, (nil, nil).
	got, err := r.UpdateCedarPolicy(ctx, &tenantB, "p-only-a", patch)
	require.NoError(t, err)
	assert.Nil(t, got, "tenant B must not be able to update tenant A's row")

	// Confirm the row is unchanged: source still the original.
	current, err := r.GetCedarPolicy(ctx, &tenantA, "p-only-a")
	require.NoError(t, err)
	require.NotNil(t, current)
	assert.Equal(t, `permit(principal, action, resource);`, current.Source)
	assert.Equal(t, int32(1), current.Version)

	// In-tenant update: succeeds and bumps the version (source changed).
	got, err = r.UpdateCedarPolicy(ctx, &tenantA, "p-only-a", patch)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, newSource, got.Source)
	assert.Equal(t, int32(2), got.Version)
}

func TestCedarPolicyDeleteScopedToTenant(t *testing.T) {
	ctx := context.Background()
	r := bootCedarRepo(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	caller := uuid.New()

	_, err := r.CreateCedarPolicy(ctx, newCreate("p-only-a"), caller, &tenantA)
	require.NoError(t, err)

	deleted, err := r.DeleteCedarPolicy(ctx, &tenantB, "p-only-a")
	require.NoError(t, err)
	assert.False(t, deleted, "tenant B must not be able to delete tenant A's row")

	// Row still exists for tenant A.
	got, err := r.GetCedarPolicy(ctx, &tenantA, "p-only-a")
	require.NoError(t, err)
	require.NotNil(t, got)

	// And tenant A's own delete works.
	deleted, err = r.DeleteCedarPolicy(ctx, &tenantA, "p-only-a")
	require.NoError(t, err)
	assert.True(t, deleted)

	got, err = r.GetCedarPolicy(ctx, &tenantA, "p-only-a")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// Globals can only be edited from the platform/admin (nil tenant)
// boundary — a tenant write context must NOT be able to mutate a
// global row even though it can read it.
func TestCedarPolicyGlobalsWritableOnlyByPlatform(t *testing.T) {
	ctx := context.Background()
	r := bootCedarRepo(t)

	tenant := uuid.New()
	caller := uuid.New()

	_, err := r.CreateCedarPolicy(ctx, newCreate("global-policy"), caller, nil)
	require.NoError(t, err)

	src := `permit(principal, action == Action::"read", resource);`
	patch := &models.UpdateCedarPolicyRequest{Source: &src}

	// Tenant tries to mutate the global → no-op.
	got, err := r.UpdateCedarPolicy(ctx, &tenant, "global-policy", patch)
	require.NoError(t, err)
	assert.Nil(t, got, "tenant write context must not be able to mutate a global")

	// And tenant tries to delete it → no-op.
	deleted, err := r.DeleteCedarPolicy(ctx, &tenant, "global-policy")
	require.NoError(t, err)
	assert.False(t, deleted, "tenant write context must not be able to delete a global")

	// Platform delete still works.
	deleted, err = r.DeleteCedarPolicy(ctx, nil, "global-policy")
	require.NoError(t, err)
	assert.True(t, deleted)
}

func collectIDs(items []models.CedarPolicy) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
