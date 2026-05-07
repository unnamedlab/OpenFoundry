package cedarauthz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// ─── PrincipalEntityFromClaims ──────────────────────────────────────

func TestPrincipalEntityFromClaimsBuildsCanonicalUser(t *testing.T) {
	t.Parallel()
	sub := uuid.New()
	org := uuid.New()
	claims := &authmw.Claims{
		Sub:   sub,
		OrgID: &org,
		Roles: []string{"analyst", "reader"},
		SessionScope: &authmw.SessionScope{
			AllowedMarkings: []string{"public", "confidential"},
		},
	}
	entity := cedarauthz.PrincipalEntityFromClaims(claims)

	uidStr := entity.UID.String()
	assert.Contains(t, uidStr, "User::", "UID type must be User")
	assert.Contains(t, uidStr, sub.String(), "UID id must be claims.Sub")
}

func TestPrincipalEntityFromClaimsHandlesEmptyOrgAndScope(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{
		Sub:   uuid.New(),
		Roles: []string{},
	}
	// No OrgID, no SessionScope → tenant defaults to "" and clearances
	// to empty set. Must not panic.
	entity := cedarauthz.PrincipalEntityFromClaims(claims)
	assert.Contains(t, entity.UID.String(), "User::")
}

// ─── MustEntityUID ──────────────────────────────────────────────────

func TestMustEntityUIDAcceptsArbitraryStrings(t *testing.T) {
	t.Parallel()
	u := cedarauthz.MustEntityUID("Dataset", "ri.foundry.main.dataset.abc")
	assert.Contains(t, u.String(), "Dataset")
	assert.Contains(t, u.String(), "ri.foundry.main.dataset.abc")
}

func TestMustEntityUIDPanicsOnEmptyType(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { cedarauthz.MustEntityUID("", "id") })
}

func TestMustEntityUIDPanicsOnEmptyID(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { cedarauthz.MustEntityUID("Type", "") })
}

// ─── EngineMiddleware + EngineFromContext ───────────────────────────

func TestEngineFromContextRoundTrip(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)

	ctx := cedarauthz.WithEngine(context.Background(), eng)
	got, ok := cedarauthz.EngineFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, eng, got)

	_, ok = cedarauthz.EngineFromContext(context.Background())
	assert.False(t, ok, "absent engine yields false")
}

func TestEngineMiddlewareInjectsEngine(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)

	var captured *cedarauthz.AuthzEngine
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ := cedarauthz.EngineFromContext(r.Context())
		captured = got
	})

	cedarauthz.EngineMiddleware(eng)(handler).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest("GET", "/", nil),
	)
	assert.Same(t, eng, captured)
}

// ─── Guard middleware ───────────────────────────────────────────────

// helper: a ResourceFunc that always returns the given UID + entities.
func staticResource(uid cedar.EntityUID, entities ...cedar.Entity) cedarauthz.ResourceFunc {
	return func(_ *http.Request) (cedar.EntityUID, []cedar.Entity, error) {
		return uid, entities, nil
	}
}

// withClaimsAndEngine wires the request context with both claims and
// the engine, mimicking a real chain (auth-middleware → EngineMiddleware
// → Guard).
func withClaimsAndEngine(t *testing.T, claims *authmw.Claims, eng *cedarauthz.AuthzEngine) *http.Request {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	ctx := req.Context()
	if claims != nil {
		ctx = authmw.ContextWithClaims(ctx, claims)
	}
	if eng != nil {
		ctx = cedarauthz.WithEngine(ctx, eng)
	}
	return req.WithContext(ctx)
}

func makeStore(t *testing.T, policies ...cedarauthz.PolicyRecord) *cedarauthz.PolicyStore {
	t.Helper()
	store, err := cedarauthz.NewWithPolicies(policies)
	require.NoError(t, err)
	return store
}

func TestGuardRequiresClaims(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)

	guard := cedarauthz.Guard(
		cedarauthz.MustEntityUID("Action", "read"),
		staticResource(cedarauthz.MustEntityUID("Dataset", "ds-1")),
	)
	req := httptest.NewRequest("GET", "/", nil).WithContext(
		cedarauthz.WithEngine(context.Background(), eng),
	)
	rec := httptest.NewRecorder()
	guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "Claims")
}

func TestGuardRequiresEngine(t *testing.T) {
	t.Parallel()
	guard := cedarauthz.Guard(
		cedarauthz.MustEntityUID("Action", "read"),
		staticResource(cedarauthz.MustEntityUID("Dataset", "ds-1")),
	)
	claims := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("GET", "/", nil).WithContext(
		authmw.ContextWithClaims(context.Background(), claims),
	)
	rec := httptest.NewRecorder()
	guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "AuthzEngine")
}

func TestGuardForwardsResourceFuncError(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)
	claims := &authmw.Claims{Sub: uuid.New()}

	resourceFn := func(*http.Request) (cedar.EntityUID, []cedar.Entity, error) {
		return cedar.EntityUID{}, nil, errors.New("missing path id")
	}
	guard := cedarauthz.Guard(cedarauthz.MustEntityUID("Action", "read"), resourceFn)
	req := withClaimsAndEngine(t, claims, eng)
	rec := httptest.NewRecorder()
	guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing path id")
}

func TestGuardDeniesByDefaultEmptyPolicySet(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	eng := cedarauthz.NewEngineNoopAudit(store)
	claims := &authmw.Claims{Sub: uuid.New()}

	dsUID := cedarauthz.MustEntityUID("Dataset", "ds-1")
	dataset := cedar.Entity{
		UID: dsUID,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"rid":      cedar.String("ri.dataset.x"),
			"tenant":   cedar.String("acme"),
			"markings": cedar.NewSet(),
		}),
	}
	guard := cedarauthz.Guard(
		cedarauthz.MustEntityUID("Action", "read"),
		staticResource(dsUID, dataset),
	)
	req := withClaimsAndEngine(t, claims, eng)
	rec := httptest.NewRecorder()
	guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, strings.ToLower(rec.Body.String()), "forbidden")
}

func TestGuardAllowsAndForwardsOutcome(t *testing.T) {
	t.Parallel()
	store := makeStore(t, cedarauthz.PolicyRecord{
		ID: "permit-all-reads",
		Source: `permit(
			principal,
			action == Action::"read",
			resource is Dataset
		);`,
	})
	eng := cedarauthz.NewEngineNoopAudit(store)
	claims := &authmw.Claims{Sub: uuid.New()}

	dsUID := cedarauthz.MustEntityUID("Dataset", "ds-allow")
	dataset := cedar.Entity{
		UID: dsUID,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"rid":      cedar.String("ri.dataset.allow"),
			"tenant":   cedar.String("acme"),
			"markings": cedar.NewSet(),
		}),
	}
	guard := cedarauthz.Guard(
		cedarauthz.MustEntityUID("Action", "read"),
		staticResource(dsUID, dataset),
	)
	var sawOutcome *cedarauthz.AuthorizeOutcome
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		o, ok := cedarauthz.OutcomeFromContext(r.Context())
		if ok {
			sawOutcome = o
		}
	})

	req := withClaimsAndEngine(t, claims, eng)
	rec := httptest.NewRecorder()
	guard(inner).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, "200 OK when handler returns no body")
	require.NotNil(t, sawOutcome, "outcome must be in the request context after a successful guard")
	assert.True(t, sawOutcome.IsAllow())
	assert.Contains(t, sawOutcome.PolicyIDs, "permit-all-reads")
}
