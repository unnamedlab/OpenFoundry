package handlers

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// recordedBatch holds the (events, ctx) tuple captured by a single
// emitAuthAudit invocation. Used by tests to assert on the exact
// envelope set the SSO handler produces.
type recordedBatch struct {
	events []audittrail.AuditEvent
	auditx audittrail.AuditContext
}

func newRecordingBatcher() (*[]recordedBatch, AuditBatchEmitter) {
	captured := &[]recordedBatch{}
	emit := func(_ context.Context, events []audittrail.AuditEvent, ctx audittrail.AuditContext) error {
		copyEvents := append([]audittrail.AuditEvent(nil), events...)
		*captured = append(*captured, recordedBatch{events: copyEvents, auditx: ctx})
		return nil
	}
	return captured, emit
}

func mintAccessToken(t *testing.T, jwt *authmw.JWTConfig, user *models.User, methods []string, exp time.Time) (string, uuid.UUID) {
	t.Helper()
	jti := uuid.New()
	claims := &authmw.Claims{
		Sub:         user.ID,
		IAT:         time.Now().Unix(),
		EXP:         exp.Unix(),
		JTI:         jti,
		Email:       user.Email,
		Name:        user.Name,
		AuthMethods: methods,
	}
	tok, err := authmw.EncodeToken(jwt, claims)
	require.NoError(t, err)
	return tok, jti
}

// TestEmitAuthAuditFirstLinkProducesThreeEnvelopes is the headline
// T8 assertion: a successful SSO callback emits exactly three audit
// envelopes — identity_linked, login, token_issued — in that order
// when the IdP binding is new.
func TestEmitAuthAuditFirstLinkProducesThreeEnvelopes(t *testing.T) {
	t.Parallel()

	jwt := authmw.NewJWTConfig("unit-test-secret")
	orgID := uuid.New()
	user := &models.User{
		ID:             uuid.New(),
		Email:          "alice@example.com",
		Name:           "Alice",
		OrganizationID: &orgID,
	}
	authMethods := []string{"sso", "okta"}
	exp := time.Now().Add(time.Hour).Truncate(time.Second).UTC()
	access, jti := mintAccessToken(t, jwt, user, authMethods, exp)

	captured, emit := newRecordingBatcher()
	s := &SSO{
		Issuer:        &service.Issuer{JWT: jwt, AccessTTL: time.Hour},
		EmitAudit:     emit,
		SourceService: "identity-federation-service",
	}

	r := httptest.NewRequest("GET", "/api/v1/auth/sso/okta/callback", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.4")
	r.Header.Set("User-Agent", "TestAgent/1.0")
	r.Header.Set("X-Request-Id", "req-abc-123")

	s.emitAuthAudit(r, authAuditInput{
		User:        user,
		Provider:    "okta",
		Subject:     "okta-subject-xyz",
		LoginEmail:  user.Email,
		FirstLink:   true,
		AuthMethods: authMethods,
		AccessToken: access,
	})

	require.Len(t, *captured, 1, "audit batcher should be invoked exactly once")
	batch := (*captured)[0]
	require.Len(t, batch.events, 3, "first-link callback must emit 3 envelopes")

	assert.Equal(t, audittrail.KindIdentityLinked, batch.events[0].Kind)
	assert.Equal(t, audittrail.KindAuthLogin, batch.events[1].Kind)
	assert.Equal(t, audittrail.KindTokenIssued, batch.events[2].Kind)

	// IdentityLinked envelope.
	linked := batch.events[0]
	assert.Equal(t, user.ID.String(), linked.UserID)
	assert.Equal(t, orgID.String(), linked.TenantID)
	assert.Equal(t, "okta", linked.Provider)
	assert.Equal(t, "okta-subject-xyz", linked.Subject)
	assert.Equal(t, "alice@example.com", linked.LoginEmail)
	assert.Equal(t, audittrail.UserResourceRID(user.ID.String()), linked.ResourceRID)

	// AuthLogin envelope.
	login := batch.events[1]
	assert.Equal(t, user.ID.String(), login.UserID)
	assert.Equal(t, orgID.String(), login.TenantID)
	assert.Equal(t, "okta", login.Provider)
	assert.Equal(t, "okta-subject-xyz", login.Subject)
	assert.Equal(t, "alice@example.com", login.LoginEmail)
	require.NotNil(t, login.MFASatisfied)
	assert.False(t, *login.MFASatisfied, "SSO-only auth methods must report MFA unsatisfied")
	assert.Equal(t, authMethods, login.AuthMethods)

	// TokenIssued envelope.
	issued := batch.events[2]
	assert.Equal(t, jti.String(), issued.TokenID)
	assert.Equal(t, user.ID.String(), issued.UserID)
	assert.Equal(t, orgID.String(), issued.TenantID)
	assert.Equal(t, exp, issued.ExpiresAt)
	assert.Equal(t, authMethods, issued.Scopes)

	// AuditContext is request-scoped: actor + IP + UA + request id.
	assert.Equal(t, user.ID.String(), batch.auditx.ActorID)
	assert.Equal(t, "203.0.113.4", batch.auditx.IP)
	assert.Equal(t, "TestAgent/1.0", batch.auditx.UserAgent)
	assert.Equal(t, "req-abc-123", batch.auditx.RequestID)
	assert.Equal(t, "identity-federation-service", batch.auditx.SourceService)
}

// TestEmitAuthAuditExistingBindingSkipsIdentityLinked asserts that a
// re-login on an already-linked IdP subject emits only auth.login +
// auth.token_issued (no spurious auth.identity_linked event each time
// the user signs in).
func TestEmitAuthAuditExistingBindingSkipsIdentityLinked(t *testing.T) {
	t.Parallel()

	jwt := authmw.NewJWTConfig("unit-test-secret")
	user := &models.User{ID: uuid.New(), Email: "bob@example.com", Name: "Bob"}
	authMethods := []string{"sso", "okta"}
	exp := time.Now().Add(time.Hour).Truncate(time.Second).UTC()
	access, _ := mintAccessToken(t, jwt, user, authMethods, exp)

	captured, emit := newRecordingBatcher()
	s := &SSO{
		Issuer:    &service.Issuer{JWT: jwt, AccessTTL: time.Hour},
		EmitAudit: emit,
	}
	r := httptest.NewRequest("GET", "/cb", nil)

	s.emitAuthAudit(r, authAuditInput{
		User:        user,
		Provider:    "okta",
		Subject:     "okta-sub",
		LoginEmail:  user.Email,
		FirstLink:   false,
		AuthMethods: authMethods,
		AccessToken: access,
	})

	require.Len(t, *captured, 1)
	batch := (*captured)[0]
	require.Len(t, batch.events, 2, "re-login should emit only auth.login + auth.token_issued")
	assert.Equal(t, audittrail.KindAuthLogin, batch.events[0].Kind)
	assert.Equal(t, audittrail.KindTokenIssued, batch.events[1].Kind)
}

// TestEmitAuthAuditMFAFlagFromAuthMethods locks the policy that the
// audit MFA flag is derived from the auth_methods slice — any of
// totp/webauthn/mfa flips it true; pure sso flows leave it false.
func TestEmitAuthAuditMFAFlagFromAuthMethods(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		methods []string
		want    bool
	}{
		{"sso_only", []string{"sso", "okta"}, false},
		{"sso_plus_totp", []string{"sso", "okta", "totp"}, true},
		{"sso_plus_webauthn", []string{"sso", "okta", "webauthn"}, true},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := mfaSatisfiedFromAuthMethods(tc.methods); got != tc.want {
				t.Fatalf("mfaSatisfied(%v) = %v, want %v", tc.methods, got, tc.want)
			}
		})
	}
}

// TestEmitAuthAuditMissingEmitterIsLoggedNotFatal verifies that if
// the production wiring forgot to inject the batcher the handler logs
// the gap and returns without panicking. Belt-and-braces — the
// programming error should be caught at boot, but if it slips through
// it must not crash the auth flow.
func TestEmitAuthAuditMissingEmitterIsLoggedNotFatal(t *testing.T) {
	t.Parallel()
	user := &models.User{ID: uuid.New()}
	jwt := authmw.NewJWTConfig("k")
	access, _ := mintAccessToken(t, jwt, user, []string{"sso"}, time.Now().Add(time.Hour))
	s := &SSO{
		Issuer:    &service.Issuer{JWT: jwt, AccessTTL: time.Hour},
		EmitAudit: nil, // simulate boot wiring miss
	}
	r := httptest.NewRequest("GET", "/cb", nil)
	// No panic, no return value to assert — the test is satisfied by
	// reaching this line.
	s.emitAuthAudit(r, authAuditInput{User: user, AccessToken: access})
}
