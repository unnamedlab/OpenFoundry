package handlers

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func TestValidateDeveloperAPIKeyExpiryRequiresTemporaryFutureExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

	_, err := validateDeveloperAPIKeyExpiry(nil, now)
	require.EqualError(t, err, "expires_at is required for temporary developer API tokens")

	past := now.Add(-time.Minute)
	_, err = validateDeveloperAPIKeyExpiry(&past, now)
	require.EqualError(t, err, "expires_at must be in the future")

	tooLong := now.Add(31 * 24 * time.Hour)
	_, err = validateDeveloperAPIKeyExpiry(&tooLong, now)
	require.EqualError(t, err, "expires_at must be within 30 days")

	ok := now.Add(7 * 24 * time.Hour)
	got, err := validateDeveloperAPIKeyExpiry(&ok, now)
	require.NoError(t, err)
	require.Equal(t, ok, got)
}

func TestDeriveAPIKeyScopesIntersectsCallerPermissions(t *testing.T) {
	t.Parallel()

	scopes, err := deriveAPIKeyScopes(
		[]string{"datasets:read", "datasets:read", "projects:write"},
		nil,
		[]string{"datasets:*", "projects:write"},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"datasets:read", "projects:write"}, scopes)

	_, err = deriveAPIKeyScopes([]string{"admin:write"}, nil, []string{"datasets:read"})
	require.EqualError(t, err, "requested scopes exceed caller permissions: admin:write")

	scopes, err = deriveAPIKeyScopes(nil, []string{"admin"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"*:*"}, scopes)
}

func TestDetectAPIKeyLeakWarningsMatchesOwnedPrefix(t *testing.T) {
	t.Parallel()
	token := "ofapikey_abcdefghijklmnopqrstuvwxyz123456"
	id := uuid.New()
	warnings := detectAPIKeyLeakWarnings(
		"export TOKEN="+token,
		"diff",
		[]models.APIKey{{ID: id, Prefix: visibleAPIKeyPrefix(token)}},
	)

	require.Len(t, warnings, 1)
	require.Equal(t, "critical", warnings[0].Severity)
	require.Equal(t, &id, warnings[0].APIKeyID)
	require.NotContains(t, warnings[0].Redacted, "mnopqrstuvwxyz")
}
