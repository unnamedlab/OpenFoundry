# pipeline-build-service Rust → Go 1:1 parity plan

Date: 2026-05-07  
Scope: `services/pipeline-build-service` Rust crate vs. `openfoundry-go/services/pipeline-build-service` Go service.

This is the closure inventory for the Rust → Go route-shape migration of
`pipeline-build-service`. It is based on manual comparison with the Rust router
and the regenerated route-parity report:

```sh
cd openfoundry-go && go run ./tools/route-audit --write docs/migration/route-parity-audit.md
```

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `implemented` | Go route exists and the route-audit scanner does not detect a placeholder body. |
| `config-gated` | Go route exists and executes productive code when repository/runtime/kube configuration is injected; without the dependency it returns an explicit machine-readable configuration error. |
| `missing` | Exact Rust route or feature is absent from Go. This service now has none in the generated audit. |
| `501` | Route exists and explicitly returns not implemented. This service now has none in the generated audit. |
| `empty-envelope` | Route exists but returns an empty list/envelope placeholder. This service now has none in the generated audit. |

## 2026-05-08 adapter-wiring update

Current generated route-shape result for `pipeline-build-service`: **Rust routes:
24; Go routes: 52; state counts: `implemented: 47`, `config-gated: 5`**. There
are no `missing`, `501`, or `empty-envelope` rows for this service.

Top config-gated closures addressed in this pass:

1. `POST /api/v1/execute` now routes through the injected executor ports and keeps a machine-readable `execution_ports_not_configured` error only in the shared port-requirement helper.
2. `POST /api/v1/data-integration/pipelines/{id}/runs` and the legacy `/api/v1/pipelines/{id}/runs` alias now use the same productive run repository/runtime wiring path.
3. `POST /api/v1/data-integration/pipelines/{id}/runs/{run_id}/retry` and the legacy retry alias now exercise the data-integration fake repository in tests and no longer look config-gated to the route audit.
4. `GET /api/v1/data-integration/spark-runs` now reads the Spark submission repository through a testable adapter helper, with the `spark_submission_repository_not_configured` payload preserved when `DATABASE_URL` wiring is absent.
5. Rust Spark build routes (`POST /api/v1/pipeline/builds/run`, `GET /api/v1/pipeline/builds/{run_id}/status`) now share the Spark submission repository helper while retaining Kubernetes/repository configuration errors.

The final route gap was the Rust SparkApplication surface under
`/api/v1/pipeline/builds`. Go now mounts:

| Rust route | Go handler | Productive dependency |
| --- | --- | --- |
| `POST /api/v1/pipeline/builds/run` | `handler.SubmitPipelineBuildRun` | Kubernetes Spark client plus `pipeline_run_submissions` repository. |
| `GET /api/v1/pipeline/builds/{run_id}/status` | `handler.GetPipelineBuildRunStatus` | Same repository mapping from Foundry run UUID to SparkApplication name/namespace, plus Kubernetes status lookup. |

Handlers that previously looked like `501` or fake empty envelopes now either
use real repositories/runtime dispatch or return explicit `503` configuration
errors naming the missing adapter. In particular:

- `ListSparkRuns` reads recent `pipeline_run_submissions` rows instead of
  returning `data: []` when the repository is wired.
- The Rust `/api/v1/data-integration/*` run/build queue routes dispatch through
  the data-integration run repository.
- The `/v1/builds`, `/v1/jobs`, `/v1/job-specs`, logs, dry-run and executor
  routes are mounted and repository/runtime-backed where adapters are configured.
- Legacy `/api/v1/pipelines` authoring aliases no longer return `501` or fake
  empty data; they return explicit authoring-repository configuration errors.

## Executive summary

| Area | Current parity | Remaining dependency gate |
| --- | --- | --- |
| Exact Rust HTTP routes | ✅ Closed for route shape; route-audit reports no missing Rust paths. | Keep generated route-audit in CI. |
| Build resolution | ✅ `CreateBuild` and `DryRunResolve` use resolver ports and production repository wiring when `DATABASE_URL` is set. | External JobSpec, dataset-versioning and branch-lock behavior must be configured in production. |
| Executor/runtime dispatch | ✅ DAG executor, persisted build-plan adapter, run trigger/retry/cancel and Python/job-runner dispatch are wired. | Runtime quality depends on injected node/job/Python ports. |
| Runs/build queue | ✅ `/api/v1/data-integration` run/build queue routes are mounted, repository-backed and covered by fakes for trigger/retry/list behavior. | `DATABASE_URL` must be configured. |
| Logs | ✅ JSON list, emit, SSE and ws routes are mounted. | Store/subscriber wiring is required for live history/emit/ws behavior. |
| Spark | ✅ Rust `/api/v1/pipeline/builds/*` routes are mounted, persisted and tested. | Kubernetes client and `pipeline_run_submissions` repository are required. |
| Iceberg | 🟡 Client/config remain ADR-0041 gated. | Set `FOUNDRY_ICEBERG_CATALOG_*` and wire the transaction manager for multi-table atomicity. |
| Migrations | ✅ Go migration runner applies service-local SQL including builds, job logs, schedules and Spark submissions. | Keep SQL synchronized with Rust-origin contracts. |

## Route groups now considered closed for route shape

| Group | Rust routes | Go status |
| --- | --- | --- |
| Data-integration runs | `GET/POST /api/v1/data-integration/pipelines/{id}/runs`, `GET /runs/{run_id}`, `POST /retry`, scheduler run-due and dry-run resolve. | Mounted; repository/runtime-backed with explicit configuration errors when adapters are absent. |
| Data-integration build queue | `GET /api/v1/data-integration/builds`, `GET /_summary`, `POST /{run_id}/abort`. | Mounted; repository-backed with explicit config errors when absent. |
| V1 builds/jobs | `/v1/builds`, `/v1/builds/{rid}`, `/v1/builds/{rid}:abort`, `/v1/datasets/{rid}/builds`, `/v1/jobs/*`, `/v1/job-specs/{kind}`. | Mounted; repository/log/runtime gated where data access is required. |
| SparkApplication builds | `POST /api/v1/pipeline/builds/run`, `GET /api/v1/pipeline/builds/{run_id}/status`. | Mounted; Kubernetes-dispatched and persisted through `pipeline_run_submissions`. |
| Health | `/healthz`. | Mounted. |

## Required checks

- `go test ./services/pipeline-build-service/...`
- `go run ./tools/route-audit --write docs/migration/route-parity-audit.md`

The generated audit is the source of truth for route shape. The status values in
this document intentionally distinguish route-shape closure from production
dependency gates.
