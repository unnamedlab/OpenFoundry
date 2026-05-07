// Package runner mirrors the Scala PipelineRunner main: argument
// parsing, spec resolution and Spark submission orchestration.
package runner

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// Args is the parsed CLI surface of pipeline-runner. The flag set
// is identical to the Scala main — pipeline-build-service emits
// arguments deterministically from the SparkApplication template.
type Args struct {
	PipelineID       string
	RunID            string
	InputDataset     string
	OutputDataset    string
	Catalog          string
	CatalogURI       string
	PipelineBuildURL string
	Smoke            bool
	Version          string
}

// DefaultPipelineBuildURL mirrors the in-cluster Service DNS for
// pipeline-build-service (Cargo.toml ENV PORT=50081).
const DefaultPipelineBuildURL = "http://pipeline-build-service.openfoundry.svc:50081"

// envPipelineBuildURL is the env var that overrides the default
// when neither the CLI flag nor the YAML value is set.
const envPipelineBuildURL = "OF_PIPELINE_BUILD_URL"

// ParseArgs implements the same flag grammar as the Scala matcher:
// repeated flags overwrite earlier values; unknown flags raise an
// error rather than being silently dropped.
func ParseArgs(argv []string) (Args, error) {
	a := Args{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch arg {
		case "--smoke":
			a.Smoke = true
		case "--pipeline-id", "--run-id", "--input-dataset", "--output-dataset",
			"--catalog", "--catalog-uri", "--pipeline-build-url":
			if i+1 >= len(argv) {
				return Args{}, fmt.Errorf("flag %s requires a value", arg)
			}
			val := strings.TrimSpace(argv[i+1])
			if val == "" {
				return Args{}, fmt.Errorf("flag %s requires a non-empty value", arg)
			}
			switch arg {
			case "--pipeline-id":
				a.PipelineID = val
			case "--run-id":
				a.RunID = val
			case "--input-dataset":
				a.InputDataset = val
			case "--output-dataset":
				a.OutputDataset = val
			case "--catalog":
				a.Catalog = val
			case "--catalog-uri":
				a.CatalogURI = val
			case "--pipeline-build-url":
				a.PipelineBuildURL = val
			}
			i++
		default:
			return Args{}, fmt.Errorf("unknown argument: %s", arg)
		}
	}

	if a.PipelineBuildURL == "" {
		if env := strings.TrimSpace(os.Getenv(envPipelineBuildURL)); env != "" {
			a.PipelineBuildURL = env
		} else {
			a.PipelineBuildURL = DefaultPipelineBuildURL
		}
	}

	return a, validate(a)
}

func validate(a Args) error {
	required := map[string]string{
		"--pipeline-id":     a.PipelineID,
		"--run-id":          a.RunID,
		"--input-dataset":   a.InputDataset,
		"--output-dataset":  a.OutputDataset,
		"--catalog":         a.Catalog,
		"--catalog-uri":     a.CatalogURI,
	}
	for flag, val := range required {
		if val == "" {
			return errors.New("missing required flag: " + flag)
		}
	}
	return nil
}

// Usage returns the textual help block printed on argument errors.
// Matches the Scala formatting for parity with the existing runbooks.
func Usage() string {
	return strings.TrimSpace(`
usage: pipeline-runner
  --pipeline-id <id> --run-id <id>
  --input-dataset <catalog.namespace.table>
  --output-dataset <catalog.namespace.table>
  --catalog <name> --catalog-uri <url>
  [--pipeline-build-url <url>] [--smoke]
`)
}
