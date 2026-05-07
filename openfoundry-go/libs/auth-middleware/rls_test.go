package authmw_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func TestRLSAdminBypassesEveryFilter(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	ctx := authmw.RLSContextFromClaims(c)
	assert.True(t, ctx.IsAdmin())
	assert.Equal(t, "TRUE", ctx.OrgFilter("dataset.org_id"))
	assert.Equal(t, "TRUE", ctx.OwnerOrOrgFilter("dataset.owner_id", "dataset.org_id"))
	assert.True(t, ctx.HasPermission("anything:read"))
}

func TestRLSOrgFilterScopesByOrgID(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	c := &authmw.Claims{Sub: uuid.New(), OrgID: &org}
	ctx := authmw.RLSContextFromClaims(c)
	assert.Equal(t, fmt.Sprintf("dataset.org_id = '%s'", org), ctx.OrgFilter("dataset.org_id"))
}

func TestRLSOrgFilterFallsBackToIsNullWhenUnscoped(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.New()}
	ctx := authmw.RLSContextFromClaims(c)
	assert.Equal(t, "dataset.org_id IS NULL", ctx.OrgFilter("dataset.org_id"))
}

func TestRLSOwnerOrOrgFilterCombinesUserAndOrg(t *testing.T) {
	t.Parallel()
	user := uuid.New()
	org := uuid.New()
	c := &authmw.Claims{Sub: user, OrgID: &org}
	ctx := authmw.RLSContextFromClaims(c)
	want := fmt.Sprintf("(d.owner_id = '%s' OR d.org_id = '%s')", user, org)
	assert.Equal(t, want, ctx.OwnerOrOrgFilter("d.owner_id", "d.org_id"))
}

func TestRLSOwnerOrOrgFilterFallsBackToOwnerOnlyWithoutOrg(t *testing.T) {
	t.Parallel()
	user := uuid.New()
	c := &authmw.Claims{Sub: user}
	ctx := authmw.RLSContextFromClaims(c)
	want := fmt.Sprintf("d.owner_id = '%s'", user)
	assert.Equal(t, want, ctx.OwnerOrOrgFilter("d.owner_id", "d.org_id"))
}

func TestRLSRowsAllPermissionPromotesToTrue(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.New(), Permissions: []string{"rows:all"}}
	ctx := authmw.RLSContextFromClaims(c)
	assert.False(t, ctx.IsAdmin())
	assert.True(t, ctx.HasPermission("rows:all"))
	assert.Equal(t, "TRUE", ctx.OwnerOrOrgFilter("d.owner_id", "d.org_id"))
}
