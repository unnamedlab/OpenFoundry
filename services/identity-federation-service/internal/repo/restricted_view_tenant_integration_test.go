//go:build integration

// Integration coverage for the per-tenant scoping the 0017 migration
// introduces on restricted_views. Verifies that every CRUD entry
// point in this repo honours the tenant boundary: cross-tenant Get
// returns nil, cross-tenant Update/Delete are silent no-ops, and a
// row created against tenantA is invisible to tenantB even when both
// share the same (resource, action).

package repo_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
)

func TestRestrictedViewRepoIsTenantScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))

	r := &repo.Repo{Pool: h.Pool}

	tenantA := uuid.New()
	tenantB := uuid.New()
	adminA := seedAdmin(t, ctx, h, tenantA)
	adminB := seedAdmin(t, ctx, h, tenantB)

	enabled := true
	mk := func(name string) *models.CreateRestrictedViewRequest {
		return &models.CreateRestrictedViewRequest{
			Name:            name,
			Resource:        "datasets",
			Action:          "read",
			HiddenColumns:   json.RawMessage(`["ssn"]`),
			AllowedOrgIDs:   json.RawMessage(`[]`),
			AllowedMarkings: json.RawMessage(`["public"]`),
			Enabled:         &enabled,
		}
	}

	viewA, err := r.CreateRestrictedView(ctx, mk("redact-a"), adminA, tenantA)
	require.NoError(t, err)
	require.NotNil(t, viewA)
	assert.Equal(t, tenantA, viewA.TenantID)

	viewB, err := r.CreateRestrictedView(ctx, mk("redact-b"), adminB, tenantB)
	require.NoError(t, err)
	require.NotNil(t, viewB)
	assert.Equal(t, tenantB, viewB.TenantID)

	t.Run("List is tenant-scoped", func(t *testing.T) {
		got, err := r.ListRestrictedViews(ctx, tenantA)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, viewA.ID, got[0].ID)

		got, err = r.ListRestrictedViews(ctx, tenantB)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, viewB.ID, got[0].ID)
	})

	t.Run("Get from another tenant returns nil", func(t *testing.T) {
		got, err := r.GetRestrictedView(ctx, viewA.ID, tenantB)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("Update from another tenant is a no-op", func(t *testing.T) {
		hijacked := "hijacked"
		out, err := r.UpdateRestrictedView(ctx, viewA.ID, tenantB,
			&models.UpdateRestrictedViewRequest{Name: &hijacked})
		require.NoError(t, err)
		assert.Nil(t, out)

		// Owning tenant still sees the original name.
		fresh, err := r.GetRestrictedView(ctx, viewA.ID, tenantA)
		require.NoError(t, err)
		require.NotNil(t, fresh)
		assert.Equal(t, "redact-a", fresh.Name)
	})

	t.Run("Delete from another tenant is silently ignored", func(t *testing.T) {
		require.NoError(t, r.DeleteRestrictedView(ctx, viewA.ID, tenantB))
		fresh, err := r.GetRestrictedView(ctx, viewA.ID, tenantA)
		require.NoError(t, err)
		require.NotNil(t, fresh, "cross-tenant delete must not affect the owning tenant")
	})

	t.Run("Owner can delete", func(t *testing.T) {
		require.NoError(t, r.DeleteRestrictedView(ctx, viewA.ID, tenantA))
		fresh, err := r.GetRestrictedView(ctx, viewA.ID, tenantA)
		require.NoError(t, err)
		assert.Nil(t, fresh)
	})
}

// seedAdmin inserts a minimal users row owned by orgID so that
// restricted_views.created_by satisfies its FK. Tests use the
// returned uuid as the JWT subject.
func seedAdmin(t *testing.T, ctx context.Context, h *testingx.PostgresHarness, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO users (id, email, username, name, password_hash, organization_id)
		 VALUES ($1, $2, $3, 'seed', 'x', $4)`,
		id, id.String()+"@example.test", id.String(), orgID)
	require.NoError(t, err)
	return id
}
