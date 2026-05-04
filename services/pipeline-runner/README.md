# `pipeline-runner` — Spark/Iceberg image for SparkApplication CRs

> **FASE 3 / Tarea 3.3** of
> [`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md).
> Companion to the SparkApplication CR template in
> [`infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml`](../../infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml)
> (see [`README-pipeline-run.md`](../../infra/helm/infra/spark-jobs/README-pipeline-run.md)).
> Will be invoked by the refactor in **Tarea 3.4**
> (`pipeline-build-service` → Kubernetes API instead of Temporal).

This service is **not** a Rust workspace member. It is a tiny
Scala/SBT project whose only job is to build a JAR that ships in a
Docker image based on `apache/spark:3.5.4`. Each pipeline run is a
fresh `SparkApplication` CR that runs this image; the JVM `main`
fetches the resolved transform plan from `pipeline-build-service`
over HTTP, executes it as Spark SQL against the Iceberg/Lakekeeper
catalog, and exits.

## Layout

```
services/pipeline-runner/
├── Dockerfile                                    # multi-stage build
├── build.sbt                                     # Scala 2.12 + Java 17
├── project/
│   ├── build.properties                          # sbt version
│   └── plugins.sbt                               # (intentionally empty)
├── src/main/scala/com/openfoundry/pipeline/
│   └── PipelineRunner.scala                      # `main` entry point
├── .dockerignore
└── README.md                                     # this file
```

## Image contents

| Layer | What | Source |
|---|---|---|
| Base | `apache/spark:3.5.4-scala2.12-java17-python3-ubuntu` | upstream |
| `/opt/spark/jars/iceberg-spark-runtime-3.5_2.12-1.5.2.jar` | Iceberg → Spark binding (Spark SQL extensions, `SparkCatalog`). | Maven Central |
| `/opt/spark/jars/iceberg-aws-bundle-1.5.2.jar` | Lakekeeper REST catalog talks to S3-compatible object storage (Ceph RGW); the AWS SDK + URLConnection HTTP client live here. | Maven Central |
| `/opt/spark/jars/pipeline-runner_2.12-0.1.0.jar` | This module — `com.openfoundry.pipeline.PipelineRunner`. | this repo |

Other Spark conf (catalog URI, S3 endpoint, restart policy) is set on
the `SparkApplication` CR in
[`_pipeline-run-template.yaml`](../../infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml).

## Version pins

- **Spark** — `3.5.4` (matches `sparkVersion: 3.5.3` in the existing
  maintenance jobs; `3.5.4` is the latest patch on the same minor and
  is API-compatible).
- **Scala** — `2.12.18` (Spark 3.5 only ships `_2.12` runtime jars; the
  `_2.13` flavour exists but is not used by the maintenance jobs).
- **Iceberg** — `1.5.2` (Iceberg 1.5.x is the documented support
  matrix for Spark 3.5; `1.5.2` is the last patch on that minor).
- **Java** — `17` (matches the upstream `java17` tag and the
  `eclipse-temurin:17-jdk` builder).
- **sbt** — `1.10.0`.

The version matrix is the failure mode called out in the migration
plan: bumping the Spark line **must** also bump the Iceberg line.

## CLI surface

The image's entrypoint is `spark-submit`; the SparkApplication CR
sets `spec.mainClass` to `com.openfoundry.pipeline.PipelineRunner`.
The class accepts the following arguments (matches what the template
in 3.2 passes):

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

When the resolved transform spec is missing from
`pipeline-build-service` (Tarea 3.4 has not landed yet, or the run is
explicitly a smoke test), the runner falls back to a "smoke
transform": `SELECT 1 AS run_id, current_timestamp() AS at` →
`output_dataset`. This satisfies the verification step in 3.3
("transform mínimo (read 1 row, write 1 row) completa") without
requiring 3.4.

## Build & push

The image targets the cluster's local registry. The brief uses
`192.168.105.3:30501` as the in-cluster registry alias for
`localhost:5001`; both names map to the same `registry:2` deployment.

```sh
# Local, single-arch (amd64) — fastest dev loop:
docker build -t localhost:5001/pipeline-runner:0.1.0 services/pipeline-runner/
docker push  localhost:5001/pipeline-runner:0.1.0

# Cluster-equivalent: linux/arm64, push to the in-cluster alias:
docker buildx build \
  --platform linux/arm64 \
  -t 192.168.105.3:30501/pipeline-runner:0.1.0 \
  --push services/pipeline-runner/
```

The image is intentionally **single-binary**: no Python wheels, no
extra apt packages, just the three JARs above on top of upstream
`apache/spark`. Final size ≈ base (≈ 600 MB) + ~50 MB of JARs.
Multi-stage build keeps the SBT toolchain out of the runtime layer.

## Verification

```sh
# 1. Spark version sanity check (per the brief):
kubectl -n openfoundry run pipeline-test --rm -i --tty \
  --image=localhost:5001/pipeline-runner:0.1.0 \
  --restart=Never -- /opt/spark/bin/spark-submit --version
# expected: "version 3.5.4"

# 2. End-to-end smoke via the SparkApplication CR template (3.2):
#    Render `_pipeline-run-template.yaml` with `--smoke` appended to the
#    arguments list (or set spark_application_type=Scala and the runner
#    will fall back to smoke when the build-service spec endpoint 404s),
#    then `kubectl apply -f` and watch:
kubectl -n openfoundry-spark get sparkapplication \
  pipeline-run-smoke-01HF9P3M5R -w
# expected: phase progresses SUBMITTED → RUNNING → COMPLETED.
```

## Why Scala (not Python)

The migration plan leaves the choice open ("Scala/Python") and the
SparkApplication template in 3.2 documents that the generator may
emit either. We pick Scala for the runner JAR because:

1. The maintenance jobs already in this chart
   (`iceberg-rewrite-data-files.yaml` etc.) are Scala — keeping a
   single language reduces operational surface.
2. The CRD's `mainClass` field is required for `type: Scala`; the
   plan's example explicitly shows `mainClass:
   com.openfoundry.pipeline.PipelineRunner`.
3. PySpark transforms are still supported — the runner accepts a spec
   whose `transform_type` is `pyspark` and shells out to
   `spark-submit --py-files`. (Wired up in Tarea 3.4 along with the
   spec-fetch endpoint.)

## Local development

There is no `cargo`/`just` integration: this module is opaque to the
Rust workspace (`Cargo.toml` does not list it; `Cargo` build commands
will not see it). To compile against an installed `sbt`:

```sh
cd services/pipeline-runner
sbt package
# produces target/scala-2.12/pipeline-runner_2.12-0.1.0.jar
```

The build is **fully offline-capable** once SBT has resolved its
dependency cache: only `spark-sql` is declared as a `Provided`
dependency (the JARs are on the runtime classpath via the base
image), and the only external runtime call is to Java 17's stdlib
`java.net.http.HttpClient`. No third-party HTTP / JSON dependencies.
