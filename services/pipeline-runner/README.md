# `pipeline-runner` — orchestrator binary for SparkApplication CRs

Go port of the Scala `services/pipeline-runner` module. Spark itself
has no Go runtime, so this binary takes over only the orchestration
surface (CLI parsing, spec resolution, smoke fallback, log prefix)
and hands the actual `df.writeTo(...).append()` execution back to
`spark-submit`. The SparkApplication CR template at
`infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml`
remains valid: it still launches a Java process, just one that wraps
this Go orchestrator instead of running Scala `main` directly.

## CLI surface

Identical to the Scala port (matched flag-for-flag so
pipeline-build-service does not need to re-render the spec):

```
--pipeline-id     <string>   pipeline RID, used to look up the spec
--run-id          <string>   per-run ULID for log scoping
--input-dataset   <string>   Iceberg table reference (catalog.namespace.table)
--output-dataset  <string>   Iceberg table reference
--catalog         <string>   Spark catalog name, e.g. `lakekeeper`
--catalog-uri     <string>   Lakekeeper REST URL
[--pipeline-build-url <url>] override for `OF_PIPELINE_BUILD_URL` env
                             (default `http://pipeline-build-service.openfoundry.svc:50081`)
[--smoke]                    skip the HTTP fetch, run a 1-row read/write
```

## Environment overrides

| Variable | Default | Purpose |
|---|---|---|
| `OF_PIPELINE_BUILD_URL` | `http://pipeline-build-service.openfoundry.svc:50081` | Spec endpoint base URL when the flag is unset. |
| `OF_PIPELINE_RUNNER_SPARK_MODE` | `spark-submit` | Set to `stub` in CI so jobs short-circuit after spec resolution. |
| `OF_PIPELINE_RUNNER_SPARK_SUBMIT` | `/opt/spark/bin/spark-submit` | Override the submission binary (test harnesses). |
| `OF_PIPELINE_RUNNER_SPARK_JAR` | `/opt/spark/jars/pipeline-runner-spark.jar` | Path to the Scala JAR holding the Spark `main`. |
| `OF_PIPELINE_RUNNER_SPARK_MAIN_CLASS` | `com.openfoundry.pipeline.PipelineRunner` | Mirror of the original Scala entrypoint. |
| `OF_PIPELINE_RUNNER_EXTRA_CONF` | unset | Extra `spark.<key>=<val>` strings, space-separated, forwarded as `--conf`. |

## Smoke fallback

When `--smoke` is passed, when the spec endpoint is unreachable or
when it returns 404, the runner uses the built-in 1-row transform:

```sql
SELECT CAST('<run_id>' AS STRING) AS run_id,
       CAST(current_timestamp() AS TIMESTAMP) AS observed_at
```

This lets the SparkApplication CR template be exercised end-to-end
against a freshly-deployed Iceberg catalog without depending on
pipeline-build-service having shipped the spec endpoint.

## Build

```sh
go build -o bin/pipeline-runner ./services/pipeline-runner/cmd/pipeline-runner
```

## Test

```sh
go test ./services/pipeline-runner/...
```

## Image

```sh
docker build -t openfoundry/pipeline-runner:dev -f services/pipeline-runner/Dockerfile .
```

The Dockerfile pulls the Iceberg + Iceberg AWS bundle JARs into
`/opt/spark/jars/`, drops the Go binary on `apache/spark:3.5.4-...`
and runs the binary as the upstream `spark` user (UID 185). The
companion Scala JAR with `com.openfoundry.pipeline.PipelineRunner`
is copied in as a separate Helm-managed layer during image promotion.
