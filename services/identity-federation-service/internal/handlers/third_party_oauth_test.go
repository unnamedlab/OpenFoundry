package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func TestDeriveThirdPartyOAuthScopesIntersectsRequestAppAndSubjectPermissions(t *testing.T) {
	t.Parallel()

	granted, missing := deriveThirdPartyOAuthScopes(
		[]string{"datasets:read", "datasets:write", "ontology:read", "admin:manage"},
		[]string{"datasets:*", "ontology:read"},
		nil,
		[]string{"datasets:read", "ontology:*"},
	)

	require.Equal(t, []string{"datasets:read", "ontology:read"}, granted)
	require.Equal(t, []string{"admin:manage", "datasets:write"}, missing)
}

func TestDeriveThirdPartyOAuthScopesKeepsAdminBoundedByApplicationMaxScope(t *testing.T) {
	t.Parallel()

	granted, missing := deriveThirdPartyOAuthScopes(
		[]string{"datasets:read", "admin:manage"},
		[]string{"datasets:*"},
		[]string{"admin"},
		nil,
	)

	require.Equal(t, []string{"datasets:read"}, granted)
	require.Equal(t, []string{"admin:manage"}, missing)
}

func TestDeriveThirdPartyOAuthScopesTreatsEmptyApplicationScopeAsUnrestricted(t *testing.T) {
	t.Parallel()

	granted, missing := deriveThirdPartyOAuthScopes(
		[]string{"datasets:read", "ontology:read"},
		nil,
		nil,
		[]string{"datasets:*"},
	)

	require.Equal(t, []string{"datasets:read"}, granted)
	require.Equal(t, []string{"ontology:read"}, missing)

	granted, missing = deriveThirdPartyOAuthScopes(nil, nil, nil, []string{"datasets:read", "ontology:write"})
	require.Equal(t, []string{"datasets:read", "ontology:write"}, granted)
	require.Empty(t, missing)
}

func TestVerifyOAuthPKCES256(t *testing.T) {
	t.Parallel()

	verifier := "correct-horse-battery-staple"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	require.True(t, verifyOAuthPKCE(oauthPKCEMethodS256, challenge, verifier))
	require.False(t, verifyOAuthPKCE(oauthPKCEMethodS256, challenge, "wrong"))
	require.False(t, verifyOAuthPKCE("plain", challenge, verifier))
}

func TestValidateOAuthClientAuthentication(t *testing.T) {
	t.Parallel()

	secret := "of3pa_secret_test"
	hash := hashOAuthClientSecret(secret)
	confidential := &models.ThirdPartyApplication{
		ID:         uuid.New(),
		ClientID:   "of3pa_confidential",
		ClientType: models.ThirdPartyClientTypeConfidential,
	}
	require.NoError(t, validateOAuthClientAuthentication(confidential, &hash, confidential.ClientID, secret))
	require.Error(t, validateOAuthClientAuthentication(confidential, &hash, confidential.ClientID, "bad-secret"))
	require.Error(t, validateOAuthClientAuthentication(confidential, nil, confidential.ClientID, secret))

	public := &models.ThirdPartyApplication{
		ID:         uuid.New(),
		ClientID:   "of3pa_public",
		ClientType: models.ThirdPartyClientTypePublic,
	}
	require.NoError(t, validateOAuthClientAuthentication(public, nil, public.ClientID, ""))
	require.Error(t, validateOAuthClientAuthentication(public, nil, "wrong-client", ""))
}

func TestEnabledThirdPartyApplicationForOrganization(t *testing.T) {
	t.Parallel()

	orgID := uuid.New()
	app := &models.ThirdPartyApplication{
		ID: uuid.New(),
		Enablements: []models.ThirdPartyApplicationEnablement{
			{ApplicationID: uuid.New(), OrganizationID: uuid.New(), Enabled: true},
			{ApplicationID: uuid.New(), OrganizationID: orgID, Enabled: true, OrganizationConsent: true},
		},
	}

	enablement, ok := enabledThirdPartyApplicationForOrganization(app, orgID)
	require.True(t, ok)
	require.True(t, enablement.OrganizationConsent)
	_, ok = enabledThirdPartyApplicationForOrganization(app, uuid.New())
	require.False(t, ok)
}
