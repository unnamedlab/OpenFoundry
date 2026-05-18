package proxy_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/proxy"
)

var markdownCodeRe = regexp.MustCompile("`([^`]+)`")
var markdownPortRe = regexp.MustCompile("`?:?(\\d{2,5})`?")
var yamlAddrPortRe = regexp.MustCompile(`(?m)^\s*addr:\s*":(\d+)"\s*$`)
var edgeGatewayPortRe = regexp.MustCompile(`c\.Server\.Port\s*=\s*(\d+)`)

type servicePortDoc struct {
	service string
	ports   []uint16
}

func upstreamForDocs(t *testing.T) config.UpstreamURLs {
	t.Helper()
	return config.UpstreamURLs{
		IdentityFederation:        "identity-federation-service",
		Cipher:                    "Cipher",
		AuthorizationPolicy:       "authorization-policy-service",
		SecurityGovernance:        "authorization-policy-service",
		NetworkBoundary:           "NetworkBoundary",
		ConnectorManagement:       "connector-management-service",
		IngestionReplication:      "ingestion-replication-service",
		CheckpointsPurpose:        "authorization-policy-service",
		AuditCompliance:           "audit-compliance-service",
		TenancyOrganizations:      "tenancy-organizations-service",
		IcebergCatalog:            "iceberg-catalog-service",
		DatasetVersioning:         "dataset-versioning-service",
		Query:                     "sql-bi-gateway-service",
		PipelineBuild:             "pipeline-build-service",
		Lineage:                   "lineage-service",
		OntologyActions:           "ontology-actions-service",
		OntologyQuery:             "ontology-query-service",
		ObjectDatabase:            "object-database-service",
		OntologyDefinition:        "ontology-definition-service",
		Workflow:                  "workflow-automation-service",
		Notebook:                  "notebook-runtime-service",
		Notification:              "notification-alerting-service",
		ModelCatalog:              "model-catalog-service",
		ModelDeployment:           "model-deployment-service",
		AIEvaluation:              "ai-evaluation-service",
		LLMCatalog:                "llm-catalog-service",
		AgentRuntime:              "agent-runtime-service",
		RetrievalContext:          "retrieval-context-service",
		KnowledgeIndex:            "knowledge-index-service",
		EntityResolution:          "entity-resolution-service",
		Report:                    "Report",
		GeospatialIntelligence:    "ontology-exploratory-analysis-service",
		CodeRepo:                  "code-repository-review-service",
		GlobalBranch:              "global-branch-service",
		FederationProductExchange: "federation-product-exchange-service",
		ApplicationComposition:    "application-composition-service",
		TelemetryGovernance:       "telemetry-governance-service",
	}
}

func readDocs(t *testing.T, root string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, "docs", "architecture", "services-and-ports.md"))
	require.NoError(t, err)
	return string(body)
}

func sectionBetween(t *testing.T, text, start, end string) string {
	t.Helper()
	startIdx := strings.Index(text, start)
	require.NotEqualf(t, -1, startIdx, "missing section start %q", start)
	text = text[startIdx+len(start):]
	endIdx := strings.Index(text, end)
	require.NotEqualf(t, -1, endIdx, "missing section end %q", end)
	return text[:endIdx]
}

func docRoutePath(raw string) string {
	path := strings.ReplaceAll(raw, "{id}", "abc")
	path = strings.ReplaceAll(path, "*", "abc")
	return path
}

func TestServicesAndPortsRouteDocsMatchRouterFixtures(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	section := sectionBetween(t, readDocs(t, root), "## Gateway Route Ownership", "### Gateway upstream aliases")
	u := upstreamForDocs(t)

	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") || !strings.Contains(line, "->") {
			continue
		}
		parts := strings.SplitN(line, "->", 2)
		routes := markdownCodeRe.FindAllStringSubmatch(parts[0], -1)
		targets := markdownCodeRe.FindAllStringSubmatch(parts[1], -1)
		require.NotEmptyf(t, targets, "route doc line has no target: %s", line)
		want := targets[0][1]
		for _, route := range routes {
			path := docRoutePath(route[1])
			got := proxy.SelectUpstream(path, u)
			assert.Equalf(t, want, got, "documented route %s must match router_table.go", route[1])
		}
	}
}

func parseDocPorts(cell string) []uint16 {
	matches := markdownPortRe.FindAllStringSubmatch(cell, -1)
	ports := make([]uint16, 0, len(matches))
	for _, match := range matches {
		v, err := strconv.ParseUint(match[1], 10, 16)
		if err != nil {
			continue
		}
		ports = append(ports, uint16(v))
	}
	return ports
}

func hasPort(ports []uint16, want uint16) bool {
	for _, port := range ports {
		if port == want {
			return true
		}
	}
	return false
}

func documentedServicePorts(t *testing.T, text string) []servicePortDoc {
	t.Helper()
	section := sectionBetween(t, text, "## Service Map", "### Internal / data-plane binaries")
	var docs []servicePortDoc
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `services/") && !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		serviceMatch := markdownCodeRe.FindStringSubmatch(cells[1])
		if len(serviceMatch) != 2 || !strings.HasSuffix(serviceMatch[1], "-service") {
			continue
		}
		ports := parseDocPorts(cells[2])
		require.NotEmptyf(t, ports, "service map row for %s has no documented port", serviceMatch[1])
		docs = append(docs, servicePortDoc{service: serviceMatch[1], ports: ports})
	}
	require.NotEmpty(t, docs, "no service map ports parsed from docs")
	return docs
}

func parseServiceConfigPort(src string) (uint16, bool) {
	if match := portDefaultRe.FindStringSubmatch(src); len(match) == 2 {
		v, err := strconv.ParseUint(match[1], 10, 16)
		if err == nil {
			return uint16(v), true
		}
	}
	if match := portDefaultConstRe.FindStringSubmatch(src); len(match) == 2 {
		for _, c := range portConstDeclRe.FindAllStringSubmatch(src, -1) {
			if c[1] != match[1] {
				continue
			}
			v, err := strconv.ParseUint(c[2], 10, 16)
			if err == nil {
				return uint16(v), true
			}
		}
	}
	return 0, false
}

func serviceDefaultPort(t *testing.T, root, service string) uint16 {
	t.Helper()
	if service == "edge-gateway-service" {
		body, err := os.ReadFile(filepath.Join(root, "services", service, "internal", "config", "config.go"))
		require.NoError(t, err)
		match := edgeGatewayPortRe.FindStringSubmatch(string(body))
		require.Len(t, match, 2, "edge gateway server port default not found")
		v, err := strconv.ParseUint(match[1], 10, 16)
		require.NoError(t, err)
		return uint16(v)
	}

	cfgPath := filepath.Join(root, "services", service, "internal", "config", "config.go")
	if body, err := os.ReadFile(cfgPath); err == nil {
		if port, ok := parseServiceConfigPort(string(body)); ok {
			return port
		}
	}

	yamlPath := filepath.Join(root, "services", service, "config.yaml")
	body, err := os.ReadFile(yamlPath)
	require.NoErrorf(t, err, "%s must expose a config.go PORT default or config.yaml server.addr", service)
	match := yamlAddrPortRe.FindStringSubmatch(string(body))
	require.Lenf(t, match, 2, "%s must expose config.yaml server.addr with a port", service)
	v, err := strconv.ParseUint(match[1], 10, 16)
	require.NoError(t, err)
	return uint16(v)
}

func TestServicesAndPortsServiceMapMatchesServiceConfig(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, doc := range documentedServicePorts(t, readDocs(t, root)) {
		doc := doc
		t.Run(doc.service, func(t *testing.T) {
			t.Parallel()
			want := serviceDefaultPort(t, root, doc.service)
			assert.Truef(t, hasPort(doc.ports, want),
				"docs service map for %s has ports %v, expected service config default %d",
				doc.service, doc.ports, want)
		})
	}
}

func upstreamFieldByKoanfTag(t *testing.T, key string) (reflect.StructField, bool) {
	t.Helper()
	tp := reflect.TypeOf(config.UpstreamURLs{})
	for i := 0; i < tp.NumField(); i++ {
		field := tp.Field(i)
		if field.Tag.Get("koanf") == key {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func TestServicesAndPortsGatewayAliasPortsMatchDefaultUpstreams(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	section := sectionBetween(t, readDocs(t, root), "### Gateway upstream aliases", "## Cross-Service Dependencies")
	defaults := reflect.ValueOf(config.DefaultUpstreams())

	checked := 0
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") || strings.Contains(line, "no `UpstreamURLs` field") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		keys := markdownCodeRe.FindStringSubmatch(cells[1])
		if len(keys) != 2 || !strings.HasSuffix(keys[1], "_service_url") {
			continue
		}
		ports := parseDocPorts(cells[1])
		require.NotEmptyf(t, ports, "gateway alias row for %s has no documented port", keys[1])
		field, ok := upstreamFieldByKoanfTag(t, keys[1])
		require.Truef(t, ok, "documented gateway alias %s has no UpstreamURLs koanf tag", keys[1])
		defaultPort := portFromURL(t, defaults.FieldByName(field.Name).String())
		assert.Truef(t, hasPort(ports, defaultPort),
			"gateway alias %s documents ports %v, expected DefaultUpstreams.%s port %d",
			keys[1], ports, field.Name, defaultPort)
		checked++
	}
	require.Greater(t, checked, 20, "gateway alias port drift check parsed too few aliases")
}
