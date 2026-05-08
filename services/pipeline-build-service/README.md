# `pipeline-build-service` (Go)

Build / execution side of Pipeline Builder. The Go service now mounts the
Rust route surface under `/api/v1/data-integration`, `/api/v1/pipeline/builds`
and `/v1`, while keeping the older `/api/v1/*` compatibility aliases for
callers that already moved to the Go namespace.

## Port status

| Component | Status |
|---|---|
| Health (`/health`, `/healthz`) + Prometheus (`/metrics`) | ✅ |
| Rust route shape | ✅ route-audit reports no `missing`, `501`, or `empty-envelope` states for `pipeline-build-service` |
| Build resolution (`CreateBuild`, `DryRunResolve`) | ✅ wired through injected JobSpec, dataset-versioning, lock and build persistence ports |
| `/api/v1/data-integration` runs/build queue | ✅ backed by the production run repository when `DATABASE_URL` is configured; otherwise explicit `503` configuration errors |
| `/v1/builds`, `/v1/jobs`, `/v1/job-specs` | ✅ mounted and backed by build/job/log repositories where data access is required |
| SparkApplication `/api/v1/pipeline/builds/run` + status | ✅ mounted, Kubernetes-dispatched, and persisted in `pipeline_run_submissions` when `DATABASE_URL` is configured |
| Spark run list compatibility alias | 🟡 config-gated; returns recent `pipeline_run_submissions` rows instead of an empty envelope once the repository is configured |
| DAG executor / runtime dispatch | ✅ executor path accepts persisted plans or inline nodes; Python and job-runner dispatch are injectable runtime ports |
| Logs | ✅ list/SSE/emit/ws surfaces are mounted; history/emit/ws paths are config-gated on live log store/subscriber wiring |
| Iceberg output client (ADR-0041) | 🟡 config-gated by `FOUNDRY_ICEBERG_CATALOG_URL`; boot warning remains intentional |
| Legacy pipeline authoring CRUD aliases | 🟡 explicit `503` configuration errors; they are compatibility aliases and no longer return `501` or fake empty data |

Use `openfoundry-go/docs/migration/route-parity-audit.md` as the generated
source of truth for route-shape parity. Productive handlers either execute real
repository/runtime work or return a machine-readable configuration error that
names the missing adapter.

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
| `WORKFLOW_SERVICE_URL` | `http://localhost:50080` |
| `AI_SERVICE_URL` | `http://localhost:50127` |
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
