# `pipeline-runner-spark` (Scala 2.12 / Spark 3.5)

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
