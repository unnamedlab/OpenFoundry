# pipeline-build-service Rust → Go 1:1 parity plan

Date: 2026-05-07  
Scope: `services/pipeline-build-service` Rust crate vs. `openfoundry-go/services/pipeline-build-service` Go service.

This is the actionable parity inventory for continuing the Rust → Go migration without re-auditing from zero. It was built from:

- Manual route verification against `services/pipeline-build-service/src/main.rs`.
- Manual scan of Rust `src/handlers/*.rs`, `src/domain/**/*.rs`, `src/models/*.rs`, migrations, and integration tests.
- Manual scan of Go `openfoundry-go/services/pipeline-build-service/**/*`.
- Regenerated `openfoundry-go/HTTP-ROUTE-AUDIT.md` with `python3 tools/http_route_audit.py --write openfoundry-go/HTTP-ROUTE-AUDIT.md`.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `implemented+tested` | Go has code and a focused Go test for equivalent behavior. |
| `config-gated+tested` | Go has code and tests, but production behavior requires injected ports/clients/adapters at runtime. This is not full production parity until adapters are wired. |
| `empty-envelope` | Route exists but returns an empty list/envelope or `404 nil`; not Rust behavior. |
| `501` | Route exists and explicitly returns not implemented. |
| `missing` | Exact Rust route or feature is absent from Go. |
| `compat-only` | Go route has no exact Rust route; do not count as Rust parity unless it is explicitly mapped below. |
| `unverified` | Code may exist, but no equivalent test or adapter was found; do not mark done. |

## Executive summary

| Area | Current parity | Blocking gap |
| --- | --- | --- |
| Exact Rust HTTP routes | Mostly missing exact paths: Rust has `/api/v1/data-integration/*`, `/v1/*`, `/api/v1/pipeline/*`; Go currently exposes mostly `/api/v1/*` compatibility routes. | Mount exact Rust paths and keep compatibility aliases only after tests prove both shapes. |
| Build resolution | `config-gated+tested`: resolver domain and `CreateBuild`/`DryRunResolve` code exist with fakes. | Production DB / dataset-versioning / job-spec / lock adapters and exact Rust route mounts. |
| Executor | `config-gated+tested`: DAG executor and HTTP execution path exist with injectable ports. | Persisted build-plan adapter, real job runners, run retry/cancel, Rust failure/staleness semantics. |
| Logs | `config-gated+tested` for SSE stream; list/emit/ws exact Rust routes missing. | Postgres log sink + broadcaster wiring, `POST /v1/jobs/{rid}/logs`, WebSocket parity. |
| Spark | `config-gated+tested` on Go-only `/api/v1/data-integration/spark-runs`; exact Rust `/api/v1/pipeline/builds/*` routes missing. | Mount exact routes and map request/response contracts to Rust names. |
| Iceberg | client tests exist; boot/runtime config not wired into service main/server. | Production `FOUNDRY_ICEBERG_CATALOG_*` config + transaction manager adapter. |
| Migrations | No Go-owned migration runner or copied SQL migrations in the service. | Decide: reuse Rust SQL migrations from Go deployment, copy migrations, or centralize DB schema ownership. |

## HTTP routes: Rust → Go

> Important: the generated audit is regex-based. The table below preserves audit state but overrides interpretation where code is only config-gated. A route is not “done” unless code and tests exist for the same behavior.

| Rust route / Go route | Method | Rust handler | Go handler | Parity status | Next action |
| --- | --- | --- | --- | --- | --- |
| `/api/v1/data-integration/pipelines/{id}/runs` | GET | `handlers::runs::list_runs` | — | missing | Mount exact Rust route; back with run repository instead of empty Go compatibility route. |
| `/api/v1/data-integration/pipelines/{id}/runs` | POST | `handlers::execute::trigger_run` | — | missing | Alias to tested `TriggerPipelineRun` only after request/response shape matches Rust. |
| `/api/v1/data-integration/pipelines/{id}/runs/{run_id}` | GET | `handlers::runs::get_run` | — | missing | Implement run lookup adapter and exact route. |
| `/api/v1/data-integration/pipelines/{id}/runs/{run_id}/retry` | POST | `handlers::execute::retry_run` | — | missing | Implement retry semantics before mounting; Go compatibility retry is still `501`. |
| `/api/v1/data-integration/pipelines/_scheduler/run-due` | POST | `handlers::execute::run_due_scheduled_pipelines` | — | missing | Requires schedule DB/adapters first. |
| `/api/v1/data-integration/pipelines/{pipeline_rid}/dry-run-resolve` | POST | `handlers::dry_run::dry_run_resolve` | — | missing | Alias tested `DryRunResolve` after exact body/path compatibility test. |
| `/api/v1/data-integration/builds` | GET | `handlers::builds::list_builds` | — | missing | Requires build list query adapter and pagination semantics. |
| `/api/v1/data-integration/builds/_summary` | GET | `handlers::builds::queue_summary` | — | missing | Requires build queue metrics/query adapter. |
| `/api/v1/data-integration/builds/{run_id}/abort` | POST | `handlers::builds::abort_build` | — | missing | Alias tested abort flow after run-id vs build-id contract is verified. |
| `/v1/builds` | POST | `handlers::builds_v1::create_build` | — | missing | Mount exact `/v1` route; can call `CreateBuild` once ports/adapters exist and response contract is tested. |
| `/v1/builds` | GET | `handlers::builds_v1::list_builds_v1` | — | missing | Implement list builds v1; current Go `/api/v1/builds` is empty-envelope. |
| `/v1/builds/{rid}` | GET | `handlers::builds_v1::get_build` | — | missing | Implement RID lookup and ETag/pagination contract tests. |
| `/v1/builds/{rid}:abort` | POST | `handlers::builds_v1::abort_build_v1` | — | missing | Mount exact colon route; adapt tested `AbortBuild` only after v1 state contract matches. |
| `/v1/datasets/{rid}/builds` | GET | `handlers::builds_v1::list_dataset_builds` | — | missing | Requires build query adapter filtered by output dataset. |
| `/v1/jobs/{rid}/outputs` | GET | `handlers::builds_v1::get_job_outputs` | — | missing | Requires output transaction query adapter. |
| `/v1/jobs/{rid}/input-resolutions` | GET | `handlers::builds_v1::get_job_input_resolutions` | — | missing | Requires persisted input resolution adapter. |
| `/v1/job-specs/{kind}` | POST | `handlers::builds_v1::create_job_spec` | — | missing | Requires job-spec persistence adapter and idempotency test. |
| `/v1/jobs/{rid}/logs` | GET | `handlers::job_logs::list_logs` | — | missing | Mount exact route; current Go `/api/v1/jobs/{id}/logs` is empty-envelope. |
| `/v1/jobs/{rid}/logs` | POST | `handlers::job_logs::emit_log` | — | missing | Implement log emission and broadcast path. |
| `/v1/jobs/{rid}/logs/stream` | GET | `handlers::job_logs::stream_logs` | — | missing | Alias tested SSE stream after exact path and Last-Event-ID behavior tests. |
| `/v1/jobs/{rid}/logs/ws` | GET | `handlers::job_logs::ws_logs` | — | missing | WebSocket route not present in Go. |
| `/api/v1/pipeline/builds/run` | POST | `handlers::spark_runs::submit_pipeline_run` | — | missing | Mount exact Spark route; Go equivalent currently lives at `/api/v1/data-integration/spark-runs`. |
| `/api/v1/pipeline/builds/{run_id}/status` | GET | `handlers::spark_runs::get_pipeline_run_status` | — | missing | Mount exact Spark status route and map `{run_id}` to SparkApplication name/status contract. |
| `/healthz` | GET | inline Rust closure | Go health handler | unverified (code exists, no focused route test found) | Add explicit route test if health contract matters for rollout. |
| `/api/v1/builds` | POST | — | `handler.CreateBuild` | compat-only / config-gated+tested | Keep as alias; not Rust exact route. Needs production adapters. |
| `/api/v1/dry-run/resolve` | POST | — | `handler.DryRunResolve` | compat-only / config-gated+tested | Keep as alias; add exact Rust route. |
| `/api/v1/execute` | POST | — | `handler.ExecutePipeline` | compat-only / config-gated+tested | Internal compatibility route; not in Rust route table. |
| `/api/v1/pipelines/{id}/runs` | POST | — | `handler.TriggerPipelineRun` | compat-only / config-gated+tested | Candidate implementation for Rust trigger route after contract test. |
| `/api/v1/builds` | GET | — | `handler.ListBuilds` | empty-envelope | Replace with query adapter or remove from parity claims. |
| `/api/v1/builds/{id}` | GET | — | `handler.GetBuild` | empty-envelope-ish (`404 nil`) | Implement lookup or stop claiming implemented. |
| `/api/v1/builds/{id}/abort` | POST | — | `handler.AbortBuild` | compat-only / config-gated+tested | Candidate for Rust abort routes; requires exact path tests and adapters. |
| `/api/v1/builds/{id}/jobs` | GET | — | `handler.ListJobs` | empty-envelope | Requires jobs query adapter. |
| `/api/v1/jobs/{id}` | GET | — | `handler.GetJob` | empty-envelope-ish (`404 nil`) | Requires job lookup adapter. |
| `/api/v1/jobs/{id}/logs` | GET | — | `handler.ListJobLogs` | empty-envelope | Replace with log store History adapter. |
| `/api/v1/jobs/{id}/logs/stream` | GET | — | `handler.StreamJobLogs` | compat-only / config-gated+tested | Candidate for Rust SSE route; exact `/v1` path missing. |
| `/api/v1/dry-run/validate` | POST | — | `handler.DryRunValidate` | 501 | Implement or remove from parity scope if not Rust route. |
| `/api/v1/pipelines` | GET | — | `handler.ListPipelines` | empty-envelope | Legacy Go-only route; not Rust parity. |
| `/api/v1/pipelines` | POST | — | `handler.CreatePipeline` | 501 | Legacy Go-only route; not Rust parity. |
| `/api/v1/pipelines/{id}` | GET | — | `handler.GetPipeline` | empty-envelope-ish (`404 nil`) | Legacy Go-only route; not Rust parity. |
| `/api/v1/pipelines/{id}` | PATCH/PUT | — | `handler.UpdatePipeline` | 501 | Legacy Go-only route; not Rust parity. |
| `/api/v1/pipelines/{id}` | DELETE | — | `handler.DeletePipeline` | empty success (`204`) | Legacy Go-only route; not Rust parity. |
| `/api/v1/pipelines/{id}/runs` | GET | — | `handler.ListPipelineRuns` | empty-envelope | Candidate for Rust list route only after exact mount/query adapter. |
| `/api/v1/pipelines/{id}/runs/{run_id}` | GET | — | `handler.GetPipelineRun` | empty-envelope-ish (`404 nil`) | Candidate for Rust get route only after adapter. |
| `/api/v1/pipelines/{id}/runs/{run_id}/retry` | POST | — | `handler.RetryPipelineRun` | 501 | Must port Rust retry. |
| `/api/v1/pipelines/{id}/runs/{run_id}/cancel` | POST | — | `handler.CancelPipelineRun` | 501 | No Rust exact route in `main.rs`, but needed for Go legacy completeness. |
| `/api/v1/data-integration/spark-runs` | GET | — | `handler.ListSparkRuns` | empty-envelope | Go-only route; not Rust parity. |
| `/api/v1/data-integration/spark-runs` | POST | — | `handler.SubmitSparkRun` | compat-only / config-gated+tested | Candidate for Rust Spark submit route after exact mount. |
| `/api/v1/data-integration/spark-runs/{id}` | GET | — | `handler.GetSparkRun` | compat-only / config-gated+tested | Candidate for Rust Spark status route after exact mount. |
| `/api/v1/data-integration/pipelines/{id}/runs/{run_id}/spec` | GET | — | `handler.GetSpecForRun` | empty-envelope-ish (`404 nil`) | Go-only helper; not Rust parity. |
| `/health`, `/metrics` | GET | — | Go health/metrics | compat-only | Operational aliases only. |

## Rust handlers → Go handlers

| Rust handler | Rust route(s) | Go equivalent | Status | Required next step |
| --- | --- | --- | --- | --- |
| `handlers::runs::list_runs` | `GET /api/v1/data-integration/pipelines/{id}/runs` | `ListPipelineRuns` only on `/api/v1/pipelines/{id}/runs` | empty-envelope + path missing | Implement repository-backed list and mount exact Rust path. |
| `handlers::runs::get_run` | `GET /api/v1/data-integration/pipelines/{id}/runs/{run_id}` | `GetPipelineRun` only on `/api/v1/pipelines/{id}/runs/{run_id}` | empty-envelope + path missing | Implement lookup/status envelope. |
| `handlers::execute::trigger_run` | `POST /api/v1/data-integration/pipelines/{id}/runs` | `TriggerPipelineRun` | config-gated+tested but path missing | Add exact route and request/response parity test; wire production `PipelineRunRepository`. |
| `handlers::execute::retry_run` | `POST /api/v1/data-integration/pipelines/{id}/runs/{run_id}/retry` | `RetryPipelineRun` | 501 + path missing | Port retry source-run lookup, reuse execution ports, test retry envelope. |
| `handlers::execute::run_due_scheduled_pipelines` | `POST /api/v1/data-integration/pipelines/_scheduler/run-due` | none | missing | Port schedule query/dispatch after migration/adapters. |
| `handlers::dry_run::dry_run_resolve` | `POST /api/v1/data-integration/pipelines/{pipeline_rid}/dry-run-resolve` | `DryRunResolve` | config-gated+tested but path missing | Mount exact route and add path-param test. |
| `handlers::builds::list_builds` | `GET /api/v1/data-integration/builds` | `ListBuilds` | empty-envelope + path missing | Implement build queue/list query. |
| `handlers::builds::queue_summary` | `GET /api/v1/data-integration/builds/_summary` | none | missing | Port queue summary SQL/metrics. |
| `handlers::builds::abort_build` | `POST /api/v1/data-integration/builds/{run_id}/abort` | `AbortBuild` | config-gated+tested but path/id contract missing | Mount exact route and verify run-id/build-id behavior. |
| `handlers::builds_v1::create_build` | `POST /v1/builds` | `CreateBuild` | config-gated+tested but path missing | Mount exact `/v1/builds`; production adapters first. |
| `handlers::builds_v1::list_builds_v1` | `GET /v1/builds` | `ListBuilds` | empty-envelope + path missing | Implement v1 pagination/filter semantics. |
| `handlers::builds_v1::get_build` | `GET /v1/builds/{rid}` | `GetBuild` | empty-envelope-ish + path missing | Implement RID lookup + ETag contract. |
| `handlers::builds_v1::abort_build_v1` | `POST /v1/builds/{rid}:abort` | `AbortBuild` | config-gated+tested but path missing | Add colon-route mount and contract test. |
| `handlers::builds_v1::list_dataset_builds` | `GET /v1/datasets/{rid}/builds` | none | missing | Implement dataset-filtered builds query. |
| `handlers::builds_v1::get_job_outputs` | `GET /v1/jobs/{rid}/outputs` | none | missing | Implement outputs query from multi-output tables. |
| `handlers::builds_v1::get_job_input_resolutions` | `GET /v1/jobs/{rid}/input-resolutions` | none | missing | Implement persisted input-resolution query. |
| `handlers::builds_v1::create_job_spec` | `POST /v1/job-specs/{kind}` | none | missing | Port job-spec publish/idempotency handler and DB adapter. |
| `handlers::job_logs::list_logs` | `GET /v1/jobs/{rid}/logs` | `ListJobLogs` | empty-envelope + path missing | Wire `logs.Service.Store.History` for JSON list route. |
| `handlers::job_logs::emit_log` | `POST /v1/jobs/{rid}/logs` | none | missing | Implement persistence + broadcast + metrics. |
| `handlers::job_logs::stream_logs` | `GET /v1/jobs/{rid}/logs/stream` | `StreamJobLogs` | config-gated+tested but path missing | Mount exact route; verify resume query and headers against Rust. |
| `handlers::job_logs::ws_logs` | `GET /v1/jobs/{rid}/logs/ws` | none | missing | Port websocket broadcaster or intentionally de-scope with client migration. |
| `handlers::spark_runs::submit_pipeline_run` | `POST /api/v1/pipeline/builds/run` | `SubmitSparkRun` | config-gated+tested but path missing | Mount exact path and translate Rust request fields. |
| `handlers::spark_runs::get_pipeline_run_status` | `GET /api/v1/pipeline/builds/{run_id}/status` | `GetSparkRun` | config-gated+tested but path missing | Mount exact path and map `run_id` to SparkApplication name. |

## Rust domain modules → Go domain modules

| Rust module(s) | Go module(s) | Status | Evidence / gap |
| --- | --- | --- | --- |
| `models/{build,job,pipeline,run}.rs` | `internal/models/*.go` | implemented+tested | JSON contract fixture exists; still add route-level envelope tests before marking HTTP done. |
| `job_lifecycle.rs` | `internal/domain/joblifecycle` | implemented+tested | State machine tests cover happy/abort/invalid/terminal transitions. |
| `branch_resolution.rs`, `build_resolution.rs`, `job_graph.rs` | `internal/domain/resolver`, `internal/handler/build_resolution.go` | config-gated+tested | Resolver logic covered with fakes; production SQL/HTTP adapters missing. |
| `build_executor.rs`, `executor.rs` | `internal/domain/executor`, `internal/handler/execution.go` | config-gated+tested | DAG executor and HTTP execute tests exist; persisted plan adapter and real runners missing. |
| `engine/{dag_executor,node_runner,runtime,python_rt}.rs` | `internal/domain/engine`, `internal/runtime/python.go` | partial+tested | DAG/fingerprint and Python sidecar tests exist; DataFusion/Polars/SQL/WASM runtimes are stubs/missing. |
| `engine/{datafusion_rt,polars_rt,sql_rt,wasm_sandbox}.rs` | none or runtime stub errors | missing/unverified | Must port or explicitly remove from supported transform matrix with tests. |
| `runners/{analytical,export,health_check,sync,transform,view_filter}.rs` | `internal/domain/runners/runners.go` | partial+tested | Dispatcher/orchestrator tests exist; external analytical/export/sync service effects are not production adapters. |
| `staleness.rs` | none | missing | Force-build/staleness tests from Rust have no Go equivalent. |
| `run_guarantees.rs` | partial in resolver/executor invariants | unverified | No direct Go test for branch/transaction guarantee helpers. |
| `marking_propagation.rs` | `internal/domain/markings` | partial/unverified | SQL helper exists; no Go test file found for markings. Do not mark done. |
| `lineage/{graph,tracker,column_level}.rs`, `lineage_events.rs` | `internal/domain/lineage` | partial+tested for graph/filtering only | Graph/marking tests exist; event enqueue/tracker/column-level persistence not ported. |
| `logs/{broadcast,composite,postgres_sink}.rs` | `internal/logs`, handler SSE glue | config-gated+tested for SSE store/subscriber contract | Postgres sink, emit endpoint, WS endpoint, metrics integration missing. |
| `iceberg_output_client.rs` | `internal/iceberg/client.go` | partial+tested | Client and rollback tests exist; service config/main injection and transaction manager adapter missing. |
| `build_events.rs` | none | missing | Outbox/build event enqueue not ported. |
| `metrics.rs` | observability metrics + scattered handler behavior | unverified | No 1:1 metric names/counters audit or tests. |
| `pipeline_schedule/*` (referenced by tests/migrations even outside `src/domain`) | `internal/domain/schedule` | partial+tested | Trigger JSON, cron window and auto-pause helpers exist; DB scheduler dispatch and versioning endpoints missing. |

## Rust migrations → Go migrations/adapters

No SQL migration files exist under `openfoundry-go/services/pipeline-build-service`. Current Go code uses interfaces/fakes and a few SQL snippets, so production parity requires an explicit schema ownership decision before handler work.

| Rust migration | Main schema/function | Go migration/adapters | Status | Dependency |
| --- | --- | --- | --- | --- |
| `20260419100005_initial_pipelines.sql` | legacy pipelines/runs foundation | no Go migration; run repository interface only | missing | Required for list/get/trigger legacy run routes. |
| `20260421170000_pipeline_enhancements.sql` | pipeline metadata/enhancements | no Go migration/adapters | missing | Required for CRUD/list compatibility if retained. |
| `20260425190000_generalized_lineage.sql` | lineage graph tables | partial in-memory/domain lineage only | partial | Needed before lineage event/tracker parity. |
| `20260427070600_16_compute_modules_foundation.sql` | compute module foundation | no Go adapter | missing | Needed for authoring/compute module integration if in scope. |
| `20260427070600_17_compute_module_runs_foundation.sql` | compute module run records | no Go adapter | missing | Needed for compute run persistence. |
| `20260504000020_job_specs.sql` | job_specs | `resolver.JobSpecRepository` interface/fakes only | config-gated | First production adapter to implement. |
| `20260504000050_builds_init.sql` | builds/jobs/build queue | `BuildRepository`, `BuildPlanRepository`, `AbortBuildRepository` interfaces only | config-gated | Required before `/v1/builds`, list/get/abort. |
| `20260504000051_multi_output.sql` | output transactions / multi-output atomicity | `executor.TransactionManager`, `internal/iceberg` tests only | config-gated | Required before executor production parity. |
| `20260504000060_input_view_filter.sql` | input view filters/resolutions | resolver models partially carry view data | partial | Required for `get_job_input_resolutions` and view-filter tests. |
| `20260504000070_job_logs.sql` | job log rows/sequence | `internal/logs` interfaces only | config-gated | Required before log list/emit/SSE production. |
| `20260504000080_pipeline_run_submissions.sql` | run submission records | no concrete Go adapter | missing | Required for Spark/run submission persistence. |
| `20260504000080_schedules_init.sql` | schedules base | `internal/domain/schedule` pure helpers only | partial | Required for scheduler dispatch. |
| `20260504000081_schedule_runs.sql` | schedule run outcomes | no Go adapter | missing | Required for auto-pause/outcome tests. |
| `20260504000082_schedule_scope.sql` | schedule scope | no Go adapter | missing | Required for project/user mode schedule tests. |
| `20260504000083_schedules_definitions.sql` | schedule definitions/versioning | pure trigger helpers only | partial | Required for preview/version diff endpoints. |
| `20260504000090_parameterized.sql` | parameterized runs | no Go adapter | missing | Required for parameterized run tests. |
| `20260505000010_pipeline_type_lifecycle.sql` | pipeline type/lifecycle | no Go adapter | missing | Required for lifecycle/type validation tests. |

## Rust integration tests → Go equivalent test status

| Rust integration test | Go equivalent test(s) | Status | Next action |
| --- | --- | --- | --- |
| `build_resolution_detects_cycle.rs` | `TestResolveBuildInvalidGraphDetectsCycle`, `TestCreateBuildValidation` | implemented+tested | Add exact `/v1/builds` route test after mount. |
| `build_resolution_missing_jobspec.rs` | `TestResolveBuildMissingJobSpecListsTriedBranches`, `TestCreateBuildMissingJobSpec` | implemented+tested | Add production adapter test. |
| `build_resolution_acquires_locks.rs` | `TestAcquireLocksSuccessAndFailure`, `TestCreateBuildLockHeld` | config-gated+tested | Implement real lock adapter. |
| `build_resolution_incompatible_ancestry_fails.rs` | `TestBranchAncestryCompatibility`, `TestCreateBuildInputBranchMissing` | partial+tested | Add exact error envelope parity test. |
| `compile_build_graph_falls_back_to_master.rs` | resolver fallback tests | partial+tested | Add named Go test for master fallback. |
| `build_does_not_create_branch_on_input_dataset.rs` | none direct | missing | Port branch side-effect guard test. |
| `build_does_not_modify_other_branches.rs` | none direct | missing | Port branch isolation test. |
| `build_queued_when_upstream_in_progress.rs` | none direct | missing | Port upstream-in-progress query and queued reason. |
| `build_locks_released_on_abort_or_finish.rs` | abort tests only | partial | Add lock release adapter/executor test. |
| `coalesce_re_run_when_previous_active.rs` | none | missing | Port active-run coalescing semantics. |
| `force_build_overrides_staleness.rs` | none | missing | Port staleness module first. |
| `staleness_skips_when_inputs_unchanged.rs` | none | missing | Port staleness signatures and last completed job adapter. |
| `input_view_filter_at_timestamp.rs` | none direct | missing | Port input view filter SQL/adapter. |
| `input_view_incremental_since_last_build.rs` | none direct | missing | Port incremental input view logic. |
| `multi_output_atomicity_aborts_all_on_partial_failure.rs` | `TestExecuteRollbackMultiOutputOnPartialCommitFailure`, `TestExecutorPartialMultiOutputCommitFailureRollsBackCommittedOutput` | config-gated+tested | Wire real transaction manager/Iceberg adapter. |
| `iceberg_output_client_commits_via_catalog.rs` | `TestOutputClientCommitOK`, `TestOutputClientTableMissing`, `TestOutputClientSchemaMismatch` | partial+tested | Add service config injection and end-to-end transaction test. |
| `parallel_execution_independent_jobs.rs` | `TestBuildOrchestratorRunsIndependentJobsInParallel`, executor DAG tests | implemented+tested | Add HTTP/plan adapter coverage. |
| `failure_cascade_dependent_only.rs` | `TestExecuteFailureInIntermediateNodeCascadesDependents` | implemented+tested | Add Rust response envelope parity. |
| `failure_cascade_all_non_dependent.rs` | `TestBuildOrchestratorAbortAllSetsAbortReason` | partial+tested | Add all-non-dependent executor/HTTP test. |
| `completed_jobs_persist_after_downstream_failure.rs` | none direct | missing | Add persistence hook test. |
| `job_lifecycle_invalid_transition_rejected.rs` | `TestSkippingStatesIsRejected`, `TestTerminalStatesHaveNoOutgoingEdges` | implemented+tested | Add DB transition adapter test. |
| `job_state_transitions_audit_trail.rs` | job lifecycle insert code only | unverified | Add pgx/sql adapter test or fake audit assertion. |
| `builds_audit_trail_complete.rs` | none | missing | Port build events/audit trail. |
| `builds_api_conformance_etag_on_get.rs` | none | missing | Implement `GET /v1/builds/{rid}` and ETag tests. |
| `builds_api_conformance_pagination.rs` | none | missing | Implement list pagination tests. |
| `builds_full_journey.rs` | pieces across resolver/executor tests | partial | Add full route-level journey after adapters. |
| `dry_run_resolve_endpoint.rs` | `TestDryRunResolveDoesNotPersist` | config-gated+tested | Mount exact Rust route and add path-param test. |
| `job_spec_publish_idempotent.rs` | none | missing | Implement `/v1/job-specs/{kind}`. |
| `validate_endpoint.rs` | `DryRunValidate` is 501 | missing | Implement validation endpoint or de-scope. |
| `runners_analytical_materializes_object_set.rs` | runner dispatch tests only | partial | Port analytical runner side effect. |
| `runners_export_writes_to_s3_target.rs` | runner dispatch tests only | partial | Port export runner + storage adapter test. |
| `runners_health_check_emits_findings.rs` | runner dispatch tests only | partial | Port health-check runner effects. |
| `runners_sync_calls_connector_service.rs` | runner dispatch tests only | partial | Port connector client runner test. |
| `preview_node_chain.rs` | none | missing | Port preview domain/handler if in service scope. |
| `media_node_validation.rs` | none | missing | Port authoring validation if in scope. |
| `pipeline_type_validation.rs` | none | missing | Port lifecycle/type validation and migration adapter. |
| `lifecycle_transitions.rs` | none | missing | Port pipeline lifecycle handlers/domain if in scope. |
| `project_scoped_run_uses_service_principal_token.rs` | none | missing | Port schedule/build client auth mode. |
| `user_mode_run_uses_user_jwt.rs` | none | missing | Port user JWT propagation. |
| `convert_to_project_scope_requires_manage_on_all_projects.rs` | none | missing | Port authorization/scope conversion. |
| `run_outcome_succeeded_when_build_started.rs` | schedule helpers only | missing | Port schedule dispatch/build client adapter. |
| `run_outcome_failed_when_build_service_5xx.rs` | none | missing | Port schedule run outcome error handling. |
| `run_outcome_ignored_when_outputs_fresh.rs` | none | missing | Requires staleness + schedule dispatch. |
| `auto_pause_after_threshold_consecutive_failures.rs` | `TestShouldAutoPauseAfterThresholdConsecutiveFailures` | partial+tested | Add DB sweep/apply test. |
| `auto_pause_exempt_skips_auto_pause.rs` | `TestAutoPauseExemptHonoursMarker` | partial+tested | Add persisted schedule sweep test. |
| `auto_unpause_after_first_success.rs` | `TestShouldAutoPauseResetsOnSuccess` | partial+tested | Add persisted auto-unpause test. |
| `pause_resets_event_observations.rs` | none | missing | Port schedule event observation persistence. |
| `sweep_apply_pauses_findings.rs` | none | missing | Port schedule sweep application. |
| `event_trigger_observed_persists_until_run.rs` | `TestEvaluateTriggerEventMatchesTargetAndBranch` only | partial | Add persistence test. |
| `trigger_compound_and_or.rs` | compound trigger tests in `schedule_test.go` | implemented+tested for pure evaluator | Add DB dispatch test. |
| `preview_next_fires_returns_correct_sequence.rs` | `TestBuildScheduleWindows*` | partial+tested | Add endpoint/envelope parity. |
| `schedule_version_diff_returns_structured_diff.rs` | none | missing | Port schedule version diff. |
| `schedule_version_snapshot_on_edit.rs` | none | missing | Port schedule versioning. |
| `migration_legacy_cron_to_trigger_json.rs` | `TestTriggerJSONRoundTripTime`, default TZ tests | partial | Add migration/backfill test. |
| `parameterized_rejects_time_trigger.rs` | none | missing | Port parameterized schedule validation. |
| `parameterized_run_with_overrides_writes_tagged_transaction.rs` | none | missing | Port parameterized run + tagged transaction. |
| `spark_submit_kube_stub.rs` | `TestSubmitSparkRunOK`, `TestSubmitSparkRunKubeUnavailableShape`, spark client tests | config-gated+tested | Mount exact Rust Spark paths. |
| `spark_e2e.rs` | spark render/client unit tests only | partial | Add end-to-end or env-gated integration. |
| `log_sink_persists_and_broadcasts.rs` | `TestStreamJobLogs*` only | partial | Implement emit/postgres sink/broadcast integration. |
| `sse_streams_logs_to_subscriber.rs` | `TestStreamJobLogsMultipleLiveEvents` | config-gated+tested | Mount exact `/v1` route. |
| `sse_resumes_from_last_sequence_after_disconnect.rs` | `TestStreamJobLogsInitialHistory` covers `Last-Event-ID` | config-gated+tested | Add exact Rust route test. |
| `log_color_coding_levels_correct.rs` | none direct | missing | Port log-level/color mapping tests. |

## Ordered backlog by real dependencies

### 1. DB / migrations / production adapters

1. Decide schema ownership: copy Rust migrations into Go, run Rust migration bundle from Go deploys, or move migrations to a shared location.
2. Implement Postgres adapters for:
   - `resolver.JobSpecRepository` (`job_specs`).
   - `resolver.DatasetVersioningRepository` and `resolver.BranchLockRepository` (dataset-versioning/branch tables or HTTP clients as Rust does).
   - `BuildRepository`, `BuildPlanRepository`, `PipelineRunRepository`, `AbortBuildRepository` (`builds`, `jobs`, `pipeline_runs`, `output_transactions`).
   - `logs.Store` + log broadcaster (`job_logs`).
   - schedule repositories (`schedules`, `schedule_runs`, definitions/version snapshots).
3. Wire `FOUNDRY_ICEBERG_CATALOG_*` into config/server and expose an `executor.TransactionManager` backed by `internal/iceberg`.

### 2. Exact Rust HTTP mounts and handler contract tests

1. Mount `/api/v1/data-integration/*` exact Rust routes and make compatibility `/api/v1/*` routes delegate only after tests pass.
2. Mount `/v1/*` builds/job-spec/log routes, including `POST /v1/builds/{rid}:abort`.
3. Mount `/api/v1/pipeline/builds/run` and `/api/v1/pipeline/builds/{run_id}/status` for Spark.
4. For every route, add route-level tests for status code, body envelope, path params, and auth/config-gated errors.

### 3. Executor / run lifecycle

1. Finish persisted plan loading for `ExecutePipeline`.
2. Implement retry and cancel routes; include lock release/output transaction abort semantics.
3. Port staleness detection and force-build behavior.
4. Port run guarantees and audit/build event enqueue.

### 4. Schedules, logs, Spark, Iceberg

1. Port scheduler dispatch (`run_due_scheduled_pipelines`) after schedule repositories exist.
2. Implement log list/emit/websocket and Postgres sink; keep SSE tests as a base.
3. Keep Spark client tests, then add exact Rust route tests and optional env-gated e2e.
4. Add end-to-end Iceberg transaction tests through executor after config/main wiring.

### 5. Tests / conformance closure

1. For each Rust integration test above, create a named Go equivalent or mark it intentionally out-of-scope with rationale.
2. Do not mark any row `implemented+tested` unless a Go test name is recorded in this document.
3. Keep `go test ./services/pipeline-build-service/...` green before starting functional changes in `pipeline-build-service`.
