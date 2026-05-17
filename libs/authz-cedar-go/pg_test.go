package cedarauthz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// PolicyTable name validation (rejects identifiers that could form a
// SQL injection vector — every char must be alnum or underscore).
func TestPgPolicyStoreRejectsBadTableNames(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	cases := []string{
		"",
		"cedar policies",      // space
		`cedar"policies`,      // quote
		"cedar;DROP TABLE x;", // semicolon
		"cedar-policies",      // hyphen (not underscore)
	}
	for _, name := range cases {
		pg := cedarauthz.NewPgPolicyStore(mock, store).WithTable(name)
		_, err := pg.Reload(context.Background())
		require.Error(t, err, "table name %q must be rejected", name)
		assert.True(t, errors.Is(err, cedarauthz.ErrBackend), "error: %v", err)
	}
}

func TestPgPolicyStoreReloadEmptyTable(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	mock.ExpectQuery(`SELECT id, version, source, description`).
		WillReturnRows(pgxmock.NewRows([]string{"id", "version", "source", "description"}))

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	count, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.True(t, store.IsEmpty())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgPolicyStoreReloadHydratesAndSwapsAtomically(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	desc := "cleared readers permitted"
	rows := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow(
			"permit-cleared-readers",
			int32(1),
			`permit(
				principal,
				action == Action::"read",
				resource is Dataset
			) when {
				principal.clearances.containsAll(resource.markings)
			};`,
			&desc,
		)
	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(rows)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	count, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, store.Len())
	require.NoError(t, mock.ExpectationsWereMet())
}

// A second Reload with a different row set must atomically replace the
// previous bundle (matches the Rust idempotent contract).
func TestPgPolicyStoreReloadIsIdempotent(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	src := `permit(principal, action == Action::"read", resource is Dataset);`
	first := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("p1", int32(1), src, (*string)(nil))
	second := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("p2", int32(1), src, (*string)(nil)).
		AddRow("p3", int32(1), src, (*string)(nil))

	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(first)
	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(second)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	c1, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, c1)
	assert.Equal(t, 1, store.Len())

	c2, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, c2)
	assert.Equal(t, 2, store.Len())
	require.NoError(t, mock.ExpectationsWereMet())
}

// Bad SQL bubbles up as ErrBackend, store stays unchanged.
func TestPgPolicyStoreReloadPropagatesBackendError(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	mock.ExpectQuery(`SELECT id, version, source, description`).
		WillReturnError(errors.New("connection refused"))

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	_, err = pg.Reload(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, cedarauthz.ErrBackend))
	assert.True(t, store.IsEmpty(), "failure must not leave a partial state")
}

// Reload with a malformed cedar policy bubbles up as PolicyParseError
// and never swaps the active set.
func TestPgPolicyStoreReloadFailsOnInvalidCedar(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewWithPolicies([]cedarauthz.PolicyRecord{
		{ID: "p1", Source: `permit(principal, action == Action::"read", resource is Dataset);`},
	})
	require.NoError(t, err)
	require.Equal(t, 1, store.Len())

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	bad := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("broken", int32(1), "this is not cedar", (*string)(nil))
	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(bad)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	_, err = pg.Reload(context.Background())
	require.Error(t, err)
	var ppe *cedarauthz.PolicyParseError
	require.True(t, errors.As(err, &ppe), "want PolicyParseError, got %T", err)
	assert.Equal(t, "broken", ppe.ID)
	// Active set is preserved — the bad bundle was never swapped in.
	assert.Equal(t, 1, store.Len(), "failed reload preserves previous policies")
}

// Tenant-scoped Reload narrows the WHERE clause to that tenant plus
// globals. Mirrors the multi-tenant guarantee the policy-service repo
// enforces on the write path.
func TestPgPolicyStoreReloadFiltersByTenant(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	tenant := uuid.New()
	rows := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("p-tenant", int32(1),
			`permit(principal, action == Action::"read", resource is Dataset);`,
			(*string)(nil))
	mock.ExpectQuery(`tenant_id = \$1 OR tenant_id IS NULL`).
		WithArgs(tenant).
		WillReturnRows(rows)

	pg := cedarauthz.NewPgPolicyStore(mock, store).WithTenantID(&tenant)
	count, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

// Platform (nil tenant) reload only loads global rows — no tenant data
// leaks into an admin engine.
func TestPgPolicyStoreReloadPlatformOnlyLoadsGlobals(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	rows := pgxmock.NewRows([]string{"id", "version", "source", "description"})
	mock.ExpectQuery(`tenant_id IS NULL`).
		WillReturnRows(rows)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	count, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TenantPolicyCache hands out per-tenant stores so two tenants never
// share a PolicySet pointer — the only way to make engine evaluation
// safe under cross-tenant traffic.
func TestTenantPolicyCacheIsolatesTenants(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	tenantA := uuid.New()
	tenantB := uuid.New()

	src := `permit(principal, action == Action::"read", resource is Dataset);`
	mock.ExpectQuery(`tenant_id = \$1 OR tenant_id IS NULL`).
		WithArgs(tenantA).
		WillReturnRows(pgxmock.NewRows([]string{"id", "version", "source", "description"}).
			AddRow("p-a", int32(1), src, (*string)(nil)))
	mock.ExpectQuery(`tenant_id = \$1 OR tenant_id IS NULL`).
		WithArgs(tenantB).
		WillReturnRows(pgxmock.NewRows([]string{"id", "version", "source", "description"}).
			AddRow("p-b1", int32(1), src, (*string)(nil)).
			AddRow("p-b2", int32(1), src, (*string)(nil)))

	cache := cedarauthz.NewTenantPolicyCache(mock)
	storeA, vA, err := cache.Get(context.Background(), &tenantA)
	require.NoError(t, err)
	require.NotNil(t, storeA)
	assert.Equal(t, 1, storeA.Len())
	assert.Equal(t, uint64(1), vA)

	storeB, vB, err := cache.Get(context.Background(), &tenantB)
	require.NoError(t, err)
	require.NotNil(t, storeB)
	assert.Equal(t, 2, storeB.Len())
	assert.Equal(t, uint64(1), vB)

	assert.NotSame(t, storeA, storeB, "each tenant must own its own PolicyStore handle")
	require.NoError(t, mock.ExpectationsWereMet())
}

// A second Get for the same tenant returns the cached store without
// re-querying. Confirms the (tenantID → store) entry is sticky until
// invalidated.
func TestTenantPolicyCacheReusesCachedStore(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	tenant := uuid.New()
	src := `permit(principal, action == Action::"read", resource is Dataset);`
	mock.ExpectQuery(`tenant_id = \$1 OR tenant_id IS NULL`).
		WithArgs(tenant).
		WillReturnRows(pgxmock.NewRows([]string{"id", "version", "source", "description"}).
			AddRow("p1", int32(1), src, (*string)(nil)))

	cache := cedarauthz.NewTenantPolicyCache(mock)
	first, v1, err := cache.Get(context.Background(), &tenant)
	require.NoError(t, err)

	second, v2, err := cache.Get(context.Background(), &tenant)
	require.NoError(t, err)
	assert.Same(t, first, second, "second Get must return the cached store")
	assert.Equal(t, v1, v2, "version token stable until Reload/Invalidate")

	require.NoError(t, mock.ExpectationsWereMet())
}

// Reload bumps the version token so callers can detect cache flips.
// Invalidate drops the entry so the next Get re-fetches.
func TestTenantPolicyCacheReloadAndInvalidate(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	tenant := uuid.New()
	src := `permit(principal, action == Action::"read", resource is Dataset);`
	// Three expected queries: initial Get, explicit Reload, post-Invalidate Get.
	for i := 0; i < 3; i++ {
		mock.ExpectQuery(`tenant_id = \$1 OR tenant_id IS NULL`).
			WithArgs(tenant).
			WillReturnRows(pgxmock.NewRows([]string{"id", "version", "source", "description"}).
				AddRow("p1", int32(1), src, (*string)(nil)))
	}

	cache := cedarauthz.NewTenantPolicyCache(mock)
	_, v1, err := cache.Get(context.Background(), &tenant)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), v1)

	_, v2, err := cache.Reload(context.Background(), &tenant)
	require.NoError(t, err)
	assert.Greater(t, v2, v1, "reload bumps the version token")

	cache.Invalidate(&tenant)
	_, v3, err := cache.Get(context.Background(), &tenant)
	require.NoError(t, err)
	assert.NotEqual(t, v2, v3, "post-invalidate Get rebuilds the entry and resets the token")

	require.NoError(t, mock.ExpectationsWereMet())
}

// Smoke test for context cancellation propagation.
func TestPgPolicyStoreReloadHonoursContext(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	mock.ExpectQuery(`SELECT id, version, source, description`).
		WillReturnError(context.DeadlineExceeded)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	_, err = pg.Reload(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, cedarauthz.ErrBackend))
}
