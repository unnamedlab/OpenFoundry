package proxy_test

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/config"
)

// upstreamServiceMapping pins each gateway upstream slot to the
// in-repo service directory whose `main.go` is expected to serve it.
//
// Several upstreams point at the same binary (per ADR-0030's
// "consolidate retired services into the surviving owner" rule); the
// table records each mapping explicitly so a future port change in a
// merged service can't drift silently.
//
// Upstreams that don't yet have a Go service in `services/` (legacy
// Rust slices not ported, or future scaffolds) are listed in the test
// as "unmapped" so the assertion that *every* UpstreamURLs field is
// accounted for keeps that list honest.
var upstreamServiceMapping = []struct {
	field          string // name of the field on config.UpstreamURLs
	serviceDir     string // optional dir under services/ that owns this upstream
	composeService string // optional compose service that must provide environment.PORT
}{
	{"IdentityFederation", "identity-federation-service", "identity-federation-service"},
	{"OauthIntegration", "", "identity-federation-service"},
	{"SessionGovernance", "", "identity-federation-service"},
	{"AuthorizationPolicy", "authorization-policy-service", "authorization-policy-service"},
	{"SecurityGovernance", "", "authorization-policy-service"},
	{"TenancyOrganizations", "tenancy-organizations-service", "tenancy-organizations-service"},
	{"Cipher", "", "authorization-policy-service"},
	{"ConnectorManagement", "connector-management-service", "connector-management-service"},
	{"DataConnector", "connector-management-service", "connector-management-service"},
	{"VirtualTable", "", "connector-management-service"},
	{"IngestionReplication", "ingestion-replication-service", "ingestion-replication-service"},
	{"DatasetVersioning", "dataset-versioning-service", "dataset-versioning-service"},
	{"DataAssetCatalog", "", "dataset-versioning-service"},
	{"DatasetQuality", "", "dataset-versioning-service"},
	{"IcebergCatalog", "iceberg-catalog-service", ""},
	{"Query", "sql-bi-gateway-service", "sql-bi-gateway-service"},
	{"PipelineAuthoring", "", "pipeline-build-service"},
	{"PipelineBuild", "pipeline-build-service", "pipeline-build-service"},
	{"PipelineSchedule", "", "pipeline-build-service"},
	{"Lineage", "lineage-service", "lineage-service"},
	{"OntologyDefinition", "ontology-definition-service", "ontology-definition-service"},
	{"Ontology", "ontology-definition-service", "ontology-definition-service"},
	{"ObjectDatabase", "object-database-service", "object-database-service"},
	{"OntologyQuery", "ontology-query-service", "ontology-query-service"},
	{"OntologyActions", "ontology-actions-service", "ontology-actions-service"},
	{"Workflow", "workflow-automation-service", "workflow-automation-service"},
	{"Notebook", "notebook-runtime-service", "notebook-runtime-service"},
	{"Notification", "notification-alerting-service", "notification-alerting-service"},
	{"ApplicationCuration", "", "application-composition-service"},
	{"ApplicationComposition", "application-composition-service", "application-composition-service"},
	{"ML", "model-catalog-service", "model-catalog-service"},
	{"ModelCatalog", "model-catalog-service", "model-catalog-service"},
	{"ModelDeployment", "model-deployment-service", "model-deployment-service"},
	{"ModelEvaluation", "", "model-deployment-service"},
	{"ModelServing", "", "model-deployment-service"},
	{"ModelInferenceHistory", "", "model-deployment-service"},
	{"AI", "agent-runtime-service", "agent-runtime-service"},
	{"AgentRuntime", "agent-runtime-service", "agent-runtime-service"},
	{"LLMCatalog", "llm-catalog-service", "llm-catalog-service"},
	{"RetrievalContext", "retrieval-context-service", "retrieval-context-service"},
	{"AIEvaluation", "ai-evaluation-service", "ai-evaluation-service"},
	{"DocumentReporting", "", "notebook-runtime-service"},
	{"EntityResolution", "entity-resolution-service", "entity-resolution-service"},
	{"Report", "", "notebook-runtime-service"},
	{"GeospatialIntelligence", "ontology-exploratory-analysis-service", "ontology-exploratory-analysis-service"},
	{"CodeRepo", "code-repository-review-service", "code-repository-review-service"},
	{"GlobalBranch", "", "code-repository-review-service"},
	{"MarketplaceCatalog", "", "federation-product-exchange-service"},
	{"ProductDistribution", "", "federation-product-exchange-service"},
	{"FederationProductExchange", "federation-product-exchange-service", "federation-product-exchange-service"},
	{"CheckpointsPurpose", "", "authorization-policy-service"},
	{"NetworkBoundary", "", "authorization-policy-service"},
	{"RetentionPolicy", "", "audit-compliance-service"},
	{"LineageDeletion", "", "audit-compliance-service"},
	{"AuditCompliance", "audit-compliance-service", "audit-compliance-service"},
	{"Audit", "audit-compliance-service", "audit-compliance-service"},
	{"SDS", "", "audit-compliance-service"},
	{"Nexus", "", "tenancy-organizations-service"},
	{"TelemetryGovernance", "telemetry-governance-service", "telemetry-governance-service"},
}

// upstreamsWithoutGoService lists UpstreamURLs fields for which no
// service directory exists yet (stubs that store `addr: :8080` in
// config.yaml, or legacy Rust slices not ported). Listing them here
// instead of in upstreamServiceMapping keeps the "every field is
// accounted for" check from drifting.
var upstreamsWithoutGoService = map[string]string{
	"KnowledgeIndex": "no compose service is wired for the knowledge-index surface yet",
}

type composeFile struct {
	Services map[string]struct {
		Environment any `yaml:"environment"`
	} `yaml:"services"`
}

func loadComposePorts(t *testing.T, root string) map[string]uint16 {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "infra", "compose", "docker-compose.yml"))
	require.NoError(t, err)
	var cf composeFile
	require.NoError(t, yaml.Unmarshal(body, &cf))
	ports := make(map[string]uint16, len(cf.Services))
	for name, svc := range cf.Services {
		port, ok := envPort(svc.Environment)
		if ok {
			ports[name] = port
		}
	}
	return ports
}

func envPort(environment any) (uint16, bool) {
	switch env := environment.(type) {
	case map[string]any:
		if raw, ok := env["PORT"]; ok {
			return parsePortValue(raw)
		}
	case map[any]any:
		if raw, ok := env["PORT"]; ok {
			return parsePortValue(raw)
		}
	case []any:
		for _, item := range env {
			entry, ok := item.(string)
			if !ok || !strings.HasPrefix(entry, "PORT=") {
				continue
			}
			return parsePortValue(strings.TrimPrefix(entry, "PORT="))
		}
	}
	return 0, false
}

func parsePortValue(raw any) (uint16, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(raw)), 10, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

// portDefaultRe captures the integer literal in
// `parseUint16(os.Getenv("PORT"), 50118)`.
var portDefaultRe = regexp.MustCompile(`parseUint16\(os\.Getenv\("PORT"\),\s*(\d+)\s*\)`)

// portDefaultConstRe matches the connector-management-service shape:
// `parseUint16("PORT", os.Getenv("PORT"), DefaultPort)` where the
// default is held in a package-level const declared as
// `DefaultPort uint16 = 50088`.
var portDefaultConstRe = regexp.MustCompile(
	`parseUint16\("PORT",\s*os\.Getenv\("PORT"\),\s*(\w+)\s*\)`,
)
var portConstDeclRe = regexp.MustCompile(`(?m)^\s*(\w+)\s+uint16\s*=\s*(\d+)\s*$`)

// repoRoot returns the path to the repo root from the test working dir
// (`services/edge-gateway-service/internal/proxy`).
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	require.NoError(t, err)
	return root
}

// servicePortFromConfig parses
// `services/<dir>/internal/config/config.go` and returns the integer
// literal passed as the parseUint16("PORT", …) fallback. Supports both
// the inline-literal shape and the named-const shape (see the regex
// declarations above).
func servicePortFromConfig(t *testing.T, root, dir string) uint16 {
	t.Helper()
	cfgPath := filepath.Join(root, "services", dir, "internal", "config", "config.go")
	body, err := os.ReadFile(cfgPath)
	require.NoErrorf(t, err, "reading %s", cfgPath)
	src := string(body)

	if m := portDefaultRe.FindStringSubmatch(src); len(m) == 2 {
		v, err := strconv.ParseUint(m[1], 10, 16)
		require.NoErrorf(t, err, "parsing port literal %q from %s", m[1], cfgPath)
		return uint16(v)
	}
	if m := portDefaultConstRe.FindStringSubmatch(src); len(m) == 2 {
		// Resolve the const declaration `<name> uint16 = N`.
		for _, c := range portConstDeclRe.FindAllStringSubmatch(src, -1) {
			if c[1] == m[1] {
				v, err := strconv.ParseUint(c[2], 10, 16)
				require.NoErrorf(t, err,
					"parsing const %s = %q from %s", c[1], c[2], cfgPath)
				return uint16(v)
			}
		}
		t.Fatalf("could not resolve const %q in %s", m[1], cfgPath)
	}
	t.Fatalf("no parseUint16 PORT default in %s", cfgPath)
	return 0
}

// portFromURL extracts the port from an upstream URL string.
func portFromURL(t *testing.T, raw string) uint16 {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoErrorf(t, err, "parsing URL %q", raw)
	port := parsed.Port()
	require.NotEmptyf(t, port, "no port in URL %q", raw)
	v, err := strconv.ParseUint(port, 10, 16)
	require.NoErrorf(t, err, "parsing port from URL %q", raw)
	return uint16(v)
}

// TestUpstreamPortsMatchComposeAndServiceDefaults asserts that every
// routed upstream either matches its compose service PORT, its owning
// service config default, or both.
//
// It catches drift across three sources of truth: gateway defaults,
// compose PORT values, and service-side parseUint16("PORT", N) fallbacks.
func TestUpstreamPortsMatchComposeAndServiceDefaults(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	composePorts := loadComposePorts(t, root)
	defaults := config.DefaultUpstreams()
	defaultsV := reflect.ValueOf(defaults)

	for _, m := range upstreamServiceMapping {
		m := m
		t.Run(m.field, func(t *testing.T) {
			t.Parallel()

			fv := defaultsV.FieldByName(m.field)
			require.Truef(t, fv.IsValid(),
				"UpstreamURLs has no field %q (upstreamServiceMapping out of date)",
				m.field)
			require.Equalf(t, reflect.String, fv.Kind(),
				"UpstreamURLs.%s is not a string", m.field)
			defaultPort := portFromURL(t, fv.String())

			if m.composeService != "" {
				composePort, ok := composePorts[m.composeService]
				require.Truef(t, ok,
					"compose service %s for UpstreamURLs.%s has no environment.PORT",
					m.composeService, m.field)
				assert.Equalf(t, composePort, defaultPort,
					"DefaultUpstreams.%s = %q, expected compose %s PORT %d",
					m.field, fv.String(), m.composeService, composePort)
			}

			if m.serviceDir == "" {
				return
			}

			cfgPath := filepath.Join(root, "services", m.serviceDir,
				"internal", "config", "config.go")
			_, err := os.Stat(cfgPath)
			require.NoErrorf(t, err,
				"%s upstream expects services/%s/internal/config/config.go",
				m.field, m.serviceDir)
			servicePort := servicePortFromConfig(t, root, m.serviceDir)
			assert.Equalf(t, servicePort, defaultPort,
				"DefaultUpstreams.%s = %q, expected services/%s default PORT %d",
				m.field, fv.String(), m.serviceDir, servicePort)
			if m.composeService != "" {
				assert.Equalf(t, composePorts[m.composeService], servicePort,
					"services/%s default PORT must match compose %s PORT",
					m.serviceDir, m.composeService)
			}
		})
	}
}

// TestUpstreamFieldsAccountedFor ensures every UpstreamURLs field is
// either tied to a real service (upstreamServiceMapping) or explicitly
// parked in upstreamsWithoutGoService. A new field added to the struct
// without a routing decision will fail this test.
func TestUpstreamFieldsAccountedFor(t *testing.T) {
	t.Parallel()
	mapped := map[string]struct{}{}
	for _, m := range upstreamServiceMapping {
		mapped[m.field] = struct{}{}
	}
	tp := reflect.TypeOf(config.UpstreamURLs{})
	var missing []string
	for i := 0; i < tp.NumField(); i++ {
		name := tp.Field(i).Name
		if _, ok := mapped[name]; ok {
			continue
		}
		if _, ok := upstreamsWithoutGoService[name]; ok {
			continue
		}
		missing = append(missing, name)
	}
	require.Emptyf(t, missing,
		"UpstreamURLs fields without a routing decision: %s — add to "+
			"upstreamServiceMapping or upstreamsWithoutGoService",
		strings.Join(missing, ", "))
}

// TestRetiredUpstreamsAreGone asserts the ADR-0030 retirements stay
// retired: AppBuilder, Approvals, ConversationState, and Streaming
// must not reappear on UpstreamURLs. If a future PR re-adds one of
// these without paving the routing path, this test breaks loudly.
func TestRetiredUpstreamsAreGone(t *testing.T) {
	t.Parallel()
	tp := reflect.TypeOf(config.UpstreamURLs{})
	for _, retired := range []string{"AppBuilder", "Approvals", "ConversationState", "Streaming"} {
		_, ok := tp.FieldByName(retired)
		assert.Falsef(t, ok,
			"UpstreamURLs.%s reappeared — ADR-0030 retired this upstream; "+
				"reintroducing it needs an ADR update and a routing entry",
			retired)
	}
}
