package models

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityProviderOrganizationRuleJSONRoundtrip(t *testing.T) {
	t.Parallel()
	ws := "engineering"
	tier := "enterprise"
	in := IdentityProviderOrganizationRule{
		Name:           "engineering-okta",
		OrganizationID: uuid.New(),
		Workspace:      &ws,
		Roles:          []string{"member", "admin"},
		TenantTier:     &tier,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got IdentityProviderOrganizationRule
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestIdentityProviderMappingJSONRoundtrip(t *testing.T) {
	t.Parallel()
	defaultOrg := uuid.New()
	defaultWS := "engineering"
	in := IdentityProviderMapping{
		ProviderSlug:          "okta-prod",
		DefaultOrganizationID: &defaultOrg,
		DefaultWorkspace:      &defaultWS,
		DefaultRoles:          []string{"member"},
		AllowedEmailDomains:   []string{"openfoundry.example", "acme.example"},
		OrganizationRules: []IdentityProviderOrganizationRule{
			{
				Name:           "rule-1",
				OrganizationID: uuid.New(),
				Roles:          []string{"member"},
			},
		},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got IdentityProviderMapping
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestResourceQuotaSettingsJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := ResourceQuotaSettings{
		MaxQueryLimit:              10000,
		MaxDistributedQueryWorkers: 32,
		MaxPipelineWorkers:         16,
		MaxRequestBodyBytes:        16 * 1024 * 1024,
		RequestsPerMinute:          1200,
		MaxStorageGB:               5000,
		MaxSharedSpaces:            64,
		MaxGuestSessions:           100,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got ResourceQuotaSettings
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestResourceManagementPolicyJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := ResourceManagementPolicy{
		Name:                "enterprise-default",
		TenantTier:          "enterprise",
		AppliesToOrgIDs:     []uuid.UUID{uuid.New(), uuid.New()},
		AppliesToWorkspaces: []string{"engineering", "research"},
		Quota: ResourceQuotaSettings{
			MaxQueryLimit:              5000,
			MaxDistributedQueryWorkers: 16,
			MaxPipelineWorkers:         8,
			MaxRequestBodyBytes:        8 * 1024 * 1024,
			RequestsPerMinute:          600,
			MaxStorageGB:               2000,
			MaxSharedSpaces:            32,
			MaxGuestSessions:           50,
		},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got ResourceManagementPolicy
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}
