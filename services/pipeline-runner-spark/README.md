# `pipeline-runner-spark` — superseded by ADR-0045

## LLM quick context (current code)

Historical Scala/Spark fat-JAR module retained as a migration reference after ADR-0045; the active runtime is the Go `services/pipeline-runner`.

Agent note: this directory is **not** a Go HTTP microservice and should not be treated as the active runtime unless ADR-0045 is reversed.

Current surface:
- CLI arguments consumed by `PipelineRunner`: `--pipeline-id`, `--run-id`, `--input-dataset`, `--output-dataset`, `--catalog`, `--catalog-uri`, `--inline-sql`, `--inline-format`, and `--smoke`.
- The historical runner builds a `SparkSession`, configures an Iceberg REST catalog, executes the provided SQL, and writes the final DataFrame to the output Iceberg table.
- There is no HTTP listener in this directory; `/healthz` and `/metrics` belong to the Go `pipeline-runner` wrapper.

State/dependency hints:
- No SQL migration files live under this service directory.
- Scala build files: `build.sbt`, `project/build.properties`, and `project/plugins.sbt`.
- Local service files present: `Dockerfile`.

Configuration signals:
- `VERSION` is read by `build.sbt` and the Docker build to stamp the assembled JAR.
- Spark catalog/S3 details are passed through Spark configuration by the Go orchestrator and Kubernetes/Spark templates, not by `os.Getenv` in this directory.

Keep this section in sync when changing routes, config, or persistence behavior.

> **SUPERSEDED.** This Scala module is retired by ADR-0045 and is no
> longer used by the runtime. Phase D of the same ADR removes the
> directory entirely; until that PR lands the code stays in-tree as
> a reference for the migration.
>
> The runtime now uses [`services/pipeline-runner`](../pipeline-runner/)
> (Go, distroless), which executes a typed
> [`pipelineplan.Plan`](../../libs/pipeline-plan/) via
> [`libs/pipeline-runtime`](../../libs/pipeline-runtime/) against Iceberg.
> Submission goes through `services/pipeline-build-service`'s
> [`internal/dispatch`](../pipeline-build-service/internal/dispatch/)
> as a `batch/v1 Job`, not a `SparkApplication` CR.
>
> Migration map (see
> [ADR-0045](../../docs/architecture/adr/ADR-0045-eliminate-pipeline-runner-spark-pure-go-runtime.md)
> and the
> [Phase 0 inventory](../../docs/migration/pipeline-runner-spark-to-go-inventory.md)):
>
> | Was (Scala main) | Now |
> |---|---|
> | `com.openfoundry.pipeline.PipelineRunner` | `services/pipeline-runner` decoding a `pipelineplan.Plan` |
> | `com.openfoundry.indexer.IcebergToObjectStoreIndexer` | `services/iceberg-object-indexer` (Phase A, PR #55) |
> | `com.openfoundry.audit.ActionLogStreamSink` | `services/action-log-sink` (Phase B, PR #66) |

---

# `pipeline-runner-spark` (Scala 2.12 / Spark 3.5) — historical

Companion JAR consumed by the Go [`pipeline-runner`](../pipeline-runner/) at
runtime via `spark-submit --class com.openfoundry.pipeline.PipelineRunner`.
Embeds only the OpenFoundry transform main; Spark, Iceberg and Hadoop are
declared `Provided` and ride on the base Spark image baked into
[`infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml`](../../infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml).

## Build

```sh
cd services/pipeline-runner-spark
sbt assembly
# → target/scala-2.12/pipeline-runner-spark-<version>.jar
```

The resulting JAR is copied into the `pipeline-runner` Docker image at
`/opt/spark/jars/pipeline-runner-spark.jar` (the path
`OF_PIPELINE_RUNNER_SPARK_JAR` defaults to in `services/pipeline-runner`).

## Argument contract

Identical to the Go orchestrator's CLI, plus the inline-SQL pair the
orchestrator resolves at runtime:

```
--pipeline-id    <string>
--run-id         <string>
--input-dataset  <iceberg-ref>     # informational, the SQL already references it
--output-dataset <iceberg-ref>     # writeTo(target).createOrReplace()
--catalog        <string>          # Iceberg Spark catalog name (default: lakekeeper)
--catalog-uri    <url>             # Lakekeeper REST URL
--inline-sql     <string>          # SELECT body composed by pipeline-build-service
--inline-format  iceberg           # only "iceberg" is supported today
[--smoke]                          # validate args + skip execution
```

Exit codes:

* `0` — transform succeeded, snapshot published.
* `1` — Spark/Iceberg failure (logged before exit).
* `2` — argument parse failure.

## Logging

Every line is prefixed `[pipeline-runner-spark pipeline_id=… run_id=…]` to
match the Go orchestrator's grep-friendly format. SparkApplication driver
stdout is mounted by Spark Operator under `kubectl logs`, and OpenFoundry's
log viewer pin-folds by `pipeline_id`/`run_id`.

## CI

`sbt test` (no Scala tests yet — the module is a thin entrypoint and is
exercised end-to-end by the smoke test described in
[`docs/poc-online-retail/README.md`](../../docs/poc-online-retail/README.md)).
