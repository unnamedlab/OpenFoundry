# `pipeline-build-service` (Go)

## LLM quick context (current code)

Owns pipeline definitions, builds, schedules, job specs/logs, pipeline type lifecycle, distributed execution dispatch, and pipeline authoring/orchestration APIs.

Agent note: large service with local/Kubernetes/Spark/runner integrations and many migrations.

Current surface:
- `/api/v1/pipelines*`
- `/api/v1/builds*`
- `/api/v1/schedules*`
- `/api/v1/job-specs*`
- `pipeline runs/logs/authoring lifecycle routes`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `25` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `dispatch`, `domain`, `handler`, `iceberg`, `logs`, `models`, `plancomposer`, `postgres`, `runtime`, `server`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `AI_SERVICE_BEARER`, `AI_SERVICE_URL`, `DATABASE_URL`, `DATASET_SERVICE_BEARER`, `DATASET_SERVICE_URL`, `DATA_DIR`, `DISTRIBUTED_COMPUTE_POLL_INTERVAL_MS`, `DISTRIBUTED_COMPUTE_TIMEOUT_SECS`
- `DISTRIBUTED_PIPELINE_WORKERS`, `FOUNDRY_ICEBERG_CATALOG_BEARER`, `FOUNDRY_ICEBERG_CATALOG_URL`, `HOST`, `JWT_SECRET`, `KUBECONFIG`, `KUBERNETES_API_URL`, `KUBERNETES_BEARER_TOKEN`
- `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT`, `LOCAL_STORAGE_ROOT`, `OBJECT_DATABASE_SERVICE_BEARER`, `OBJECT_DATABASE_SERVICE_URL`, `ONTOLOGY_DEFINITION_SERVICE_BEARER`, `ONTOLOGY_DEFINITION_SERVICE_URL`, `OPENFOUNDRY_DISTRIBUTED_CLUSTER_SMOKE`
- `OPENFOUNDRY_DISTRIBUTED_INPUT_DATASET_RID`, `OPENFOUNDRY_DISTRIBUTED_INPUT_TABLE`, `OPENFOUNDRY_DISTRIBUTED_OUTPUT_DATASET_RID`, `OPENFOUNDRY_DISTRIBUTED_OUTPUT_TABLE`, `OPENFOUNDRY_PIPELINE_AIP_PROVIDER_SMOKE`, `OPENFOUNDRY_PIPELINE_LLM_PROVIDER_SMOKE`, `PIPELINE_RUNNER_IMAGE`, `PIPELINE_RUNNER_NAMESPACE`
- `PORT`, `PYTHON_SIDECAR_BINARY`, `PYTHON_SIDECAR_TIMEOUT_SECONDS`, `S3_ACCESS_KEY`, `S3_ENDPOINT`, `S3_REGION`, `S3_SECRET_KEY`, `SERVICE_VERSION`
- `SPARK_NAMESPACE`, `STORAGE_BACKEND`, `STORAGE_BUCKET`, `WORKFLOW_SERVICE_URL`

Keep this section in sync when changing routes, config, or persistence behavior.

Build / execution side of Pipeline Builder. The Go service now mounts the
Rust route surface under `/api/v1/data-integration`, `/api/v1/pipeline/builds`
and `/v1`, while keeping the older `/api/v1/*` compatibility aliases for
callers that already moved to the Go namespace.

## Compatibility naming

Pipeline Builder public payloads should follow the frozen terminology in
[`docs/reference/foundry-compatibility-glossary.md`](../../docs/reference/foundry-compatibility-glossary.md):
`pipeline`, `pipeline_node`, `transform`, `build`, `job`, `dataset_output`,
`object_output`, and `link_output`. In short: use `id` for internal UUIDs,
`rid` for stable external resources, `transform_type` for node behavior, and
`output_dataset_id`/`output_dataset_rid` according to the API surface.

## Pipeline IR

Authoring CRUD now persists a stable `pipeline_ir.v1` graph under the existing
`pipelines.dag` JSONB column. The IR models nodes, ports, edges, resources,
inputs, outputs, transform config, output schema, preview schema, validation
state, and version metadata. Legacy DAG rows stored as `[]PipelineNode` remain
readable: `Pipeline.ParsedIR()` normalizes both shapes, while `ParsedNodes()`
keeps execution adapters isolated from the new authoring envelope.

New create/update payloads may send either `ir`, `dag`, or compatibility
`nodes`; handlers normalize them to the canonical IR before persistence and
reject duplicate node IDs, missing dependencies, invalid edge ports, cycles, and
unsupported IR versions.

## Port status

| Component | Status |
|---|---|
| Health (`/health`, `/healthz`) + Prometheus (`/metrics`) | ✅ |
| Route surface | ✅ `go run ./tools/route-audit --services pipeline-build-service` reports no `501` or `empty-envelope` states |
| Build resolution (`CreateBuild`, `DryRunResolve`) | ✅ wired through injected JobSpec, dataset-versioning, lock and build persistence ports |
| `/api/v1/data-integration` runs/build queue | ✅ backed by the production run repository when `DATABASE_URL` is configured; otherwise explicit `503` configuration errors |
| `/v1/builds`, `/v1/jobs`, `/v1/job-specs` | ✅ mounted and backed by build/job/log repositories where data access is required |
| SparkApplication `/api/v1/pipeline/builds/run` + status | ✅ mounted, Kubernetes-dispatched, and persisted in `pipeline_run_submissions` when `DATABASE_URL` is configured |
| Spark run list compatibility alias | 🟡 config-gated; returns recent `pipeline_run_submissions` rows instead of an empty envelope once the repository is configured |
| DAG executor / runtime dispatch | ✅ executor path accepts persisted plans or inline nodes; lightweight table transforms run through the existing `pipeline-expression` runtime, while Python, Spark, and job-runner dispatch remain injectable runtime ports |
| Lightweight / Faster pipeline type | ✅ `pipeline_type=FASTER` persists through authoring CRUD, accepts `LIGHTWEIGHT` aliases, tags execution plans with `preferred_runtime=lightweight_table`, and uses the existing OpenFoundry local table/expression runtime without DuckDB/DataFusion |
| Spark / Flink distributed pipeline type | ✅ `pipeline_type=DISTRIBUTED` persists with `distributed_config`; unchanged graph nodes dispatch through the `DistributedTransformRunner` port, Spark/PySpark submit SparkApplication CRs through existing Kubernetes wiring, and Flink remains an injectable runtime adapter with an explicit config-gated error until supplied |
| Python transform node | ✅ sidecar-backed `python` nodes receive upstream rows as `prepared_inputs`, enforce optional package allowlists, clamp timeouts, capture stdout/stderr, and publish `result_rows` for downstream output commits |
| LLM node and AIP generation | ✅ `llm` nodes dispatch through the configured AI service, publish generated values as normal table columns, and `/api/v1/pipelines/{id}/aip/generate` appends provider-generated transform nodes to the graph with preview feedback |
| Reusable functions / UDF nodes | ✅ versioned registry functions appear in the transform catalog; expression UDF nodes execute through the lightweight runtime with optional `function_auto_upgrade`; Python function catalog entries remain sidecar-gated separately from Python transform nodes |
| Pipeline authoring lifecycle | ✅ draft DAGs, published DAGs, branch names, proposal state, version history, publish, and restore are persisted through `pipeline_versions`; builds prefer the published graph when present |
| Build orchestration lifecycle | ✅ pipeline runs persist `queued`, `running`, `succeeded`, `failed`, and `cancelled`; each run stores rich `node_results` with transition events, attempts, row counts, schema deltas, output resources, and log RIDs for the run detail UI |
| Dataset output commits | ✅ non-Iceberg dataset outputs POST successful runtime rows to `dataset-versioning` with inferred schema, preview rows, file metadata, and lineage before marking `job_outputs` committed |
| Ontology object/link outputs | ✅ object outputs commit a backing dataset, create/update ontology-definition object types and properties, and materialize rows into object-database bridge objects; link outputs deploy/update link types and write link rows through object-database |
| Logs | ✅ list/SSE/emit/ws surfaces are mounted; history/emit/ws paths are config-gated on live log store/subscriber wiring |
| Iceberg output client (ADR-0041) | 🟡 config-gated by `FOUNDRY_ICEBERG_CATALOG_URL`; boot warning remains intentional |
| Legacy pipeline authoring CRUD aliases | ✅ mounted through the repository-backed authoring lifecycle when `DATABASE_URL` is configured; otherwise explicit `503` configuration errors |

Run `go run ./tools/route-audit --services pipeline-build-service` to regenerate
the handler-classification snapshot for this service. Productive handlers either
execute real repository/runtime work or return a machine-readable configuration
error that names the missing adapter.

## Build & run

```sh
go build -o bin/pipeline-build-service ./services/pipeline-build-service/cmd/pipeline-build-service
go test ./services/pipeline-build-service/...
```

## Configuration

| Variable | Default |
|---|---|
| `HOST` | `0.0.0.0` |
| `PORT` | `50081` |
| `JWT_SECRET` | (required for authenticated route groups) |
| `DATABASE_URL` | unset; repository-backed handlers return explicit `503` until configured |
| `DATA_DIR` | `/var/lib/openfoundry/pipeline-build` |
| `DATASET_SERVICE_URL` | `http://localhost:50079` |
| `DATASET_SERVICE_BEARER` | unset; forwarded as `Authorization: Bearer` for dataset output commits when configured |
| `ONTOLOGY_DEFINITION_SERVICE_URL` | `http://localhost:50103` |
| `ONTOLOGY_DEFINITION_SERVICE_BEARER` | unset; forwarded as `Authorization: Bearer` for ontology object output deploys when configured |
| `OBJECT_DATABASE_SERVICE_URL` | `http://localhost:50104` |
| `OBJECT_DATABASE_SERVICE_BEARER` | unset; forwarded as `Authorization: Bearer` for object output materialization when configured |
| `WORKFLOW_SERVICE_URL` | `http://localhost:50080` |
| `AI_SERVICE_URL` | `http://localhost:50127` |
| `AI_SERVICE_BEARER` | unset; forwarded as `Authorization: Bearer` for LLM nodes and AIP-assisted generation when configured |
| `STORAGE_BACKEND` | `local` |
| `STORAGE_BUCKET` | unset |
| `S3_*` | unset |
| `LOCAL_STORAGE_ROOT` | unset |
| `DISTRIBUTED_PIPELINE_WORKERS` | `4` |
| `DISTRIBUTED_COMPUTE_POLL_INTERVAL_MS` | `1000` |
| `DISTRIBUTED_COMPUTE_TIMEOUT_SECS` | `1800` |
| `SPARK_NAMESPACE` | `openfoundry-spark` |
| `PIPELINE_RUNNER_IMAGE` | `openfoundry/pipeline-runner:dev` |
| `KUBERNETES_API_URL` / in-cluster service env | unset; SparkApplication handlers return explicit `503` until kube wiring is available |
| `KUBERNETES_BEARER_TOKEN` | unset |
| `FOUNDRY_ICEBERG_CATALOG_URL` | unset (boot-time warning matches Rust) |
| `FOUNDRY_ICEBERG_CATALOG_BEARER` | unset |
