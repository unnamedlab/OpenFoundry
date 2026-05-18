package runner

import (
	"encoding/base64"
	"strings"
	"testing"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

func TestParseArgs_happyPath(t *testing.T) {
	t.Setenv("ICEBERG_CATALOG_URL", "") // clear env so explicit flags win deterministically
	t.Setenv("ICEBERG_TABLE_WRITER_URL", "")
	t.Setenv("OF_PIPELINE_RUNNER_HEALTH_ADDR", "")
	t.Setenv("PORT", "")

	args, err := ParseArgs([]string{
		"--pipeline-id", "p1",
		"--run-id", "r1",
		"--input-dataset", "cat.ns.in",
		"--output-dataset", "cat.ns.out",
		"--catalog-uri", "http://lakekeeper:8181/catalog",
		"--catalog-warehouse", "openfoundry",
		"--catalog-credential", "lakekeeper:s",
		"--oauth-token-uri", "http://oidc/token",
		"--oauth-scope", "openid",
		"--table-writer-url", "http://iceberg-catalog:8080",
		"--internal-token", "tok",
		"--health-addr", "0.0.0.0:9091",
		"--log-format", "json",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args.PipelineID != "p1" || args.RunID != "r1" {
		t.Errorf("missing required scope fields: %+v", args)
	}
	if args.CatalogURI != "http://lakekeeper:8181/catalog" {
		t.Errorf("CatalogURI = %q", args.CatalogURI)
	}
	if args.CatalogWarehouse != "openfoundry" {
		t.Errorf("CatalogWarehouse = %q", args.CatalogWarehouse)
	}
	if args.TableWriterURL != "http://iceberg-catalog:8080" {
		t.Errorf("TableWriterURL = %q", args.TableWriterURL)
	}
	if args.LogFormat != "json" {
		t.Errorf("LogFormat = %q", args.LogFormat)
	}
	if args.HealthAddr != "0.0.0.0:9091" {
		t.Errorf("HealthAddr = %q", args.HealthAddr)
	}
}

func TestParseArgs_envFallbacks(t *testing.T) {
	t.Setenv("ICEBERG_CATALOG_URL", "http://env-catalog")
	t.Setenv("ICEBERG_TABLE_WRITER_URL", "")
	t.Setenv("OF_PIPELINE_RUNNER_HEALTH_ADDR", "")
	t.Setenv("PORT", "9090")

	args, err := ParseArgs([]string{"--pipeline-id", "p", "--run-id", "r"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args.CatalogURI != "http://env-catalog" {
		t.Errorf("CatalogURI should fall back to env, got %q", args.CatalogURI)
	}
	if args.TableWriterURL != "http://env-catalog" {
		t.Errorf("TableWriterURL should default to CatalogURI when unset, got %q", args.TableWriterURL)
	}
	if args.HealthAddr != "0.0.0.0:9090" {
		t.Errorf("HealthAddr should derive from PORT, got %q", args.HealthAddr)
	}
}

func TestParseArgs_smokeShortCircuitsValidation(t *testing.T) {
	t.Setenv("ICEBERG_CATALOG_URL", "")
	t.Setenv("ICEBERG_TABLE_WRITER_URL", "")
	t.Setenv("OF_PIPELINE_RUNNER_HEALTH_ADDR", "")
	t.Setenv("PORT", "")

	args, err := ParseArgs([]string{"--smoke"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !args.Smoke {
		t.Error("Smoke should be true")
	}
	if args.PipelineID != "" {
		t.Errorf("smoke mode should not require PipelineID")
	}
}

func TestParseArgs_rejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	_, err := ParseArgs([]string{"--bogus", "x"})
	if err == nil {
		t.Fatal("expected error on unknown flag")
	}
	if !strings.Contains(err.Error(), "--bogus") {
		t.Errorf("error should name the unknown flag: %v", err)
	}
}

func TestParseArgs_requiresMandatoryFields(t *testing.T) {
	t.Setenv("ICEBERG_CATALOG_URL", "")
	t.Setenv("ICEBERG_TABLE_WRITER_URL", "")
	t.Setenv("OF_PIPELINE_RUNNER_HEALTH_ADDR", "")
	t.Setenv("PORT", "")

	cases := []struct {
		name string
		argv []string
		want string
	}{
		{"missing pipeline-id", []string{"--run-id", "r", "--catalog-uri", "u", "--table-writer-url", "w"}, "--pipeline-id"},
		{"missing run-id", []string{"--pipeline-id", "p", "--catalog-uri", "u", "--table-writer-url", "w"}, "--run-id"},
		{"missing catalog-uri", []string{"--pipeline-id", "p", "--run-id", "r"}, "--catalog-uri"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseArgs(tc.argv)
			if err == nil {
				t.Fatalf("expected error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q should mention %q", err, tc.want)
			}
		})
	}
}

func TestParseArgs_flagWithoutValue(t *testing.T) {
	t.Parallel()
	_, err := ParseArgs([]string{"--pipeline-id"})
	if err == nil || !strings.Contains(err.Error(), "requires a value") {
		t.Fatalf("expected dangling-flag error, got %v", err)
	}
}

func TestParseArgs_rejectsBadLogFormat(t *testing.T) {
	t.Parallel()
	_, err := ParseArgs([]string{"--smoke", "--log-format", "yaml"})
	if err == nil || !strings.Contains(err.Error(), "--log-format") {
		t.Fatalf("expected log-format error, got %v", err)
	}
}

func TestLoadPlanFromEnv_happyPath(t *testing.T) {
	plan := pipelineplan.Plan{
		PipelineID: "p", RunID: "r",
		Ops: []pipelineplan.Op{
			{ID: "src", Kind: pipelineplan.KindReadTable,
				ReadTable: &pipelineplan.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
			{ID: "sink", Kind: pipelineplan.KindWriteTable, Inputs: []string{"src"},
				WriteTable: &pipelineplan.WriteTable{Catalog: "c", Namespace: "n", Table: "t", Mode: pipelineplan.WriteModeAppend}},
		},
	}
	jsonBytes := mustJSON(t, plan)
	t.Setenv(EnvPipelinePlanB64, base64.StdEncoding.EncodeToString(jsonBytes))

	got, err := loadPlanFromEnv()
	if err != nil {
		t.Fatalf("loadPlanFromEnv: %v", err)
	}
	if got.PipelineID != "p" || len(got.Ops) != 2 {
		t.Errorf("decoded plan mismatch: %+v", got)
	}
}

func TestLoadPlanFromEnv_emptyEnv(t *testing.T) {
	t.Setenv(EnvPipelinePlanB64, "")
	_, err := loadPlanFromEnv()
	if err == nil || !strings.Contains(err.Error(), "env var is empty") {
		t.Fatalf("expected empty-env error, got %v", err)
	}
}

func TestLoadPlanFromEnv_badBase64(t *testing.T) {
	t.Setenv(EnvPipelinePlanB64, "not!base64!")
	_, err := loadPlanFromEnv()
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("expected base64 decode error, got %v", err)
	}
}

func TestLoadPlanFromEnv_badJSON(t *testing.T) {
	t.Setenv(EnvPipelinePlanB64, base64.StdEncoding.EncodeToString([]byte("{not json")))
	_, err := loadPlanFromEnv()
	if err == nil || !strings.Contains(err.Error(), "unmarshal plan JSON") {
		t.Fatalf("expected JSON error, got %v", err)
	}
}

func TestFirstCatalogNameInPlan(t *testing.T) {
	t.Parallel()
	plan := pipelineplan.Plan{Ops: []pipelineplan.Op{
		{ID: "src", Kind: pipelineplan.KindReadTable,
			ReadTable: &pipelineplan.ReadTable{Catalog: "lakekeeper", Namespace: "n", Table: "t"}},
	}}
	if got := firstCatalogNameInPlan(plan); got != "lakekeeper" {
		t.Errorf("got %q, want %q", got, "lakekeeper")
	}
	if got := firstCatalogNameInPlan(pipelineplan.Plan{}); got != "" {
		t.Errorf("empty plan should yield empty catalog name, got %q", got)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := jsonMarshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// jsonMarshal indirection so the test does not need its own
// `encoding/json` import block when the only consumer is the helper.
func jsonMarshal(v any) ([]byte, error) {
	return jsonMarshalImpl(v)
}
