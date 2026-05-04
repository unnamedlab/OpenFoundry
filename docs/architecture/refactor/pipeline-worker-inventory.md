# `pipeline-worker` functional inventory

> **Migration context.** FASE 3 / Tarea 3.1 of
> [`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../migration-plan-foundry-pattern-orchestration.md).
> The plan retires the centralised Temporal orchestrator in favour of
> Foundry-pattern primitives: SparkApplication CRs for batch, Kafka
> consumers for fan-out, Postgres state machines for stateful flows,
> all coordinated via the transactional outbox + Debezium (ADR-0022,
> ADR-0037). This document is the read-only baseline used to plan the
> refactor of `workers-go/pipeline/` — **no code is being moved here**;
> the migration itself lands in subsequent tasks (3.2+).

---

## 1. Worker process topology

| Field | Value | Source |
|---|---|---|
| Worker binary | `workers-go/pipeline/main.go` | [main.go](../../../workers-go/pipeline/main.go) |
| Temporal task queue | `openfoundry.pipeline` (constant `contract.TaskQueue`) | [contract.go:7](../../../workers-go/pipeline/internal/contract/contract.go) |
| Registered workflows | **`PipelineRun`** (only one) | [main.go:38](../../../workers-go/pipeline/main.go) |
| Registered activities | `(*Activities).BuildPipeline`, `(*Activities).ExecutePipeline` | [activities.go:117,135](../../../workers-go/pipeline/activities/activities.go) |
| Health / metrics | `:9090/healthz`, `:9090/metrics` (placeholder text) | [main.go:54-66](../../../workers-go/pipeline/main.go) |
| Cross-language contract | Mirrors `libs/temporal-client::PipelineRunInput` / `PipelineRunResult` | [contract.go:13-26](../../../workers-go/pipeline/internal/contract/contract.go) |

Configuration env (worker → Rust services):

| Env var | Default | Purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` / `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Temporal frontend gRPC |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.pipeline` | Task queue polled |
| `OF_PIPELINE_AUTHORING_URL` | `http://pipeline-authoring-service:50080` | `BuildPipeline` HTTP base |
| `OF_PIPELINE_BUILD_URL` | `http://pipeline-build-service:50081` | `ExecutePipeline` HTTP base |
| `OF_PIPELINE_BEARER_TOKEN` | _(empty)_ | Optional service bearer token |

---

## 2. Workflow inventory

There is exactly **one workflow type** registered on this task queue.

### `PipelineRun` — orchestrates one pipeline execution end-to-end

Source: [`workflows/pipeline_run.go`](../../../workers-go/pipeline/workflows/pipeline_run.go)

```text
PipelineRun(input) → result
   │
   ├── ActivityOptions{ StartToClose=30m, Retry: init=1m, max=15m, attempts=3 }
   │
   ├── Step 1: ExecuteActivity(BuildPipeline, BuildInput{
   │       pipeline_id, tenant_id, revision, parameters, audit_correlation_id
   │   }) → BuildResult{ status, plan, error }
   │       └── On error: return PipelineRunResult{status=failed, error}
   │
   └── Step 2: ExecuteActivity(ExecutePipeline, ExecuteInput{
           pipeline_id, tenant_id, plan: build.Plan, audit_correlation_id
       }) → ExecuteResult{ status: completed|failed|running, run_id, run }
           └── On error: return PipelineRunResult{status=failed, error}
```

| Field | Value |
|---|---|
| Inputs (`PipelineRunInput`) | `pipeline_id`, `tenant_id`, `revision?`, `parameters` (free-form `map[string]any`) |
| Outputs (`PipelineRunResult`) | `pipeline_id`, `status` (`completed`/`failed`/`running`), `error?` |
| Search attributes | `audit_correlation_id` is propagated via the `x-audit-correlation-id` HTTP header to both activities (workflow execution ID acts as the correlation ID). |
| Signals | **None.** No `workflow.GetSignalChannel` calls anywhere in `workflows/`. |
| Queries | **None.** No `workflow.SetQueryHandler` calls. |
| Child workflows | **None.** `PipelineRun` is a flat 2-step DAG of activities. |
| Compensation | **None.** Failure is reported via the result, no rollback step. |
| Determinism hazards | None observed (no `time.Now`, `rand`, native goroutines in workflow code). |

This minimal design (no signals, no queries, no children, no timers) is
the single most important fact of this inventory: the workflow does
nothing Temporal-specific that a Spark Operator + Postgres state
machine can't replicate (see §6).

---

## 3. Activity inventory

Both activities are thin HTTP/JSON clients (REST, not gRPC, per
ADR-0021 §Wire format and migration-plan task S2.6).

### 3.1 `BuildPipeline` → `pipeline-authoring-service`

Source: [`activities/activities.go:117-177`](../../../workers-go/pipeline/activities/activities.go)

| Aspect | Detail |
|---|---|
| Target service | `pipeline-authoring-service` (`OF_PIPELINE_AUTHORING_URL`, default `:50080`) |
| Endpoints called | 1) `GET  /api/v1/data-integration/pipelines/{pipeline_id}` (only when `Parameters` does **not** already contain a compile request body, i.e. no `nodes`/`pipeline` key)<br>2) `POST /api/v1/data-integration/pipelines/_compile` |
| Rust handler | `handlers::compiler::compile_pipeline` (route registered in [`pipeline-authoring-service/src/main.rs:170`](../../../services/pipeline-authoring-service/src/main.rs)) |
| Headers | `authorization: Bearer …` (if token configured), `x-audit-correlation-id: <workflow-execution-id>`, `content-type: application/json` |
| Input | `BuildInput{ pipeline_id, tenant_id, revision?, parameters, audit_correlation_id }` |
| Compile request body | `{ status, nodes (=DAG), schedule_config, retry_policy, start_from_node, distributed_worker_count }` — built either from `Parameters` directly or from the persisted pipeline's `dag` field |
| Output | `BuildResult{ status="compiled", plan: response["plan"] (with pipeline_id, tenant_id, revision injected), error? }` |
| Failure modes | `nonRetryable("invalid_pipeline_build_input" / "pipeline_build_config" / "pipeline_compile_response" / "pipeline_client_error")` for 4xx (except 429); transient retry for 5xx and 429 |
| Side effects | None. Compile is pure (no DB writes). |

### 3.2 `ExecutePipeline` → `pipeline-build-service`

Source: [`activities/activities.go:135-217`](../../../workers-go/pipeline/activities/activities.go)

| Aspect | Detail |
|---|---|
| Target service | `pipeline-build-service` (`OF_PIPELINE_BUILD_URL`, default `:50081`) |
| Endpoint called | `POST /api/v1/data-integration/pipelines/{pipeline_id}/runs` |
| Rust handler | `handlers::execute::trigger_run` (route registered in [`pipeline-build-service/src/main.rs:71-74`](../../../services/pipeline-build-service/src/main.rs)) → `domain::executor::start_pipeline_run` ([`executor.rs:17`](../../../services/pipeline-build-service/src/domain/executor.rs)) |
| Headers | Same as 3.1 |
| Input | `ExecuteInput{ pipeline_id, tenant_id, plan, audit_correlation_id }` |
| Request body | `{ context: { trigger:{type:"temporal"}, tenant_id, compiled_plan, audit_correlation_id, temporal_task_queue, temporal_workflow_ref }, skip_unchanged: true, from_node_id? (=plan.start_from_node) }` |
| Output | `ExecuteResult{ status: normalised(completed/failed/cancelled/aborted/running), run_id, run: full response, error? }` |
| Failure modes | Same retry classification as 3.1 |
| Side effects | The Rust handler (a) inserts a `pipeline_runs` row, (b) drives `domain::engine::execute_pipeline` synchronously per-node, (c) updates `pipeline_runs` with the final status. **The activity blocks on the synchronous run** — there is no polling loop in the worker. |

---

## 4. Per-node transform dispatch (the actual work)

`ExecutePipeline` does not run any transform itself; the work lives in
`pipeline-build-service::domain::engine`
([`engine/mod.rs:159-280`](../../../services/pipeline-build-service/src/domain/engine/mod.rs)).
For each node in the compiled plan, the engine dispatches by
`transform_type`:

| `transform_type` | Implementation | Notes for the Spark migration |
|---|---|---|
| `sql` | `runtime::execute_sql_transform` (in-process via DataFusion / Iceberg path) | Stays in Rust. Will be wrapped behind a manifest contract once Spark owns large jobs. |
| `python` | `runtime::execute_python_transform` (sync HTTP to compute-modules-runtime) | Already remote. Candidate to share the Spark submission path for parity. |
| `llm` | `runtime::execute_llm_transform` (HTTP `ai-service`) | Stays as-is. |
| `wasm` | `runtime::execute_wasm_transform` (in-process WASI) | Stays in Rust. |
| `passthrough` | `runtime::execute_passthrough_transform` (copy input dataset version) | Stays in Rust. |
| **`spark` / `pyspark`** | `runtime::execute_distributed_compute_transform` ([`runtime.rs:682-701`](../../../services/pipeline-build-service/src/domain/engine/runtime.rs)) → `execute_remote_compute_job` with `default_job_type="spark-batch"`, `execution_mode="async"`, `input_mode="dataset_manifest"`, `output_delivery="direct_upload"`. Uses the `openfoundry/distributed-compute.v1` contract. | **This is exactly the slice that should become a SparkApplication CR**: today the engine POSTs to a remote endpoint and polls for completion ([`runtime.rs:1408-1417`](../../../services/pipeline-build-service/src/domain/engine/runtime.rs)). The work moves to Spark Operator and the engine becomes a CR creator + watcher. |
| `external` / `remote` | `runtime::execute_remote_compute_transform` (sync `inline_rows` / `pipeline_upload`) | Stays as remote HTTP. |

So a single pipeline run is already a heterogeneous DAG where **only
some nodes are Spark batch**. That observation drives the migration
strategy in §6.

---

## 5. How `PipelineRun` is triggered

The worker is invoked by exactly two upstreams.

### 5.1 Ad-hoc, user-triggered

Path: `pipeline-authoring-service` (or any client) → Temporal client
→ start workflow on task queue `openfoundry.pipeline` →
`PipelineRun(pipeline_id=…, parameters={…})`.

### 5.2 Scheduled / event-triggered (`pipeline-schedule-service`)

Source: [`services/pipeline-schedule-service/`](../../../services/pipeline-schedule-service/), notably:

- [`main.rs:168-171`](../../../services/pipeline-schedule-service/src/main.rs) — instantiates `temporal_client::PipelineScheduleClient`.
- [`domain/temporal_schedule.rs`](../../../services/pipeline-schedule-service/src/domain/temporal_schedule.rs) — converts a Foundry schedule definition to a Temporal Schedule that targets the `PipelineRun` workflow type with a `PipelineRunInput`.
- HTTP surface ([`main.rs:213-303`](../../../services/pipeline-schedule-service/src/main.rs)) — `POST /v1/schedules`, `:run-now`, `:pause`, `:resume`, plus the legacy `/schedules/temporal` and the `/workflows/events/{event_name}` event-trigger fan-out.

Important: `pipeline-schedule-service` already owns Foundry-parity
schedule semantics (cron, time triggers, AIP, troubleshooting, sweep
linter). Temporal Schedules are only the dispatch substrate. The
migration therefore changes the dispatch substrate (Temporal Schedule
→ k8s `CronJob` / Kubernetes Operator that creates SparkApplication
CRs); the user-facing schedule API stays put.

---

## 6. Migration table — Temporal pieces → Foundry-pattern targets

Aligned with FASE 3 of the migration plan and ADR-0037. Each row is a
discrete piece of behaviour observable in `workers-go/pipeline/`
today; the right-hand column is the post-migration owner.

| # | Today (Temporal) | Tomorrow (Foundry pattern) | Work-type bucket |
|---|---|---|---|
| 1 | `PipelineRun` workflow registered on `openfoundry.pipeline` | **Removed.** No central workflow object — orchestration is the SparkApplication graph + state machine in `pipeline_runs`. | n/a |
| 2 | `BuildPipeline` activity → `POST /pipelines/_compile` | **Inline call** from `pipeline-build-service` itself when it accepts the run request (compile is pure, no need for a separate worker hop). | DAG build |
| 3 | `ExecutePipeline` activity → `POST /pipelines/{id}/runs` | **`pipeline-build-service` HTTP handler** (already exists) creates a SparkApplication CR per Spark/PySpark node, plus drives non-Spark nodes inline (Rust engine path stays). | Spark batch puro **→ SparkApplication CR** |
| 4 | DAG between activities (Step 1 → Step 2) inside the workflow | **DAG between SparkApplication CRs** via Spark Operator `dependsOn` (when present) and the `pipeline_runs` Postgres state machine for non-Spark nodes. | Coordinación entre Spark jobs **→ DAG via SparkApplication dependsOn** |
| 5 | Retries (`InitialInterval=1m`, backoff 2.0, `MaximumAttempts=3`) | SparkApplication CR `restartPolicy.type=OnFailure` with `onFailureRetries=3`, `onFailureRetryInterval=60`, `onSubmissionFailureRetries=3`. | Same semantics, different CR. |
| 6 | `StartToCloseTimeout=30m` | SparkApplication `timeToLiveSeconds` + per-job `spec.executor.deleteOnTermination`; Postgres state machine enforces a hard wall-clock kill. | Same semantics. |
| 7 | User-triggered run (`POST /pipelines/{id}/runs` from UI) | **`pipeline-build-service`** keeps the same HTTP API, but instead of synchronously running the engine it (a) inserts the `pipeline_runs` row, (b) creates a SparkApplication CR per Spark node and dispatches non-Spark nodes inline, (c) returns `202 Accepted` with the run id. | User-triggered **→ API en pipeline-build-service que crea SparkApp CR** |
| 8 | Scheduled run (`pipeline-schedule-service` → Temporal Schedule → `PipelineRun`) | `pipeline-schedule-service` reconciles its schedule rows into **k8s `CronJob` resources** (or, optionally, `ScheduledSparkApplication` if the pipeline is single-node Spark) that hit the same `pipeline-build-service` HTTP API. | Scheduled **→ CronJob k8s que crea SparkApp CR** |
| 9 | Event-triggered run (`POST /workflows/events/{event_name}` → emits `WorkflowRunRequested` → currently lands on Temporal) | Replace the dispatch with an outbox event (`pipeline.run.requested.v1`); a Kafka consumer in `pipeline-build-service` creates the SparkApplication CR. (ADR-0022, ADR-0038.) | Event-triggered **→ Kafka consumer** |
| 10 | `audit_correlation_id` propagation via `x-audit-correlation-id` header | Same header continues to flow; CR labels carry it (`of/audit-correlation-id`) so logs and lineage events stitch back. | Cross-cutting. |
| 11 | `parameters` map → compile + execute payload | Identical wire shape preserved on the new HTTP handler. | n/a |
| 12 | Per-node dispatch in `pipeline-build-service::domain::engine` (sql/python/llm/wasm/passthrough/spark/external) | **Unchanged for non-Spark transform types.** For `spark`/`pyspark`, the in-process `execute_distributed_compute_transform` POST → poll loop is replaced by `apply SparkApplication CR` → watch `.status.applicationState`. | The Spark slice is the only one that becomes a CR. |

---

## 7. Temporal-specific features that don't map to Spark

The good news: **`PipelineRun` uses none of the Temporal features that
have no Spark equivalent.**

Concretely, an inventory of features the workflow does *not* use:

- **No `workflow.GetSignalChannel`** — no live human/UI input mid-run.
- **No `workflow.SetQueryHandler`** — no synchronous state read API.
- **No timers / `workflow.Sleep`** — only activity timeouts.
- **No `ContinueAsNew`** — runs are bounded (one build + one execute).
- **No child workflows** — flat activity DAG.
- **No `Selector` / racing futures** — sequential `.Get()` calls.
- **No durable signals across versions / patching** — `workflow.GetVersion` not used.

The migration therefore does **not** need a "Workshop UI for live
queries" workaround mentioned as a possible failure mode in the task
brief. If, in a future iteration, the team wants a live progress feed
(e.g. per-node status while a run is in flight), the existing
[`/jobs/{rid}/logs/stream`](../../../services/pipeline-build-service/src/main.rs)
SSE / WebSocket surface already covers that — it doesn't need to be
implemented from scratch.

The single piece of wire-format leakage to clean up is the
`temporal_task_queue` / `temporal_workflow_ref` fields the activity
stuffs into the run `context`
([`activities.go:194-195`](../../../workers-go/pipeline/activities/activities.go))
— these become `runtime` / `correlation_id` once Temporal is gone.

---

## 8. Net call graph (today)

```text
                 ┌──────────────────────────┐
                 │ pipeline-authoring-service│ user "run"
                 │  /v1/.../{id}:run        │ ─────────────┐
                 └──────────────────────────┘              │
                                                           ▼
                 ┌──────────────────────────┐    Temporal client.start
                 │ pipeline-schedule-service│ ───────────────────────┐
                 │  Temporal Schedules      │                        │
                 └──────────────────────────┘                        │
                                                                     ▼
                                                    ┌────────────────────────────┐
                                                    │  Temporal frontend (gRPC)  │
                                                    │  ns=default, tq=of.pipeline│
                                                    └─────────────┬──────────────┘
                                                                  │ poll
                                                                  ▼
                                  ┌──────────────────────────────────────┐
                                  │  workers-go/pipeline (this worker)   │
                                  │  PipelineRun workflow                │
                                  │   ├── BuildPipeline activity         │
                                  │   └── ExecutePipeline activity       │
                                  └──┬─────────────────┬─────────────────┘
                       HTTP /compile │                 │ HTTP /pipelines/{id}/runs
                                     ▼                 ▼
                ┌──────────────────────────┐  ┌──────────────────────────┐
                │ pipeline-authoring-svc   │  │ pipeline-build-service   │
                │ /pipelines/_compile      │  │ /pipelines/{id}/runs     │
                └──────────────────────────┘  └────────────┬─────────────┘
                                                           │ engine::execute_pipeline
                                                           ▼
                                                ┌──────────────────────┐
                                                │ per-node dispatch:   │
                                                │  sql/python/llm/wasm │
                                                │  passthrough         │
                                                │  spark/pyspark ─────►│ remote compute
                                                │  external/remote    ─│► remote compute
                                                └──────────────────────┘
```

## 9. Net call graph (post-migration target, for orientation only)

```text
   user "run"        schedule fires                event fires
        │                  │                            │
        ▼                  ▼                            ▼
   ┌───────────────────────────────────────────────────────────────┐
   │           pipeline-build-service  (HTTP API + consumer)       │
   │  POST /pipelines/{id}/runs                                    │
   │  Kafka consumer: pipeline.run.requested.v1                    │
   └────────────────────────────┬──────────────────────────────────┘
                                │ (1) compile inline
                                │ (2) INSERT pipeline_runs (state machine)
                                │ (3) for each node:
                                │       sql/python/llm/wasm/passthrough/external → engine inline
                                │       spark/pyspark                         → kubectl apply SparkApplication CR
                                ▼
                ┌─────────────────────────────────────┐
                │ Spark Operator (sparkoperator.k8s.io)│
                │   SparkApplication / ScheduledSpark │
                └─────────────────────────────────────┘
                                │ status updates
                                ▼
                ┌─────────────────────────────────────┐
                │ pipeline-build-service watch loop   │
                │ updates pipeline_runs row status    │
                └─────────────────────────────────────┘
```

---

## 10. Verification checklist for the next tasks (3.2+)

When refactor work begins, the following items from this inventory
must remain stable contracts (callers depend on them):

- [ ] `PipelineRunInput` field set (`pipeline_id`, `tenant_id`, `revision`, `parameters`) is preserved on the new HTTP / Kafka contract.
- [ ] `PipelineRunResult.status` enum (`completed` / `failed` / `running`) keeps the same string values — `pipeline-schedule-service` and the UI both pattern-match on these.
- [ ] `x-audit-correlation-id` header is still propagated end-to-end; CR labels and Kafka headers carry the same value.
- [ ] The compile request body keys (`status`, `nodes`, `schedule_config`, `retry_policy`, `start_from_node`, `distributed_worker_count`) stay as-is — `pipeline-authoring-service::handlers::compiler::compile_pipeline` consumes them by name.
- [ ] The execute request body shape (`{ context, skip_unchanged, from_node_id? }`) stays as-is — `pipeline-build-service::handlers::execute::trigger_run` already accepts it; only the worker-side caller changes.
- [ ] The non-Spark `transform_type` branches in `domain::engine::mod.rs` remain in place — the migration strictly narrows what runs as a SparkApplication CR; everything else continues to run inline in `pipeline-build-service`.
