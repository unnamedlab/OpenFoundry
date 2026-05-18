package proxy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/proxy"
)

// upstreamFor wires every URL slot to a unique probe so the test asserts
// which upstream the rule selected via simple string equality.
func upstreamFor(t *testing.T) config.UpstreamURLs {
	t.Helper()
	u := config.UpstreamURLs{}
	// Reflective string pointers would be tidier but invasive; the
	// fields below cover every code path the table-driven test exercises.
	u.IdentityFederation = "id-fed"
	u.Cipher = "cipher"
	u.AuthorizationPolicy = "authz"
	u.SecurityGovernance = "sec-gov"
	u.NetworkBoundary = "net-bound"
	u.ConnectorManagement = "conn-mgmt"
	u.IngestionReplication = "ingest"
	u.CheckpointsPurpose = "checkpoints"
	u.AuditCompliance = "audit"
	u.TenancyOrganizations = "tenancy"
	u.IcebergCatalog = "iceberg"
	u.DatasetVersioning = "dvs"
	u.Query = "query"
	u.PipelineBuild = "pipeline"
	u.Lineage = "lineage"
	u.OntologyActions = "ontology-actions"
	u.OntologyQuery = "ontology-query"
	u.ObjectDatabase = "object-db"
	u.OntologyDefinition = "ontology-def"
	u.Workflow = "workflow"
	u.Notebook = "notebook"
	u.Notification = "notif"
	u.ModelCatalog = "model-catalog"
	u.ModelDeployment = "model-deploy"
	u.ML = "ml"
	u.AIEvaluation = "ai-eval"
	u.LLMCatalog = "llm-catalog"
	u.AgentRuntime = "agent-runtime"
	u.RetrievalContext = "retrieval"
	u.KnowledgeIndex = "knowledge"
	u.AI = "ai"
	u.EntityResolution = "entity-res"
	u.Report = "report"
	u.GeospatialIntelligence = "geo"
	u.GlobalBranch = "branch"
	u.CodeRepo = "code-repo"
	u.FederationProductExchange = "marketplace"
	u.ApplicationComposition = "app-comp"
	u.TelemetryGovernance = "telemetry"
	return u
}

func TestSelectUpstream(t *testing.T) {
	t.Parallel()
	u := upstreamFor(t)

	cases := []struct {
		path string
		want string
	}{
		// identity & auth
		{"/api/v1/auth/login", "id-fed"},
		{"/api/v1/auth/sso/google/callback", "id-fed"},
		{"/api/v1/api-keys", "id-fed"},
		{"/api/v1/auth/cipher", "cipher"},
		{"/api/v1/users/me", "id-fed"},
		{"/api/v1/application-access/evaluate", "id-fed"},
		{"/api/v1/file-access-presets/visible", "id-fed"},
		{"/api/v1/users/abc/roles", "authz"},
		{"/api/v1/roles", "authz"},
		{"/api/v1/permissions/x", "authz"},
		// security/network
		{"/api/v1/security-governance/posture", "sec-gov"},
		{"/api/v1/network-boundaries", "net-bound"},
		{"/api/v1/data-connection/sources", "conn-mgmt"},
		{"/api/v1/data-connection/egress-policies", "net-bound"},
		// retention/audit
		{"/api/v1/retention/policies", "audit"},
		{"/api/v1/audit/gdpr/erase", "audit"},
		{"/api/v1/audit/events", "audit"},
		{"/api/v1/audit/sds/foo", "audit"},
		// tenancy
		{"/api/v1/organizations", "tenancy"},
		{"/api/v1/projects", "tenancy"},
		// iceberg
		{"/api/v1/iceberg-tables", "iceberg"},
		{"/iceberg/v1/namespaces", "iceberg"},
		{"/v1/iceberg-clients/api-tokens", "iceberg"},
		// datasets / catalog
		{"/api/v1/datasets/abc/quality", "dvs"},
		{"/api/v1/datasets/abc/transactions", "dvs"},
		{"/api/v1/datasets/abc/branches/main", "dvs"},
		{"/api/v1/datasets/abc/files", "dvs"},
		{"/api/v1/datasets/catalog/facets", "dvs"},
		{"/api/v1/datasets", "dvs"},
		// pipelines/queries/lineage
		{"/api/v1/queries/run", "query"},
		{"/api/v1/pipelines/123/run", "pipeline"},
		{"/api/v1/pipelines", "pipeline"},
		{"/api/v1/lineage/abc", "lineage"},
		// ontology
		{"/api/v1/ontology/actions/exec", "ontology-actions"},
		{"/api/v1/ontology/funnel/x", "ontology-actions"},
		{"/api/v1/ontology/types/X/objects/Y/inline-edit/foo", "ontology-actions"},
		{"/api/v1/ontology/types/X/properties/P/objects/Y/inline-edit", "ontology-actions"},
		{"/api/v1/ontology/types/X/inline-edit-batch", "ontology-actions"},
		{"/api/v1/ontology/search?q=x", "ontology-query"},
		{"/api/v1/ontology/types/X/objects/query", "object-db"},
		{"/api/v1/ontology/types/X/objects", "object-db"},
		{"/api/v1/ontology/links/X/instances/y", "object-db"},
		{"/api/v1/ontology/interfaces", "ontology-def"},
		{"/api/v1/ontology/types", "ontology-def"},
		{"/api/v1/ontology/anything-else", "ontology-def"},
		// workflows/notebooks (ADR-0030: approvals-service retired into
		// workflow-automation-service; both /workflows/approvals and the
		// /approvals alias resolve to workflow now).
		{"/api/v1/workflows/approvals/x", "workflow"},
		{"/api/v1/approvals/x", "workflow"},
		{"/api/v1/workflows/instances", "workflow"},
		{"/api/v1/notebooks/abc", "notebook"},
		{"/api/v1/notepad/abc", "notebook"},
		// monitoring rules
		{"/api/v1/monitoring/views", "telemetry"},
		{"/api/v1/monitoring/rules/abc/pause", "telemetry"},
		{"/api/v1/monitor-rules/abc", "telemetry"},
		// ML / AI
		{"/api/v1/ml/experiments", "model-catalog"},
		{"/api/v1/ml/deployments/abc/predict", "model-deploy"},
		{"/api/v1/ml/deployments/abc/drift", "model-deploy"},
		{"/api/v1/ml/widgets", "ml"},
		{"/api/v1/ai/evaluations/run", "ai-eval"},
		{"/api/v1/ai/providers", "llm-catalog"},
		{"/api/v1/ai/prompts", "agent-runtime"},
		{"/api/v1/agent-runtime/agents", "agent-runtime"},
		{"/api/v1/ai/knowledge-bases/abc/search", "knowledge"},
		{"/api/v1/ai/knowledge-bases", "knowledge"},
		// ADR-0030: conversation-state-service retired into agent-runtime-service.
		{"/api/v1/ai/conversations", "agent-runtime"},
		{"/api/v1/ai/anything", "ai"},
		// ADR-0030: streaming-service retired with no Go successor — the
		// route returns the empty string (404 / unknown_service_route).
		{"/api/v1/streaming/foo", ""},
		{"/api/v1/reports", "report"},
		{"/api/v1/geospatial/x", "geo"},
		// code repo
		{"/api/v1/code-repos/repositories/abc/branches", "code-repo"},
		{"/api/v1/global-branches", "branch"},
		{"/api/v1/code-repos/repositories/abc/commits", "code-repo"},
		// marketplace
		{"/api/v1/marketplace/installs", "marketplace"},
		{"/api/v1/marketplace/devops/branches", "marketplace"},
		{"/api/v1/marketplace/anything", "marketplace"},
		// apps
		{"/api/v1/widgets/x", "app-comp"},
		{"/api/v1/apps/templates", "app-comp"},
		{"/api/v1/apps/from-template", "app-comp"},
		{"/api/v1/apps/abc/preview", "app-comp"},
		{"/api/v1/apps/abc/pages/page-1", "app-comp"},
		{"/api/v1/apps/abc/slate-package", "app-comp"},
		{"/api/v1/apps/abc/publish", "app-comp"},
		// ADR-0030: app-builder-service retired into application-composition-service.
		{"/api/v1/apps", "app-comp"},
		// no rule
		{"/healthz", ""},
		{"/api/v1/totally-unknown", ""},
	}
	for _, c := range cases {
		got := proxy.SelectUpstream(c.path, u)
		assert.Equal(t, c.want, got, "path=%s", c.path)
	}
}
