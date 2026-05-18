package config_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/config"
)

// knownUpstreamAliases lists upstream-URL groups that intentionally share
// a port because one bounded context is served by a surviving binary.
// Any URL collision between fields NOT in the same group is a bug: the
// gateway would route two unrelated surfaces to the same process.
var knownUpstreamAliases = [][]string{
	{"IdentityFederation", "OauthIntegration", "SessionGovernance"},
	{"AuthorizationPolicy", "SecurityGovernance", "Cipher", "CheckpointsPurpose", "NetworkBoundary"},
	{"TenancyOrganizations", "Nexus"},
	{"DataConnector", "ConnectorManagement", "VirtualTable"},
	{"DatasetVersioning", "DataAssetCatalog", "DatasetQuality"},
	{"PipelineAuthoring", "PipelineBuild", "PipelineSchedule"},
	{"OntologyDefinition", "Ontology"},
	{"ApplicationCuration", "ApplicationComposition"},
	{"ML", "ModelCatalog"},
	{"ModelDeployment", "ModelEvaluation", "ModelServing", "ModelInferenceHistory"},
	{"AI", "AgentRuntime"},
	{"Notebook", "DocumentReporting", "Report"},
	{"CodeRepo", "GlobalBranch"},
	{"MarketplaceCatalog", "ProductDistribution", "FederationProductExchange"},
	{"AuditCompliance", "Audit", "RetentionPolicy", "LineageDeletion", "SDS"},
}

func aliasGroupOf(field string) int {
	for i, group := range knownUpstreamAliases {
		for _, name := range group {
			if name == field {
				return i
			}
		}
	}
	return -1
}

func TestDefaultUpstreams_NoUnintendedDuplicates(t *testing.T) {
	d := config.DefaultUpstreams()
	v := reflect.ValueOf(d)
	tt := v.Type()

	byURL := make(map[string][]string, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		url := v.Field(i).String()
		require.NotEmpty(t, url, "field %s has empty default URL", tt.Field(i).Name)
		byURL[url] = append(byURL[url], tt.Field(i).Name)
	}

	for url, fields := range byURL {
		if len(fields) == 1 {
			continue
		}
		// All fields sharing this URL must be in the same documented alias group.
		group := aliasGroupOf(fields[0])
		assert.NotEqual(t, -1, group,
			"URL %s shared by %v but no alias group documented", url, fields)
		for _, f := range fields[1:] {
			assert.Equal(t, group, aliasGroupOf(f),
				"URL %s shared across distinct services %v — unintended port collision", url, fields)
		}
	}
}

// TestDefaultUpstreams_SecurityGovernanceNotificationDistinct guards the
// specific regression where SecurityGovernance and Notification both
// defaulted to :50114 (notification-alerting-service's port), breaking
// routing in dev/docker-compose.
func TestDefaultUpstreams_SecurityGovernanceNotificationDistinct(t *testing.T) {
	d := config.DefaultUpstreams()
	assert.NotEqual(t, d.SecurityGovernance, d.Notification,
		"SecurityGovernance and Notification must not share a port")
	assert.Equal(t, "http://localhost:50114", d.Notification,
		"Notification owns :50114 (notification-alerting-service)")
	assert.Equal(t, d.AuthorizationPolicy, d.SecurityGovernance,
		"SecurityGovernance is absorbed into authorization-policy-service (ADR-0030 B14)")
}
