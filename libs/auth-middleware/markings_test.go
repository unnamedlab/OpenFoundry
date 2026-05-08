package authmw_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/security"
)

// claimsWith mirrors the Rust `claims_with(roles, allowed)` helper
// in libs/auth-middleware/src/markings.rs verbatim.
func claimsWith(roles []string, allowed []string) *authmw.Claims {
	now := time.Now().Unix()
	return &authmw.Claims{
		Sub:   uuid.New(),
		IAT:   now,
		EXP:   now + 3600,
		JTI:   uuid.New(),
		Email: "u@example.test",
		Name:  "u",
		Roles: roles,
		SessionScope: &authmw.SessionScope{
			AllowedMarkings: allowed,
		},
	}
}

func TestAdminBypassesEnforcement(t *testing.T) {
	t.Parallel()
	c := claimsWith([]string{"admin"}, nil)
	clearances := authmw.CallerClearancesFromClaimsNamesOnly(c)
	assert.True(t, clearances.IsAdmin())
	restricted := security.NewMarkingID()
	require.NoError(t, authmw.EnforceMarkings([]security.MarkingID{restricted}, &clearances))
}

func TestNonAdminWithFullCoveragePasses(t *testing.T) {
	t.Parallel()
	public := security.NewMarkingID()
	restricted := security.NewMarkingID()
	resolver := authmw.NewStaticMarkingNameResolver(map[string]security.MarkingID{
		"public":     public,
		"restricted": restricted,
	})
	c := claimsWith(nil, []string{"public", "restricted"})
	clearances := authmw.CallerClearancesFromClaims(c, resolver)
	require.NoError(t, authmw.EnforceMarkings([]security.MarkingID{public, restricted}, &clearances))
}

func TestNonAdminMissingMarkingIsForbidden(t *testing.T) {
	t.Parallel()
	public := security.NewMarkingID()
	restricted := security.NewMarkingID()
	resolver := authmw.NewStaticMarkingNameResolver(map[string]security.MarkingID{
		"public":     public,
		"restricted": restricted,
	})
	c := claimsWith(nil, []string{"public"})
	clearances := authmw.CallerClearancesFromClaims(c, resolver)
	err := authmw.EnforceMarkings([]security.MarkingID{public, restricted}, &clearances)
	require.Error(t, err)
	var enf *authmw.MarkingEnforcementError
	require.True(t, errors.As(err, &enf))
	assert.Equal(t, []security.MarkingID{restricted}, enf.Missing)
}

func TestAllowsNameHandlesCaseInsensitiveLookup(t *testing.T) {
	t.Parallel()
	c := claimsWith(nil, []string{"PII"})
	clearances := authmw.CallerClearancesFromClaimsNamesOnly(c)
	assert.True(t, clearances.AllowsName("pii"))
	assert.True(t, clearances.AllowsName("PiI"))
	assert.False(t, clearances.AllowsName("restricted"))
}
