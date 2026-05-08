package authmw_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// scopedClaims mirrors the Rust `scoped_claims()` test fixture in
// libs/auth-middleware/src/claims.rs verbatim — same field set, same
// values — so the assertions below test identical behaviour to the
// Rust unit tests.
func scopedClaims() *authmw.Claims {
	guestEmail := "guest@example.com"
	guestName := "Guest"
	workspace := "shared"
	clearance := "public"
	tokenUse := "access"
	sessionKind := "guest_session"
	orgID := uuid.Nil

	return &authmw.Claims{
		Sub:         uuid.Nil,
		IAT:         0,
		EXP:         math.MaxInt64,
		JTI:         uuid.Nil,
		Email:       "guest@example.com",
		Name:        "Guest",
		Roles:       []string{"viewer"},
		Permissions: []string{"datasets:read"},
		OrgID:       &orgID,
		Attributes:  json.RawMessage(`{"classification_clearance":"confidential"}`),
		AuthMethods: []string{"guest"},
		TokenUse:    &tokenUse,
		SessionKind: &sessionKind,
		SessionScope: &authmw.SessionScope{
			AllowedMethods:          []string{"GET"},
			AllowedPathPrefixes:     []string{"/api/v1/datasets"},
			AllowedSubjectIDs:       []string{"subject-1"},
			AllowedOrgIDs:           []uuid.UUID{uuid.Nil},
			Workspace:               &workspace,
			ClassificationClearance: &clearance,
			AllowedMarkings:         []string{"public"},
			RestrictedViewIDs:       []uuid.UUID{uuid.Nil},
			ConsumerMode:            true,
			GuestEmail:              &guestEmail,
			GuestDisplayName:        &guestName,
		},
	}
}

func TestSessionScopeLimitsMethodsAndPaths(t *testing.T) {
	t.Parallel()
	c := scopedClaims()
	assert.True(t, c.IsGuestSession())
	assert.True(t, c.AllowsHTTPMethod("GET"))
	assert.False(t, c.AllowsHTTPMethod("POST"))
	assert.True(t, c.AllowsPath("/api/v1/datasets/123"))
	assert.False(t, c.AllowsPath("/api/v1/pipelines"))
}

func TestSessionScopeLimitsSubjectsAndPrefersScopeClearance(t *testing.T) {
	t.Parallel()
	c := scopedClaims()
	clearance, ok := c.ClassificationClearance()
	assert.True(t, ok)
	assert.Equal(t, "public", clearance)
	assert.True(t, c.AllowsMarking("public"))
	assert.False(t, c.AllowsMarking("confidential"))

	subj1 := "subject-1"
	subj2 := "subject-2"
	assert.True(t, c.AllowsSubjectID(&subj1))
	assert.False(t, c.AllowsSubjectID(&subj2))

	assert.Equal(t, []uuid.UUID{uuid.Nil}, c.AllowedOrgIDs())
	zero := uuid.Nil
	assert.True(t, c.AllowsOrgID(&zero))
	assert.Equal(t, []uuid.UUID{uuid.Nil}, c.RestrictedViewIDs())
	assert.True(t, c.ConsumerModeEnabled())
}

func TestAllowedOrgIDsFallsBackToOrgIDWhenScopeUnset(t *testing.T) {
	t.Parallel()
	org := uuid.New()
	c := &authmw.Claims{OrgID: &org}
	assert.Equal(t, []uuid.UUID{org}, c.AllowedOrgIDs())

	noOrg := &authmw.Claims{}
	assert.Empty(t, noOrg.AllowedOrgIDs())
}

func TestAllowsOrgIDNilResourceForNonGuestSessionPasses(t *testing.T) {
	t.Parallel()
	// Non-guest session with a scoped allowlist: nil resource org
	// must pass (mirrors Rust `!is_guest_session()`).
	org := uuid.New()
	c := &authmw.Claims{
		Roles:        []string{"member"},
		SessionScope: &authmw.SessionScope{AllowedOrgIDs: []uuid.UUID{org}},
	}
	assert.True(t, c.AllowsOrgID(nil))

	// Same shape but flipped to a guest session: nil org now denied.
	guest := "guest@example.com"
	guestKind := "guest_session"
	c.SessionKind = &guestKind
	c.SessionScope.GuestEmail = &guest
	assert.False(t, c.AllowsOrgID(nil))
}

func TestAttributeReturnsValueWhenPresent(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Attributes: json.RawMessage(`{"region":"eu","tier":3}`)}

	v, ok := c.Attribute("region")
	assert.True(t, ok)
	assert.Equal(t, "eu", v)

	v, ok = c.Attribute("tier")
	assert.True(t, ok)
	assert.InDelta(t, float64(3), v, 0)

	_, ok = c.Attribute("missing")
	assert.False(t, ok)

	empty := &authmw.Claims{}
	_, ok = empty.Attribute("anything")
	assert.False(t, ok)
}

func TestIssuedAtAndExpiresAtRoundTripUTC(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{IAT: 1_700_000_000, EXP: 1_700_003_600}
	assert.Equal(t, time.Unix(1_700_000_000, 0).UTC(), c.IssuedAt())
	assert.Equal(t, time.Unix(1_700_003_600, 0).UTC(), c.ExpiresAt())
}
