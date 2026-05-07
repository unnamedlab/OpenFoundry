package cedarauthz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	authzcedar "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// --- Bundle parsing ------------------------------------------------------

func TestBundledPolicyRecordsParsesAllThree(t *testing.T) {
	t.Parallel()
	records, err := BundledPolicyRecords()
	require.NoError(t, err)
	require.Len(t, records, 3, "identity_admin.cedar declares 3 policies")

	ids := map[string]struct{}{}
	for _, r := range records {
		ids[r.ID] = struct{}{}
		assert.Equal(t, int32(1), r.Version)
		assert.NotEmpty(t, r.Source, "policy id=%s should round-trip its source", r.ID)
	}
	// Every policy should be present (cedar parser uses the @id
	// annotation as the policy id).
	for _, want := range []string{"identity-jwks-rotation", "identity-scim-provisioning", "identity-scim-forbid-human"} {
		_, ok := ids[want]
		assert.True(t, ok, "missing policy id %q in bundle", want)
	}
}

func TestBootstrapEngineYieldsLiveEngine(t *testing.T) {
	t.Parallel()
	engine, err := BootstrapEngine()
	require.NoError(t, err)
	require.NotNil(t, engine)
	assert.NotNil(t, engine.Store())
	assert.False(t, engine.Store().IsEmpty(), "engine must boot with the bundled policies")
	assert.Equal(t, 3, engine.Store().Len())
}

// --- Action UIDs ---------------------------------------------------------

func TestActionUIDsMatchCedarPolicy(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"rotate_jwks":           ActionRotateJwks.ID.String(),
		"retire_jwks":           ActionRetireJwks.ID.String(),
		"scim_provision_user":   ActionScimProvisionUser.ID.String(),
		"scim_provision_group":  ActionScimProvisionGroup.ID.String(),
	}
	for want, got := range cases {
		assert.Equal(t, want, got, "action id mismatch for %s", want)
	}
	// Verbatim Rust quirk: deprovision maps to the same provision id.
	assert.Equal(t, ActionScimProvisionUser, ActionScimDeprovisionUser,
		"Rust source intentionally aliases deprovision to provision; preserve")
}

// --- Resource extractors -------------------------------------------------

func TestJwksKeyResourceShape(t *testing.T) {
	t.Parallel()
	uid, ents, err := JwksKeyResource(httptest.NewRequest(http.MethodPost, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, "JwksKey", string(uid.Type))
	assert.Equal(t, "_active", uid.ID.String())
	require.Len(t, ents, 1)
}

func TestScimUserResourceShape(t *testing.T) {
	t.Parallel()
	uid, ents, err := ScimUserResource(httptest.NewRequest(http.MethodPost, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, "ScimUser", string(uid.Type))
	assert.Equal(t, "_pool", uid.ID.String())
	require.Len(t, ents, 1)
}

func TestScimGroupResourceShape(t *testing.T) {
	t.Parallel()
	uid, ents, err := ScimGroupResource(httptest.NewRequest(http.MethodPost, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, "ScimGroup", string(uid.Type))
	assert.Equal(t, "_pool", uid.ID.String())
	require.Len(t, ents, 1)
}

// --- Principal hydration -------------------------------------------------

func TestPrincipalEntitiesFromClaimsBaseline(t *testing.T) {
	t.Parallel()
	sub := uuid.New()
	org := uuid.New()
	claims := &authmw.Claims{
		Sub:   sub,
		OrgID: &org,
		Roles: []string{"admin"},
	}
	principal, parents := PrincipalEntitiesFromClaims(claims)
	assert.Equal(t, "User", string(principal.UID.Type))
	assert.Equal(t, sub.String(), principal.UID.ID.String())

	// One parent entity for the role.
	require.Len(t, parents, 1)
	assert.Equal(t, "Role", string(parents[0].UID.Type))
	assert.Equal(t, "admin", parents[0].UID.ID.String())
}

func TestPrincipalEntitiesFromClaimsKindAndMFA(t *testing.T) {
	t.Parallel()
	sub := uuid.New()
	attrs, err := json.Marshal(map[string]any{
		"kind":         "service_account",
		"mfa_age_secs": 120,
		"groups":       []any{"IdentityKeyRotators"},
	})
	require.NoError(t, err)
	claims := &authmw.Claims{
		Sub:        sub,
		Roles:      []string{"scim_writer"},
		Attributes: attrs,
	}
	principal, parents := PrincipalEntitiesFromClaims(claims)

	// One Group + one Role parent.
	require.Len(t, parents, 2)
	got := map[string]string{}
	for _, e := range parents {
		got[string(e.UID.Type)] = e.UID.ID.String()
	}
	assert.Equal(t, "IdentityKeyRotators", got["Group"])
	assert.Equal(t, "scim_writer", got["Role"])

	// Principal carries kind + mfa_age_secs.
	kindV, ok := principal.Attributes.Get(cedar.String("kind"))
	require.True(t, ok)
	assert.Equal(t, cedar.String("service_account"), kindV)
	mfaV, ok := principal.Attributes.Get(cedar.String("mfa_age_secs"))
	require.True(t, ok)
	assert.Equal(t, cedar.Long(120), mfaV)
}

func TestPrincipalEntitiesFromClaimsHandlesNilAttributes(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{
		Sub:        uuid.New(),
		Attributes: nil,
	}
	principal, parents := PrincipalEntitiesFromClaims(claims)
	_, has := principal.Attributes.Get(cedar.String("kind"))
	assert.False(t, has, "kind is absent when attributes are nil")
	assert.Empty(t, parents)
}

func TestPrincipalEntitiesFromClaimsIgnoresMalformedAttributes(t *testing.T) {
	t.Parallel()
	claims := &authmw.Claims{
		Sub:        uuid.New(),
		Attributes: json.RawMessage("not-json"),
	}
	principal, parents := PrincipalEntitiesFromClaims(claims)
	_, has := principal.Attributes.Get(cedar.String("kind"))
	assert.False(t, has)
	assert.Empty(t, parents)
}

// --- AdminGuard middleware behaviour ------------------------------------

func TestAdminGuardRejectsMissingClaims(t *testing.T) {
	t.Parallel()
	engine, err := BootstrapEngine()
	require.NoError(t, err)

	called := false
	wrapped := AdminGuard(ActionRotateJwks, JwksKeyResource)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/jwks/rotate", nil)
	// Engine in context but no Claims.
	req = req.WithContext(authzcedar.WithEngine(req.Context(), engine))
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.False(t, called, "downstream handler must NOT run")
	assert.Contains(t, rec.Body.String(), "missing Claims")
}

func TestAdminGuardRejectsMissingEngine(t *testing.T) {
	t.Parallel()
	wrapped := AdminGuard(ActionRotateJwks, JwksKeyResource)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/jwks/rotate", nil)
	req = req.WithContext(authmw.ContextWithClaims(req.Context(), &authmw.Claims{Sub: uuid.New()}))
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "AuthzEngine not configured")
}

func TestAdminGuardJwksRotationAllowedForGroupMember(t *testing.T) {
	t.Parallel()
	engine, err := BootstrapEngine()
	require.NoError(t, err)

	// Service-account in IdentityKeyRotators with mfa_age_secs <= 300.
	attrs, err := json.Marshal(map[string]any{
		"kind":         "service_account",
		"mfa_age_secs": 60,
		"groups":       []any{"IdentityKeyRotators"},
	})
	require.NoError(t, err)
	claims := &authmw.Claims{
		Sub:        uuid.New(),
		Attributes: attrs,
	}

	called := false
	wrapped := AdminGuard(ActionRotateJwks, JwksKeyResource)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/jwks/rotate", nil)
	ctx := authmw.ContextWithClaims(req.Context(), claims)
	ctx = authzcedar.WithEngine(ctx, engine)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code, "handler must run; got body=%s", rec.Body.String())
	assert.True(t, called)
}

func TestAdminGuardJwksRotationDeniedWhenMFAStale(t *testing.T) {
	t.Parallel()
	engine, err := BootstrapEngine()
	require.NoError(t, err)

	// MFA last refreshed 10 minutes ago — policy requires <= 300 s.
	attrs, _ := json.Marshal(map[string]any{
		"kind":         "service_account",
		"mfa_age_secs": 600,
		"groups":       []any{"IdentityKeyRotators"},
	})
	claims := &authmw.Claims{Sub: uuid.New(), Attributes: attrs}

	wrapped := AdminGuard(ActionRotateJwks, JwksKeyResource)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("handler must not run")
	}))
	req := httptest.NewRequest(http.MethodPost, "/jwks/rotate", nil)
	ctx := authmw.ContextWithClaims(req.Context(), claims)
	ctx = authzcedar.WithEngine(ctx, engine)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "forbidden"))
}

func TestAdminGuardScimAllowsServiceAccountWithRole(t *testing.T) {
	t.Parallel()
	engine, err := BootstrapEngine()
	require.NoError(t, err)

	attrs, _ := json.Marshal(map[string]any{"kind": "service_account"})
	claims := &authmw.Claims{
		Sub:        uuid.New(),
		Roles:      []string{"scim_writer"},
		Attributes: attrs,
	}
	wrapped := AdminGuard(ActionScimProvisionUser, ScimUserResource)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", nil)
	ctx := authmw.ContextWithClaims(req.Context(), claims)
	ctx = authzcedar.WithEngine(ctx, engine)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusCreated, rec.Code, "got %s", rec.Body.String())
}

func TestAdminGuardScimDeniesHumanWithRole(t *testing.T) {
	t.Parallel()
	// The forbid policy must deny humans even when they hold
	// scim_writer (the common-mistake catch).
	engine, err := BootstrapEngine()
	require.NoError(t, err)

	attrs, _ := json.Marshal(map[string]any{"kind": "human"})
	claims := &authmw.Claims{
		Sub:        uuid.New(),
		Roles:      []string{"scim_writer"},
		Attributes: attrs,
	}
	wrapped := AdminGuard(ActionScimProvisionUser, ScimUserResource)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("forbid policy should kick in; handler must not run")
	}))
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", nil)
	ctx := authmw.ContextWithClaims(req.Context(), claims)
	ctx = authzcedar.WithEngine(ctx, engine)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
