# `workflow-automation` worker functional inventory

> **Migration context.** FASE 5 / Tarea 5.1 of
> [`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../migration-plan-foundry-pattern-orchestration.md).
> The plan describes this worker as "the most complex" — orchestrates
> ontology actions with retries, waits between steps, possible
> escalation; equivalent to Foundry **Automate** in pure form. This
> inventory establishes the **read-only baseline**: the worker, its
> single workflow, the activity it dispatches, and — most importantly
> — the heterogeneous set of producers that today land in a single
> `trigger_payload` map. **No code is being moved here**; the
> migration itself lands in subsequent tasks (5.2–5.4).

---

## 1. Worker process topology

| Field | Value | Source |
|---|---|---|
| Worker binary | `workers-go/workflow-automation/main.go` | [main.go](../../../workers-go/workflow-automation/main.go) |
| Temporal task queue | `openfoundry.workflow-automation` (constant `contract.TaskQueue`) | [contract.go:11](../../../workers-go/workflow-automation/internal/contract/contract.go) |
| Registered workflows | **`WorkflowAutomationRun`** (only one — registered name pinned via `RegisterWorkflowWithOptions`) | [main.go:58-60](../../../workers-go/workflow-automation/main.go) |
| Registered activities | `(*Activities).ExecuteOntologyAction` (only one) | [activities.go:80](../../../workers-go/workflow-automation/activities/activities.go) |
| Health / metrics | `:9090/healthz`, `:9090/metrics` (placeholder; mirrors `pipeline` / `reindex`) | [main.go:77-90](../../../workers-go/workflow-automation/main.go) |
| Cross-language contract | Mirrors `libs/temporal-client::AutomationRunInput` and `task_queues::WORKFLOW_AUTOMATION` / `workflow_types::AUTOMATION_RUN` | [contract.go:11-25](../../../workers-go/workflow-automation/internal/contract/contract.go), [temporal-client lib.rs:1627,1637](../../../libs/temporal-client/src/lib.rs) |
| Canonical workflow id | `workflow-automation:{definition_id}:{run_id}` | [temporal-client lib.rs:1738](../../../libs/temporal-client/src/lib.rs) |

Configuration env (worker → Rust services):

| Env var | Default | Purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` / `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Temporal frontend gRPC |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.workflow-automation` | Task queue polled |
| `OF_LOG_LEVEL` | `info` | slog level |
| `METRICS_ADDR` | `:9090` | Prometheus exporter address |
| `OF_ONTOLOGY_ACTIONS_URL` (or `ONTOLOGY_ACTIONS_SERVICE_URL`, `ONTOLOGY_SERVICE_URL`, legacy `OF_ONTOLOGY_ACTIONS_GRPC_ADDR`) | _(required for the action activity)_ | `ExecuteOntologyAction` HTTP base |
| `OF_ONTOLOGY_ACTIONS_BEARER_TOKEN` (or `ONTOLOGY_ACTIONS_BEARER_TOKEN`) | _(required)_ | Service bearer token used on `POST /api/v1/ontology/actions/{id}/execute` |

---

## 2. Workflow inventory

There is exactly **one workflow type** registered on this task queue.

### `WorkflowAutomationRun` — single-step ontology-action dispatcher

Source: [`workflows/automation_run.go`](../../../workers-go/workflow-automation/workflows/automation_run.go)

```text
WorkflowAutomationRun(input) → result
   │
   ├── ActivityOptions{ StartToClose=5m, Retry: init=30s, max=10m, attempts=5 }
   │
   ├── if triggerHasOntologyAction(input.TriggerPayload):
   │       ExecuteActivity(ExecuteOntologyAction, input) → map[string]any
   │       └── On error: return AutomationRunResult{status="failed", error}
   │       return AutomationRunResult{status="completed", result}
   │
   └── else:
       return AutomationRunResult{status="completed"}   // no-op
```

| Field | Value |
|---|---|
| Inputs (`AutomationRunInput`) | `run_id`, `definition_id`, `tenant_id`, `triggered_by`, `trigger_payload` (free-form `map[string]any`) |
| Outputs (`AutomationRunResult`) | `run_id`, `status` (`completed`/`failed`/`cancelled`), `error?`, `result?` |
| Search attributes | `audit_correlation_id` (= `run_id`) — set on the start request by the Rust adapter ([`temporal_adapter.rs:80-89`](../../../services/workflow-automation-service/src/domain/temporal_adapter.rs)) |
| Signals | **None.** No `workflow.GetSignalChannel` calls anywhere. |
| Queries | **None.** No `workflow.SetQueryHandler` calls. The `query_run_state` adapter on the Rust side ([`temporal_adapter.rs:118-128`](../../../services/workflow-automation-service/src/domain/temporal_adapter.rs)) targets handlers that **do not exist yet** — it would currently fail at runtime against this worker. |
| Child workflows | **None.** Single activity, single result. |
| Compensation | **None.** Failure is reported via the result; no rollback step. |
| Determinism hazards | None observed (no `time.Now`, `rand`, native goroutines in workflow code). |

`triggerHasOntologyAction` recognises **either** of two payload shapes:
`{action_id: "..."}` at the root, or `{ontology_action: {action_id: "..."}}` nested
([automation_run.go:85-97](../../../workers-go/workflow-automation/workflows/automation_run.go)).
If neither matches, the workflow returns `status="completed"` without
side effects — i.e. it acts as a no-op for any non-action payload.

This is the single most important fact of this inventory: **the worker
today only knows how to execute ontology-action effects.** Every
"branching", "parallel", "compensation", "human-in-the-loop",
"step_runner", "simulation" file the Rust service still ships under
[`services/workflow-automation-service/src/domain/`](../../../services/workflow-automation-service/src/domain/)
is an empty stub left over from the pre-Temporal in-process executor
(verified: `wc -l` returns 0 for `compensation.rs`,
`human_in_loop.rs`, `parallel.rs`, `simulation.rs`, `step_runner.rs`,
plus `models/action.rs` and `models/trigger.rs`). The `branching.rs`
condition evaluator has 51 lines of code but no callers — it is
currently dead code.

---

## 3. Activity inventory

There is exactly **one activity** registered on this task queue.

### 3.1 `ExecuteOntologyAction` → `ontology-actions-service`

Source: [`activities/activities.go:80-105`](../../../workers-go/workflow-automation/activities/activities.go)

| Aspect | Detail |
|---|---|
| Target service | `ontology-actions-service` (`OF_ONTOLOGY_ACTIONS_URL`, default _none_; service listens on `:50106` per [`workflow-automation-service::config::default_ontology_service_url`](../../../services/workflow-automation-service/src/config.rs)) |
| Endpoint called | `POST /api/v1/ontology/actions/{action_id}/execute` (route registered in [`ontology-actions-service::lib.rs:44`](../../../services/ontology-actions-service/src/lib.rs)) |
| Headers | `authorization: Bearer …` (required), `x-audit-correlation-id: <run_id>`, `content-type: application/json` |
| Input | `AutomationRunInput`; the activity extracts the action invocation from `TriggerPayload` (root `action_id` first, falls back to `ontology_action.action_id`) and ignores every other field |
| Request body | `{ parameters: {...}, target_object_id?: "...", justification?: "..." }` |
| Output | `{status:"completed", action_id, target_object_id?, response: <decoded JSON body of the upstream response>}` |
| Failure modes | `nonRetryable("invalid_ontology_action_input"` / `"ontology_actions_config"` / `"ontology_actions_request"` / `"ontology_actions_client_error"`)` for 4xx (except 429); transient retry for 5xx and 429 |
| Side effects | None on the worker side. The downstream Rust handler owns audit + Cedar authz + dataset writes. |

There are **no other activities** registered on this task queue today.
Anything the Rust service comments still mention from the legacy
in-process executor (notification-send, branching, approval issue,
parallel fan-out) is not implemented in the worker.

---

## 4. How `WorkflowAutomationRun` is triggered

Unlike `pipeline` (2 producers) and `reindex` (0 in-tree producers),
this workflow has **6 distinct producer paths**. They all converge on
the same `dispatch_run` Rust function ([`execute.rs:165`](../../../services/workflow-automation-service/src/handlers/execute.rs))
which calls `TemporalAdapter::start_run` to enqueue the workflow on
Temporal, but the `trigger_type` string they pass and the **shape they
stuff into `trigger_payload`** vary considerably.

### 4.1 Producer matrix

| # | `trigger_type` | Producer | Surface | Path to worker | Shape of `trigger_payload` |
|---|---|---|---|---|---|
| 1 | `manual` | UI / external authenticated client | `POST /api/v1/workflows/{id}/runs` ([execute.rs:21](../../../services/workflow-automation-service/src/handlers/execute.rs)) | HTTP → `dispatch_run` → Temporal start | `body.context` verbatim (free-form, set by the workflow definition's UI) |
| 2 | `webhook` | external system (HMAC-authenticated) | `POST /api/v1/workflows/{id}/webhook` ([execute.rs:43](../../../services/workflow-automation-service/src/handlers/execute.rs)) | HTTP → `dispatch_run` → Temporal start | `{trigger:{type:"webhook", workflow_id}, payload: body.context}` |
| 3 | `lineage_build` | `pipeline-build-service` / `lineage-service` lineage materialisation flow | `POST /api/v1/workflows/{id}/_internal/lineage` ([execute.rs:97](../../../services/workflow-automation-service/src/handlers/execute.rs)); callers at [pipeline-build-service lineage/mod.rs:413](../../../services/pipeline-build-service/src/domain/lineage/mod.rs), [lineage-service lineage/mod.rs:444](../../../services/lineage-service/src/domain/lineage/mod.rs) | HTTP (service-to-service, unauthenticated) → `dispatch_run` → Temporal start | `body.context` (lineage build payload) |
| 4 | `event` | `pipeline-schedule-service` `POST /workflows/events/{event_name}` ([pipeline-schedule-service main.rs:268](../../../services/pipeline-schedule-service/src/main.rs)); fan-out helper [`trigger_event_workflows`](../../../services/pipeline-schedule-service/src/domain/workflow.rs) | NATS subject `of.workflows.run.requested` ([contracts.rs:8](../../../libs/event-bus-control/src/contracts.rs)) → consumed by `workflow-automation-service::workflow_run_requested::consume` ([workflow_run_requested.rs:15](../../../services/workflow-automation-service/src/domain/workflow_run_requested.rs)) | NATS → consumer → `execute_internal_triggered_run` → `dispatch_run` → Temporal start | `{trigger:{type:"event", name, actor_id}, event: <upstream event>}` |
| 5 | `cron` | `pipeline-schedule-service` reconciles workflows where `trigger_type='cron'` into a Temporal Schedule (see [`pipeline-schedule-service::domain::workflow::workflow_schedule_expression`](../../../services/pipeline-schedule-service/src/domain/workflow.rs)) | Temporal Schedule (server-side cron) → directly starts `WorkflowAutomationRun` on `openfoundry.workflow-automation` task queue | Whatever payload was set on the Schedule action when it was created (typically empty / `{}`) |
| 6 | `event` (NATS direct) | Any internal service that calls `pipeline-schedule-service::domain::workflow::trigger_internal_workflow_run` directly | NATS publish on `of.workflows.run.requested` | Same as #4: consumed by `workflow_run_requested::consume`; producer-supplied `context` |

Important asymmetry today: **paths #1, #2, #3, #4, #6 all funnel
through `dispatch_run` on the Rust adapter**, but **path #5 bypasses
the service entirely** — Temporal Schedule fires the workflow
directly. This means cron-triggered runs do not go through the same
audit / projection write path as the others.

### 4.2 What the worker actually distinguishes

After the trigger has been turned into a `trigger_payload` map and
handed to the workflow, the worker only branches on **one bit**:
"does the payload carry an `action_id`?". Concretely, the only two
runtime kinds the worker observes are:

| Kind | Detection | Outcome |
|---|---|---|
| `OntologyActionInvocation` | Root `action_id` non-empty, OR `ontology_action.action_id` non-empty | `ExecuteOntologyAction` activity → ontology-actions-service POST → `status=completed` (or `failed`) |
| `Noop` | Anything else | `status=completed` immediately, no activity dispatch |

The fan-out happens **upstream** (which producer chose to put an
`action_id` in the context); the worker is monomorphic.

### 4.3 Where state lives today

| Surface | Today |
|---|---|
| Workflow definition (catalog) | `workflows` table in `pg-policy.workflow_automation` ([migration 20260421140000](../../../services/workflow-automation-service/migrations/20260421140000_workflows.sql)) — fields: `id, name, owner_id, status, trigger_type, trigger_config, steps, webhook_secret, next_run_at, last_triggered_at` |
| Workflow run history | `workflow_runs` table exists in the same migration, **but no INSERT is wired in current code** — `accepted_run` ([execute.rs:224-251](../../../services/workflow-automation-service/src/handlers/execute.rs)) builds the response in memory only |
| Run projections (read-side) | `workflow_run_projections` table ([migration 20260503090000](../../../services/workflow-automation-service/migrations/20260503090000_workflow_run_projections.sql)) — read by `runs::list_runs` ([runs.rs:23](../../../services/workflow-automation-service/src/handlers/runs.rs)). **No writer in-tree** — the projection table is empty until a future projector is added. |
| Authoritative live state | Temporal visibility (per [`temporal_adapter::list_runs`](../../../services/workflow-automation-service/src/domain/temporal_adapter.rs) and the module doc-comment "the runtime view of 'what runs exist for this definition' is sourced from Temporal visibility … instead of the legacy `workflow_run_projections` Postgres table") |

So the live truth is in Temporal and only there. Any FASE 5 design
that retires Temporal must replace **both** (a) the dispatch
substrate and (b) the visibility surface — Tarea 5.2's
`automation_runs` state-machine table covers both.

---

## 5. Trigger payload shapes catalog

The full set of `trigger_payload` shapes a `WorkflowAutomationRun`
execution can see today, derived from the producer matrix in §4.1 and
the worker's `triggerHasOntologyAction` predicate.

### 5.1 Ontology action invocation (only kind the worker acts on)

```json
// flat form (preferred by activity tests)
{
  "action_id": "string",                  // required
  "target_object_id": "string",           // optional
  "parameters": { ... },                  // optional, free-form
  "justification": "string"               // optional
}

// nested form (legacy compatibility)
{
  "ontology_action": {
    "action_id": "string",                // required
    "target_object_id": "string",         // optional
    "parameters": { ... },                // optional
    "justification": "string"             // optional
  }
}
```

Producer: any of the 6 producer paths in §4.1, **provided** the
caller put an `action_id` field in the context. Today, no producer
synthesises this shape automatically — it is the job of the workflow
definition's UI / API caller to embed it.

### 5.2 Webhook envelope

```json
{
  "trigger": { "type": "webhook", "workflow_id": "<uuid>" },
  "payload": <free-form body sent to the webhook>
}
```

Producer: path #2 only. The worker treats this as `Noop` unless
`payload.action_id` exists — and since the wrapping `trigger`/`payload`
structure means `action_id` is **never at the root**, webhook-triggered
runs only do real work if the upstream sender uses the nested
`ontology_action` form _inside_ `payload`. There is no test of this
path in the worker today.

### 5.3 Event envelope

```json
{
  "trigger": { "type": "event", "name": "<event_name>", "actor_id": "<uuid>" },
  "event":   <upstream domain event payload>
}
```

Producer: path #4 (NATS event fan-out). Same caveat as §5.2: the
worker only fires the activity if the `event` payload contains a
nested `ontology_action.action_id` — which is not a documented
contract anywhere.

### 5.4 Lineage envelope

Free-form JSON value coming in via `body.context` of
`POST /api/v1/workflows/{id}/_internal/lineage`. Callers in
[`pipeline-build-service::domain::lineage`](../../../services/pipeline-build-service/src/domain/lineage/mod.rs)
and [`lineage-service::domain::lineage`](../../../services/lineage-service/src/domain/lineage/mod.rs)
populate this with workflow-definition-specific lineage metadata.
Same caveat: only acted on if it carries an `action_id` somewhere.

### 5.5 Manual / cron / direct-NATS

`body.context` (or the Schedule-action input) verbatim, with no
wrapping. Workflow definition author chooses what goes in.

---

## 6. Migration table — Temporal pieces → Automate-pattern targets

Aligned with FASE 5 of the migration plan and ADR-0022 (outbox) +
ADR-0038 (idempotency contract). Each row is a discrete piece of
behaviour observable in `workers-go/workflow-automation/` (or in the
adjacent Rust adapter that funnels into it) today; the right-hand
column is the post-migration owner.

| # | Today (Temporal) | Tomorrow (Automate pattern) | Notes / target topic |
|---|---|---|---|
| 1 | `WorkflowAutomationRun` workflow registered on `openfoundry.workflow-automation` | **Removed.** Orchestration is a Postgres state machine row in `workflow_automation.automation_runs` driven by a Kafka consumer. | Tarea 5.2 schema. |
| 2 | `ExecuteOntologyAction` activity → `POST /actions/{id}/execute` | **Inline HTTP call** from `workflow-automation-service::condition_consumer` (no separate worker hop needed). Same endpoint, same request body, same headers. | Effect call unchanged. |
| 3 | Single-step DAG inside the workflow | Single state transition `Queued → Running → Completed/Failed` in `automation_runs`. Multi-step variants (Tarea 5.2 mentions `Suspended`, `Compensating`) are introduced **before** any caller actually uses them — purely a schema affordance for future expansion. | n/a |
| 4 | Activity retries (`InitialInterval=30s`, backoff 2.0, `Max=10m`, `MaximumAttempts=5`) | Consumer-side `tokio::time::sleep` with the same envelope on transient HTTP errors; dead-letter to `__dlq.automate.condition.v1` after N attempts (per migration plan Tarea 5.3 step 2.f) | Same semantics, different substrate. |
| 5 | `StartToCloseTimeout=5m` per activity | `reqwest` per-request timeout + Postgres `expires_at` column on the row drives a hard wall-clock kill (sweep job, similar to Tarea 7.4 cron). | Same semantics. |
| 6 | Path #1 — manual run via `POST /workflows/{id}/runs` | Same HTTP API; handler **publishes via outbox to `automate.condition.v1`** (event_id = UUIDv5(definition_id ‖ run_id)) instead of starting a Temporal workflow. Returns `202 Accepted`. | Producer change inside the same handler. |
| 7 | Path #2 — webhook trigger | Same as #6 after HMAC verification. The `{trigger, payload}` envelope is normalised into the same `automate.condition.v1` event shape. | Producer change. |
| 8 | Path #3 — lineage trigger | Two options (decide in Tarea 5.3): keep the HTTP `/_internal/lineage` route (publishing internally into outbox) **or** replace with a direct service-to-service publish from `pipeline-build-service` / `lineage-service` outbox to `automate.condition.v1`. The HTTP route is preferable in the first iteration (smaller blast radius). | Producer change. |
| 9 | Path #4 + #6 — NATS `of.workflows.run.requested` consumed by `workflow_run_requested::consume` | **Replace NATS with Kafka.** Producers (today: `pipeline-schedule-service::trigger_internal_workflow_run`) publish via outbox to `automate.condition.v1`. The current `workflow_run_requested::consume` task disappears; the new condition consumer is the single entry point. | Substrate change. NATS subject deprecated. |
| 10 | Path #5 — Temporal Schedule for `trigger_type='cron'` | `pipeline-schedule-service` reconciles its cron workflow rows into **k8s `CronJob`** resources that call back into `workflow-automation-service::POST /api/v1/workflows/{id}/runs` (or directly publish to `automate.condition.v1` from a small CronJob image). | Substrate change; mirrors the FASE 3 / Tarea 3.5 pattern for pipelines. |
| 11 | Path #4 (event) — `POST /workflows/events/{event_name}` on `pipeline-schedule-service` matches active `event`-triggered workflows | Move the matcher into `workflow-automation-service` itself: a Kafka consumer on the **upstream** domain topic (e.g. `ontology.changes.v1`) joins against `workflows WHERE trigger_type='event' AND trigger_config->>event_name = <topic-name>`. Per match, publish `automate.condition.v1`. Eliminates the cross-service hop. | Routing logic moves; outbox-driven. |
| 12 | Audit correlation id propagation via `x-audit-correlation-id` HTTP header (today set from `run_id`) | Same header continues to flow on the effect call; carried as a header on the `automate.condition.v1` event so the consumer stamps it on the outgoing HTTP request. | Cross-cutting. |
| 13 | `workflow_runs` / `workflow_run_projections` Postgres tables (declared but unused for writes) | Replaced by `workflow_automation.automation_runs` from Tarea 5.2; old tables dropped in a follow-up. | DB-side cleanup. |
| 14 | Authoritative live state in Temporal visibility (`temporal_adapter::list_runs` / `query_run_state`) | Authoritative live state is the `automation_runs` row. `list_runs` reads from Postgres (LIMIT/OFFSET). `query_run_state` is removed (no callers in tree depend on it; the no-op handlers it would target don't exist). | Visibility move. |
| 15 | `cancel_run` adapter method ([`temporal_adapter.rs:91`](../../../services/workflow-automation-service/src/domain/temporal_adapter.rs)) — sends Temporal cancel | New HTTP route + state transition `Running → Cancelled` on the row, surfaced through the same response shape. Consumer polls between effect retries to honour cancel. | API preserved. |
| 16 | Approval continuation `POST /workflows/approvals/{approval_id}/continue` → `ApprovalsClient::decide` (Temporal signal) | **Out of scope for FASE 5** — handled by FASE 7 (`approvals-worker` refactor). The handler stays Temporal-backed until the approvals service is migrated, then both pieces are switched over together. | Cross-phase dependency. |

---

## 7. Target service structure (`workflow-automation-service` 2.0)

Per Tarea 5.3, the service grows from "thin REST→Temporal adapter +
NATS consumer" to a self-contained Automate runtime. Three concurrent
tokio tasks under the same `main.rs` `tokio::join!` (or `tokio::spawn`
fan-out, mirroring the existing `workflow_run_requested::consume`
spawn at [main.rs:102-106](../../../services/workflow-automation-service/src/main.rs)).

```text
                       ┌─────────────────────────────────────────────────────┐
                       │             workflow-automation-service              │
                       │  (one Rust binary, three concurrent tokio tasks)     │
                       │                                                       │
   user / webhook ────►│  (a) HTTP API (axum)                                  │
   service-to-svc      │      - POST /workflows/{id}/runs       (manual)       │
                       │      - POST /workflows/{id}/webhook    (webhook)      │
                       │      - POST /workflows/{id}/_internal/lineage         │
                       │      - GET  /workflows/{id}/runs       (read state)   │
                       │      - POST /workflows/{id}/cancel     (cancel)       │
                       │      └─► writes outbox row in same TX as work      ──┐│
                       │                                                       ││
   k8s CronJob   ─────►│  (b) Cron entry — same /runs endpoint                 ││
                       │                                                       ││
   automate.condition  │  (c) Condition consumer (rdkafka)                     ││
   .v1 (Kafka)   ─────►│      - dedup via libs/idempotency (event_id UUIDv5)   ││
                       │      - load AutomationDefinition from pg              ││
                       │      - INSERT automation_runs(state=Running)          ││
                       │      - HTTP POST ontology-actions-service             ││
                       │      - on success: state=Completed, write outbox     ──┤
                       │      - on retryable err: backoff + retry              ││
                       │      - on terminal err: state=Failed, write outbox   ──┤
                       │                                                       ││
                       └────────────────────────────────────────────────────┬──┘│
                                                                            │   │
                              outbox.events  ──── Debezium ──► Kafka ◄──────┘   │
                                                                                │
                             ┌──────────────────────────────────────────────────┘
                             ▼
                    ┌─────────────────────┐
                    │ automate.outcome.v1 │  (downstream: notifications,
                    └─────────────────────┘   audit-compliance, UI feed)
```

Component cardinality:

| Component | Substrate | Notes |
|---|---|---|
| HTTP API | axum, port `:50137` (unchanged) | Same routes today minus the Temporal-specific ones; `/runs` becomes outbox-publish |
| Condition consumer | rdkafka (`event-bus-data`); group `workflow-automation-condition`; topic `automate.condition.v1` | Single consumer per service replica; partitioned by `tenant_id` |
| Effect dispatcher | `reqwest::Client` shared (already present in `AppState`) | Same env vars (`OF_ONTOLOGY_ACTIONS_*`); per-request 30 s timeout matches today's `HTTPClient.Timeout` |
| Outcome publisher | Outbox row in same TX as state transition; published by Debezium on `automate.outcome.v1` | event_id = UUIDv5(`run_id ‖ "outcome" ‖ status`) |
| State store | `pg-policy.workflow_automation.automation_runs` (Tarea 5.2) | Optimistic concurrency via `version` column |
| Timeout sweep | (deferred) k8s CronJob reading rows with `expires_at < now()` | Mirror of Tarea 7.4; can land in Tarea 5.3 follow-up |

What goes away:
- `domain/workflow_run_requested.rs` (NATS consumer) — replaced by Kafka condition consumer.
- `domain/temporal_adapter.rs` (Temporal start / list / query / cancel) — gone with FASE 8.
- `tests/temporal_e2e.rs` — replaced by an outbox→consumer→effect integration test.
- The empty domain stubs (`compensation.rs`, `human_in_loop.rs`, `parallel.rs`, `simulation.rs`, `step_runner.rs`, `models/action.rs`, `models/trigger.rs`) can be deleted without impact.

What stays:
- `domain/lineage.rs` (snapshot builder + sync HTTP client) — pure functional translator, unrelated to Temporal.
- `domain/branching.rs` — currently dead but kept as a building block in case multi-step automations re-enter the picture; revisit in Tarea 5.3 step 1.
- `handlers/crud.rs` (workflows CRUD) — independent of execution substrate.
- `handlers/approvals.rs` (`/workflows/approvals/{approval_id}/continue` → `ApprovalsClient`) — left untouched until FASE 7 migrates it together with `approvals-service`.

---

## 8. Net call graph (today)

```text
                        ┌──────────────────────────────────────┐
   UI / external  ─────►│ POST /workflows/{id}/runs            │ #1 manual
                        │ POST /workflows/{id}/webhook         │ #2 webhook
                        └──────────────────────────────────────┘
   pipeline-build /     ┌──────────────────────────────────────┐
   lineage-service ────►│ POST /workflows/{id}/_internal/lineage│ #3 lineage_build
                        └──────────────────────────────────────┘
                                            │
                                            ▼
                        ┌──────────────────────────────────────┐
                        │ workflow-automation-service           │
                        │  dispatch_run() → TemporalAdapter     │
                        │  AppState{workflow_client}            │
                        └──────────────────────────────────────┘
                                            │ start_workflow_execution
                                            ▼
                        ┌──────────────────────────────────────┐
                        │ Temporal frontend (gRPC)              │
                        │ ns=default, tq=of.workflow-automation │
                        └──────────────────────────────────────┘
                                            │ poll
                                            ▼
                        ┌──────────────────────────────────────┐
                        │ workers-go/workflow-automation worker │
                        │   AutomationRun workflow              │
                        │   ├── (action_id present?)            │
                        │   │   └── ExecuteOntologyAction       │
                        │   └── else: noop completed            │
                        └──────────────────────────────────────┘
                                            │ HTTP POST
                                            ▼
                        ┌──────────────────────────────────────┐
                        │ ontology-actions-service              │
                        │ POST /api/v1/ontology/actions/{id}/   │
                        │      execute                          │
                        └──────────────────────────────────────┘

   (in parallel — paths #4 / #5 / #6)

   pipeline-schedule-service ─POST /workflows/events/{event_name}─►
                                   │
                                   ▼
                       NATS subject  of.workflows.run.requested
                                   │
                                   ▼
                       workflow-automation-service::workflow_run_requested::consume
                                   │ → execute_internal_triggered_run → dispatch_run
                                   ▼
                       (joins the Temporal path above)

   pipeline-schedule-service ──reconcile cron workflows──►
                              Temporal Schedule (server-side cron)
                                   │ fires
                                   ▼
                              starts AutomationRun directly
                              on tq=of.workflow-automation
                              (bypasses dispatch_run / Rust adapter)
```

## 9. Net call graph (post-migration target, for orientation only)

```text
   user / webhook / svc-to-svc ──POST /workflows/{id}/runs──►┐
                                                              │
   k8s CronJob                  ──POST /workflows/{id}/runs──►│
                                                              │
                                                              ▼
                          ┌─────────────────────────────────────────┐
                          │      workflow-automation-service        │
                          │  HTTP handler:                          │
                          │   1. INSERT automation_runs(Queued)     │
                          │   2. INSERT outbox.events               │
                          │   (same TX, atomic)                     │
                          │   3. return 202                         │
                          └─────────────────────────────────────────┘
                                                              │
                                                Debezium      │
                                                              ▼
                                                   ┌────────────────────────┐
                                                   │ automate.condition.v1  │
                                                   └────────────────────────┘
                                                              │
                                                              ▼
                          ┌─────────────────────────────────────────┐
                          │  workflow-automation-service            │
                          │   condition consumer (same binary)      │
                          │   1. dedup via libs/idempotency         │
                          │   2. load AutomationDefinition          │
                          │   3. UPDATE automation_runs(Running)    │
                          │   4. HTTP POST ontology-actions-service │
                          │   5. on success/fail:                   │
                          │      UPDATE automation_runs +           │
                          │      INSERT outbox.events (one TX)      │
                          └─────────────────────────────────────────┘
                                                              │
                                                Debezium      │
                                                              ▼
                                                   ┌────────────────────────┐
                                                   │ automate.outcome.v1    │
                                                   └────────────────────────┘
                                                              │
                                                              ▼
                                            notifications, audit-compliance,
                                            UI live feed, lineage updates
```

---

## 10. Risks / non-functional notes

* **Hidden semantic gap on producers #2/#3/#4** — those producers
  wrap their context inside a `{trigger, payload|event}` envelope, so
  `triggerHasOntologyAction` only matches when the upstream sender
  also nests an `ontology_action` block inside `payload` / `event`.
  Today this means webhook / lineage / event triggers silently no-op
  unless the workflow definition author explicitly fills the action
  block. The migration is a good time to either (a) standardise the
  envelope (always `automate.condition.v1` carries `{action: {id, ...},
  context: {...}}`) or (b) document the no-op behaviour explicitly.
* **Unimplemented query handlers** — `temporal_adapter::query_run_state`
  is callable from Rust but the worker registers no `SetQueryHandler`,
  so the route would currently fail at runtime. The replacement is
  `GET /workflows/{id}/runs/{run_id}` reading from `automation_runs`.
* **Dead code stubs** — `compensation.rs`, `human_in_loop.rs`,
  `parallel.rs`, `simulation.rs`, `step_runner.rs`, `models/action.rs`,
  `models/trigger.rs` are all empty files. Tarea 5.4 should delete
  them; they create the false impression that the legacy executor's
  surface still exists in some form.
* **Cron path #5 bypasses the service** — Temporal Schedule writes
  directly to the worker. Post-migration this becomes a CronJob
  hitting `/runs`, which restores the audit-correlation-id +
  outbox-write invariant for cron-triggered runs.
* **NATS subject deprecation** — `of.workflows.run.requested` has
  exactly one consumer (`workflow_run_requested::consume`) and one
  producer (`pipeline-schedule-service::trigger_internal_workflow_run`).
  Removing it is a 2-file change, but both must move in the same PR
  to avoid orphaned events on the JetStream stream.
* **Approval continuation is FASE 7 territory** —
  `/workflows/approvals/{approval_id}/continue` calls `ApprovalsClient::decide`
  which signals the Temporal `ApprovalRequest` workflow owned by
  `approvals-service`. Don't migrate it during FASE 5; it goes when
  `approvals-worker` does.
* **`workflow_runs` and `workflow_run_projections` are write-orphan
  tables** — they exist in migrations but have no writer in current
  code. If Tarea 5.2's `automation_runs` becomes the new state store,
  drop both old tables in the same migration to avoid future
  confusion.
* **HTTP-only effect surface** — the activity is a thin HTTP/JSON
  client (consistent with ADR-0021 §Wire format). The migration does
  not need to introduce gRPC or any new transport — `reqwest` against
  the same endpoint preserves the contract.
* **Determinism is moot post-migration** — the workflow body has no
  determinism hazards today (no `time.Now`, no `rand`, no goroutines),
  so removing the determinism harness is risk-free. The Foundry
  pattern's idempotency guarantee comes from `libs/idempotency` +
  deterministic `event_id`, not from replay.

---

## 11. Acceptance for Tarea 5.1

* This document exists at `docs/architecture/refactor/workflow-automation-worker-inventory.md`.
* §2 documents the single workflow (`WorkflowAutomationRun`) with its
  full input / output / retry-policy / Temporal-feature footprint.
* §3 documents the single activity (`ExecuteOntologyAction`) with its
  full HTTP contract (URL, headers, body, response, error mapping).
* §4 lists every "trigger payload" producer in the codebase (six
  paths), and §5 catalogues every payload shape the worker can
  observe.
* §6 maps each Temporal primitive in use today to its Automate-pattern
  replacement (Postgres state machine + outbox + Kafka condition /
  outcome topics + same HTTP effect endpoint), ready to drive Tareas
  5.2 (state-machine schema), 5.3 (consumer + dispatcher service)
  and 5.4 (worker deletion).
* §7 sketches the post-migration `workflow-automation-service` 2.0
  shape (HTTP API + condition consumer + effect dispatcher + outcome
  publisher) so Tarea 5.3 has a starting blueprint.
