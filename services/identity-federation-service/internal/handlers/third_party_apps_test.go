package handlers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func TestValidateThirdPartyApplicationRegistrationPublicClientRequiresPKCEAndRejectsClientCredentials(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	app := models.ThirdPartyApplication{
		ID:                          uuid.New(),
		ClientID:                    "of3pa_test",
		Name:                        "Browser app",
		ClientType:                  models.ThirdPartyClientTypePublic,
		EnabledGrantTypes:           []string{models.ThirdPartyGrantClientCredentials},
		RedirectURIs:                []string{"https://app.example.com/callback"},
		ManagingOrganizationID:      orgID,
		DiscoverableOrganizationIDs: []uuid.UUID{orgID},
		PreferredManagementSurface:  models.ThirdPartyManagementDeveloperConsole,
	}

	err := validateThirdPartyApplicationRegistration(&app)
	require.EqualError(t, err, "public clients cannot use client_credentials")

	app.EnabledGrantTypes = []string{models.ThirdPartyGrantAuthorizationCode}
	require.NoError(t, validateThirdPartyApplicationRegistration(&app))
	require.True(t, app.RequiresPKCE)
}

func TestValidateThirdPartyApplicationRegistrationRedirectRules(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	app := models.ThirdPartyApplication{
		ID:                          uuid.New(),
		ClientID:                    "of3pa_test",
		Name:                        "Server app",
		ClientType:                  models.ThirdPartyClientTypeConfidential,
		EnabledGrantTypes:           []string{models.ThirdPartyGrantAuthorizationCode},
		ManagingOrganizationID:      orgID,
		DiscoverableOrganizationIDs: []uuid.UUID{orgID},
		PreferredManagementSurface:  models.ThirdPartyManagementDeveloperConsole,
	}

	err := validateThirdPartyApplicationRegistration(&app)
	require.EqualError(t, err, "authorization_code grant requires at least one redirect URI")

	app.RedirectURIs = []string{"http://example.com/callback"}
	err = validateThirdPartyApplicationRegistration(&app)
	require.EqualError(t, err, `redirect URI "http://example.com/callback" must use https unless it targets localhost`)

	app.RedirectURIs = []string{"http://localhost:3000/callback"}
	require.NoError(t, validateThirdPartyApplicationRegistration(&app))
}

func TestBuildThirdPartyApplicationCreatesSecretAndServiceUserForConfidentialClientCredentials(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	actor := uuid.New()
	app, serviceUser, secret, secretHash, err := buildThirdPartyApplicationFromCreate(
		models.CreateThirdPartyApplicationRequest{
			Name:                        "Sync worker",
			ClientType:                  models.ThirdPartyClientTypeConfidential,
			EnabledGrantTypes:           []string{models.ThirdPartyGrantClientCredentials},
			Scopes:                      []string{"datasets:read"},
			ManagingOrganizationID:      &orgID,
			DiscoverableOrganizationIDs: []uuid.UUID{orgID},
		},
		&authmw.Claims{Sub: actor, OrgID: &orgID, Roles: []string{"third_party_application_admin"}},
	)

	require.NoError(t, err)
	require.NotNil(t, secret)
	require.NotNil(t, secretHash)
	require.NotEmpty(t, app.ClientSecretPrefix)
	require.NotNil(t, serviceUser)
	require.Equal(t, app.ClientID, serviceUser.Username)
	require.Equal(t, serviceUser.ID, *app.ServiceUserID)
}

func TestCanManageThirdPartyApplicationOrganizationRequiresOrgPermission(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	otherOrg := uuid.New()

	require.True(t, canManageThirdPartyApplicationOrganization(&authmw.Claims{Roles: []string{"admin"}}, orgID))
	require.True(t, canManageThirdPartyApplicationOrganization(&authmw.Claims{
		OrgID:       &orgID,
		Permissions: []string{"oauth_clients:manage"},
	}, orgID))
	require.False(t, canManageThirdPartyApplicationOrganization(&authmw.Claims{
		OrgID:       &otherOrg,
		Permissions: []string{"oauth_clients:read"},
	}, orgID))
}
