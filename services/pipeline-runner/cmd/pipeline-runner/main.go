// Command pipeline-runner executes a pipelineplan.Plan against
// Iceberg providers. ADR-0045 Phase C.5 — replaces the prior
// Scala-on-Spark binary and its `spark-submit` shell-out. The
// dispatcher (Phase C.4.a) ships the Plan as base64-encoded JSON
// in the PIPELINE_PLAN_B64 env var; this binary decodes it,
// validates it, and runs it via libs/pipeline-runtime.
//
//	pipeline-runner
//	  --pipeline-id        <RID>
//	  --run-id             <ULID>
//	  --input-dataset      <catalog.namespace.table>   (informational, log scope)
//	  --output-dataset     <catalog.namespace.table>   (informational, log scope)
//	  --catalog-uri        <https://...>               (Iceberg REST catalog)
//	  [--catalog-warehouse <name>]
//	  [--catalog-credential <user:secret>]
//	  [--oauth-token-uri   <url>]
//	  [--oauth-scope       <scope>]
//	  [--table-writer-url  <url>]                       defaults to --catalog-uri
//	  [--internal-token    <token>]
//	  [--health-addr       <host:port>]                 default 0.0.0.0:9090
//	  [--log-format        text|json]                   default text
//	  [--smoke]                                         validate Plan and exit
//
// Env: PIPELINE_PLAN_B64 carries the base64-JSON Plan. Most flags
// also accept env-var fallbacks (see Args.applyEnvFallbacks).
package main

import (
	"fmt"
	"os"

	"github.com/openfoundry/openfoundry-go/services/pipeline-runner/internal/runner"
)

var version = "dev"

func main() {
	args, err := runner.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipeline-runner: %s\n", err)
		os.Exit(2)
	}

	args.Version = version
	if err := runner.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "pipeline-runner: FAILED %s\n", err)
		os.Exit(1)
	}
}
