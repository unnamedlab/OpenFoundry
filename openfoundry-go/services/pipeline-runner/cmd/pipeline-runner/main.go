// Command pipeline-runner is the orchestrator entrypoint launched by
// the SparkApplication CR generated from
// infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml.
//
// Origin: ports services/pipeline-runner (Scala/SBT module that
// produced pipeline-runner_2.12-0.1.0.jar and ran inside the
// apache/spark base image). Spark itself has no Go runtime; this
// Go port keeps the CLI surface, the spec-fetch fallback flow and
// the smoke transform, then shells out to `spark-submit` so the
// SparkApplication CR template stays unchanged.
//
//	pipeline-runner
//	  --pipeline-id <RID>
//	  --run-id      <ULID>
//	  --input-dataset  <catalog.namespace.table>
//	  --output-dataset <catalog.namespace.table>
//	  --catalog        <name, e.g. lakekeeper>
//	  --catalog-uri    <https://...>
//	  [--pipeline-build-url http://pipeline-build-service.openfoundry.svc:50081]
//	  [--smoke]
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
		fmt.Fprintln(os.Stderr, runner.Usage())
		os.Exit(2)
	}

	args.Version = version
	if err := runner.Run(args); err != nil {
		fmt.Fprintf(os.Stderr, "pipeline-runner: FAILED %s\n", err)
		os.Exit(1)
	}
}
