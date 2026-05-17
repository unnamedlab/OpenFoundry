// Unit-level guard for the tenant isolation invariant on
// ListEnabledABACPoliciesMatching. Drives the query through pgxmock so
// we can pin two behaviours without a Postgres dependency:
//
//   1. The SQL hits the tenant_id = $1 predicate (regex on the query).
//   2. With two tenants, the policy authored under tenant A is *not*
//      returned to a tenant-B caller.
//
// The corresponding wire-level scenario against a real Postgres lives
// in abac_tenant_integration_test.go (build tag: integration).
package repo_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
)

var abacScanCols = []string{
	"id", "tenant_id", "name", "description", "effect", "resource", "action",
	"conditions", "row_filter", "enabled", "created_by", "created_at", "updated_at",
}

func TestListEnabledABACPoliciesMatchingFiltersByTenant(t *testing.T) {
	ctx := context.Background()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	tenantA := uuid.New()
	tenantB := uuid.New()
	policyID := uuid.New()
	createdBy := uuid.New()
	now := time.Now().UTC()

	// Tenant A: returns the policy it owns.
	mock.ExpectQuery(`tenant_id = \$1`).
		WithArgs(tenantA, "datasets", "read").
		WillReturnRows(pgxmock.NewRows(abacScanCols).AddRow(
			policyID, tenantA, "allow-read", nil, "allow",
			"datasets", "read", []byte(`{}`), nil, true,
			&createdBy, now, now,
		))

	// Tenant B: empty result set — the same policy must not surface.
	mock.ExpectQuery(`tenant_id = \$1`).
		WithArgs(tenantB, "datasets", "read").
		WillReturnRows(pgxmock.NewRows(abacScanCols))

	r := &repo.Repo{Pool: mock}

	got, err := r.ListEnabledABACPoliciesMatching(ctx, tenantA, "datasets", "read")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, tenantA, got[0].TenantID)
	assert.Equal(t, policyID, got[0].ID)

	got, err = r.ListEnabledABACPoliciesMatching(ctx, tenantB, "datasets", "read")
	require.NoError(t, err)
	assert.Empty(t, got, "tenant B must not see tenant A's policy")

	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateABACPolicyPersistsCallerTenant(t *testing.T) {
	ctx := context.Background()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	tenantID := uuid.New()
	caller := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery(`INSERT INTO abac_policies`).
		WithArgs(
			pgxmock.AnyArg(), // generated id
			tenantID,
			"p",
			pgxmock.AnyArg(), // description (*string nil)
			"allow", "datasets", "read",
			pgxmock.AnyArg(), // conditions default `{}`
			pgxmock.AnyArg(), // row_filter (*string nil)
			true,
			caller,
		).
		WillReturnRows(pgxmock.NewRows(abacScanCols).AddRow(
			uuid.New(), tenantID, "p", nil, "allow",
			"datasets", "read", []byte(`{}`), nil, true,
			&caller, now, now,
		))

	r := &repo.Repo{Pool: mock}

	body := &models.CreateABACPolicyRequest{
		Name:     "p",
		Effect:   "allow",
		Resource: "datasets",
		Action:   "read",
	}
	p, err := r.CreateABACPolicy(ctx, body, tenantID, caller)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, tenantID, p.TenantID)
	require.NoError(t, mock.ExpectationsWereMet())
}
