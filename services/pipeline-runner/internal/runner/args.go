// Package runner is the entrypoint of pipeline-runner: parses CLI
// args, decodes the [pipelineplan.Plan] from the env var the
// dispatcher injected, and drives the libs/pipeline-runtime
// interpreter against the Iceberg providers. ADR-0045 Phase C.5 —
// replaces the prior `spark-submit` orchestrator and the
// pipeline-build-service spec-fetch loop.
package runner

import (
	"fmt"
	"os"
	"strings"
)

// Args is the parsed CLI surface. Phase C.5 drops the `--inline-sql`
// / `--inline-format` flags the SparkApplication template fed and
// retires `--pipeline-build-url` (no more HTTP spec fetch). The
// Plan now arrives base64-encoded in the PIPELINE_PLAN_B64 env var
// the dispatcher's Job manifest sets.
type Args struct {
	// Logging scope only — the actual table reads/writes come from
	// the Plan, not these flags.
	PipelineID    string
	RunID         string
	InputDataset  string
	OutputDataset string

	// Iceberg catalog REST endpoint. Required outside of --smoke.
	CatalogURI string
	// Optional catalog auth knobs used by the IcebergReader; mirror
	// the Lakekeeper-flavoured config the Phase A indexer carried.
	CatalogWarehouse  string
	CatalogCredential string
	OAuthTokenURI     string
	OAuthScope        string

	// HTTP append adapter URL. Defaults to CatalogURI when blank —
	// matches the Phase B convention iceberg-catalog-service ships.
	TableWriterURL string

	// Internal token sent on the HTTP append adapter request if set
	// (matches the dev secret used by the Phase B sinks).
	InternalToken string

	// Smoke skips the Plan execution: validates the Plan only.
	Smoke bool

	// HealthAddr is the bind address for the /healthz + /metrics
	// sidecar. Empty falls back to OF_PIPELINE_RUNNER_HEALTH_ADDR,
	// then PORT, then 0.0.0.0:9090.
	HealthAddr string

	// LogFormat: "text" (default) or "json".
	LogFormat string

	// Version stamps log lines and /healthz.
	Version string
}

// ParseArgs parses argv into [Args]. Returns an error on unknown
// flags or missing values so misconfigured Jobs surface a clear
// diagnostic rather than dropping the offending flag silently.
func ParseArgs(argv []string) (Args, error) {
	a := defaultArgs()
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--smoke":
			a.Smoke = true
		case "--pipeline-id", "--run-id", "--input-dataset", "--output-dataset",
			"--catalog-uri", "--catalog-warehouse", "--catalog-credential",
			"--oauth-token-uri", "--oauth-scope",
			"--table-writer-url", "--internal-token",
			"--health-addr", "--log-format":
			if i+1 >= len(argv) {
				return Args{}, fmt.Errorf("flag %s requires a value", arg)
			}
			val := strings.TrimSpace(argv[i+1])
			switch arg {
			case "--pipeline-id":
				a.PipelineID = val
			case "--run-id":
				a.RunID = val
			case "--input-dataset":
				a.InputDataset = val
			case "--output-dataset":
				a.OutputDataset = val
			case "--catalog-uri":
				a.CatalogURI = val
			case "--catalog-warehouse":
				a.CatalogWarehouse = val
			case "--catalog-credential":
				a.CatalogCredential = val
			case "--oauth-token-uri":
				a.OAuthTokenURI = val
			case "--oauth-scope":
				a.OAuthScope = val
			case "--table-writer-url":
				a.TableWriterURL = val
			case "--internal-token":
				a.InternalToken = val
			case "--health-addr":
				a.HealthAddr = val
			case "--log-format":
				a.LogFormat = val
			}
			i++
		default:
			return Args{}, fmt.Errorf("unknown flag: %s", arg)
		}
	}
	return a, a.applyEnvFallbacks()
}

func defaultArgs() Args {
	return Args{
		LogFormat: "text",
	}
}

// applyEnvFallbacks fills empty fields from environment variables.
// Operators ship these via the Job env, so the CLI can stay short.
func (a *Args) applyEnvFallbacks() error {
	a.HealthAddr = firstNonEmpty(a.HealthAddr,
		os.Getenv("OF_PIPELINE_RUNNER_HEALTH_ADDR"),
		portToAddr(os.Getenv("PORT")),
		"0.0.0.0:9090")
	a.CatalogURI = firstNonEmpty(a.CatalogURI, os.Getenv("ICEBERG_CATALOG_URL"))
	a.CatalogWarehouse = firstNonEmpty(a.CatalogWarehouse, os.Getenv("ICEBERG_WAREHOUSE"))
	a.CatalogCredential = firstNonEmpty(a.CatalogCredential, os.Getenv("ICEBERG_CATALOG_CREDENTIAL"))
	a.OAuthTokenURI = firstNonEmpty(a.OAuthTokenURI, os.Getenv("ICEBERG_OAUTH_TOKEN_URI"))
	a.OAuthScope = firstNonEmpty(a.OAuthScope, os.Getenv("ICEBERG_OAUTH_SCOPE"))
	a.TableWriterURL = firstNonEmpty(a.TableWriterURL, os.Getenv("ICEBERG_TABLE_WRITER_URL"), a.CatalogURI)
	if a.LogFormat != "" && a.LogFormat != "text" && a.LogFormat != "json" {
		return fmt.Errorf("--log-format must be 'text' or 'json', got %q", a.LogFormat)
	}
	if a.Smoke {
		return nil
	}
	if strings.TrimSpace(a.PipelineID) == "" {
		return fmt.Errorf("--pipeline-id is required outside of --smoke mode")
	}
	if strings.TrimSpace(a.RunID) == "" {
		return fmt.Errorf("--run-id is required outside of --smoke mode")
	}
	if strings.TrimSpace(a.CatalogURI) == "" {
		return fmt.Errorf("--catalog-uri (or ICEBERG_CATALOG_URL) is required outside of --smoke mode")
	}
	if strings.TrimSpace(a.TableWriterURL) == "" {
		return fmt.Errorf("--table-writer-url (or ICEBERG_TABLE_WRITER_URL) is required outside of --smoke mode")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func portToAddr(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		return ""
	}
	return "0.0.0.0:" + port
}
