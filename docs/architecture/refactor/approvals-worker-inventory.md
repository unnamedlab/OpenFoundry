# `approvals` worker functional inventory

> **Migration context.** FASE 7 / Tarea 7.1 of
> [`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../migration-plan-foundry-pattern-orchestration.md).
> The plan retires the Temporal-backed `ApprovalRequestWorkflow` in
> favour of a plain Postgres state machine + a CronJob-driven
> timeout sweep. This inventory is the **read-only baseline** —
> what the worker, the adjacent Rust adapter and the cross-service
> callers actually do today, plus the gap between the plan's
> approval-type taxonomy ("single, multi-approver, threshold-based,
> time-based escalation") and the code (only single-approver
> exists). **No code is being moved here**; the migration itself
> lands in subsequent tasks (7.2–7.5).

---

## 1. Worker process topology

| Field | Value | Source |
|---|---|---|
| Worker binary | `workers-go/approvals/main.go` | [main.go](../../../workers-go/approvals/main.go) |
| Temporal task queue | `openfoundry.approvals` (constant `contract.TaskQueue`) | [contract.go:5](../../../workers-go/approvals/internal/contract/contract.go) |
| Registered workflows | **`ApprovalRequestWorkflow`** (only one — registered by Go function symbol) | [main.go:38](../../../workers-go/approvals/main.go) |
| Registered activities | `(*Activities).EmitAuditEvent` (only one) | [activities.go](../../../workers-go/approvals/activities/activities.go) |
| Health / metrics | `:9090/healthz`, `:9090/metrics` (placeholder; mirrors the other workers) | [main.go:54-66](../../../workers-go/approvals/main.go) |
| Cross-language contract | Mirrors `libs/temporal-client::ApprovalRequestInput` / `ApprovalDecision` and `task_queues::APPROVALS` / `workflow_types::APPROVAL_REQUEST` | [contract.go:4-32](../../../workers-go/approvals/internal/contract/contract.go), [temporal-client lib.rs `ApprovalsClient`](../../../libs/temporal-client/src/lib.rs) |
| Canonical workflow id | `approval:{request_id}` | [temporal-client lib.rs `ApprovalsClient::open`](../../../libs/temporal-client/src/lib.rs) |
| Signals | **`decide`** — the FIRST worker in the codebase that uses Temporal signals. | [approval_request.go:34](../../../workers-go/approvals/workflows/approval_request.go) |
| Timers | **24 h hard timeout** — the FIRST worker that uses `workflow.NewTimer`. | [approval_request.go:40](../../../workers-go/approvals/workflows/approval_request.go) |
| Determinism hazards | `workflow.Now(ctx)` is used (deterministic) — no `time.Now`, no `rand`, no goroutines. |

Configuration env (worker → Rust services):

| Env var | Default | Purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` / `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Temporal frontend gRPC |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.approvals` | Task queue polled |
| `OF_AUDIT_COMPLIANCE_URL` (or `OF_AUDIT_URL`, `AUDIT_COMPLIANCE_SERVICE_URL`, `AUDIT_SERVICE_URL`, legacy `OF_AUDIT_GRPC_ADDR`) | `http://audit-compliance-service:50115` | `EmitAuditEvent` HTTP base |
| `OF_AUDIT_BEARER_TOKEN` | _(empty)_ | Service bearer token used on `POST /api/v1/audit/events` |
| `OF_LOG_LEVEL` | `info` | slog level |
| `METRICS_ADDR` | `:9090` | Prometheus exporter address |

---

## 2. Workflow inventory

There is exactly **one workflow type** registered on this task queue.

### `ApprovalRequestWorkflow` — selector { signal `decide`, timer 24h }

Source: [`workflows/approval_request.go`](../../../workers-go/approvals/workflows/approval_request.go)

```text
ApprovalRequestWorkflow(input) → result
   │
   ├── selector := workflow.NewSelector(ctx)
   │     ├── signal "decide" → decision
   │     └── timer 24h       → decision = {outcome: "expired"}
   │   selector.Select(ctx)   ← BLOCKS here until one of the two fires
   │
   ├── result.Decision = match decision.Outcome {
   │       "approve" → "approved"
   │       "reject"  → "rejected"
   │       _         → "expired"
   │   }
   │
   └── ActivityOptions{ StartToClose=30s, Retry: 1s→10s, attempts=3 }
       ExecuteActivity(EmitAuditEvent, AuditEvent{
           occurred_at  = workflow.Now,
           tenant_id, actor=approver, action="approval.<decision>",
           resource_type="approval_request", resource_id=request_id,
           audit_correlation_id = workflow.GetInfo.WorkflowExecution.ID,
           attributes = { subject, approver_set },
       })
       └── On error: log warning + return (do NOT fail the workflow —
           the decision is already durable in Temporal history;
           a downstream reconciler can replay missing audit events).
```

| Field | Value |
|---|---|
| Inputs (`ApprovalRequestInput`) | `request_id`, `tenant_id`, `subject`, `approver_set` (free-form `[]string`), `action_payload` (`map[string]any`) |
| Outputs (`ApprovalResult`) | `request_id`, `decision` (`approved`/`rejected`/`expired`), `approver?` |
| Search attributes | `audit_correlation_id` (auto-generated UUIDv7 if not supplied — see [`temporal_adapter.rs:73`](../../../services/approvals-service/src/domain/temporal_adapter.rs)) |
| Signals | **`decide`** — payload `{outcome: "approve"|"reject", approver, comment?}` ([contract.go:22-26](../../../workers-go/approvals/internal/contract/contract.go)). |
| Queries | **None.** No `workflow.SetQueryHandler`. |
| Child workflows | **None.** Single-workflow flow. |
| Compensation | **None.** Idempotent decision + best-effort audit emit. |
| Timer reset | Not implemented — once the 24h timer is set, it runs to completion. No "extend deadline" signal. |
| Continue-as-new | **None.** Each approval is a single bounded execution. |

**Hard-coded 24h timeout** — `workflow.NewTimer(ctx, 24*time.Hour)`
([approval_request.go:40](../../../workers-go/approvals/workflows/approval_request.go)).
The code-comment explicitly flags this as needing per-tenant override
("should come from policy in a follow-up PR (S2.5.b allows per-tenant
overrides)"); FASE 7 / Tarea 7.2's `expires_at TIMESTAMPTZ` column
finally moves the deadline into the row.

---

## 3. Activity inventory

There is exactly **one activity** registered on this task queue.

### 3.1 `EmitAuditEvent` → `audit-compliance-service`

Source: [`activities/activities.go`](../../../workers-go/approvals/activities/activities.go)

| Aspect | Detail |
|---|---|
| Target service | `audit-compliance-service` (`OF_AUDIT_COMPLIANCE_URL`, default `:50115`) |
| Endpoint called | `POST /api/v1/audit/events` |
| Headers | `authorization: Bearer …` (optional), `x-audit-correlation-id: <workflow-id>`, `content-type: application/json` |
| Input | `AuditEvent{occurred_at, tenant_id, actor, action, resource_type, resource_id, audit_correlation_id, attributes}` |
| Output | _(unit; the activity logs the response and returns `nil`)_ |
| Failure modes | 4xx (except 429) → `nonRetryable`; 5xx + 429 → retry within the 3-attempt envelope. The workflow body **swallows** activity errors so a downstream reconciler can replay missing audit events from Temporal history. |
| Side effects | None on the worker side. The downstream service writes the row to its append-only `audit_events` ledger. |

There are **no other activities** registered on this task queue.
The plan's mentions of "escalation activities" / "notification fan-out"
do not exist in code.

---

## 4. How `ApprovalRequestWorkflow` is triggered (and decided)

There are **two distinct producer paths** that converge on
`ApprovalsClient` (the typed Rust wrapper around the workflow), each
landing on a different verb.

### 4.1 Path A — open / start

| Surface | Rust adapter | Effect |
|---|---|---|
| `POST /api/v1/approvals` on `approvals-service` ([handlers/approvals.rs:21-47](../../../services/approvals-service/src/handlers/approvals.rs)) | `ApprovalsAdapter::open_approval` ([temporal_adapter.rs:72-82](../../../services/approvals-service/src/domain/temporal_adapter.rs)) | `ApprovalsClient::open(request_id, ApprovalRequestInput, audit_correlation_id)` → Temporal `start_workflow_execution` on `openfoundry.approvals` task queue → `ApprovalRequestWorkflow(input)` |

There is **no other producer of approval starts in tree** — a `grep` for `ApprovalsClient::open` returns only this one site.

### 4.2 Path B — decide (signal)

| # | Surface | Rust adapter | Effect |
|---|---|---|---|
| 1 | `POST /api/v1/approvals/{id}/decide` on `approvals-service` ([handlers/approvals.rs:71-103](../../../services/approvals-service/src/handlers/approvals.rs)) | `ApprovalsAdapter::decide_approval` ([temporal_adapter.rs:84-86](../../../services/approvals-service/src/domain/temporal_adapter.rs)) | `ApprovalsClient::decide(request_id, ApprovalDecision)` → Temporal `signal_workflow_execution(decide)` |
| 2 | `POST /api/v1/workflows/approvals/{approval_id}/continue` on **`workflow-automation-service`** ([workflow-automation-service handlers/approvals.rs:12-43](../../../services/workflow-automation-service/src/handlers/approvals.rs)) | Inline `ApprovalsClient::new(...).decide(...)` — **NO adapter wrapper**, the handler builds the client itself | Same Temporal signal as above. |

So the cross-service entanglement is: **`workflow-automation-service`
holds a Temporal `WorkflowClient` purely so it can signal
`approvals-service`'s workflows**. This is the only reason
`workflow-automation-service` carries a `temporal-client` dep after
FASE 5 ([workflow-automation-service main.rs:35-37](../../../services/workflow-automation-service/src/main.rs)).
FASE 7 is what unblocks dropping that dep (Tarea 8.3).

### 4.3 UI surfaces

The web app has two distinct decision endpoints in
[`apps/web/src/lib/api/workflows.ts`](../../../apps/web/src/lib/api/workflows.ts):

* `POST /workflows/approvals/{id}/decision` — calls
  `workflow-automation-service` (path B, row 2 above).
* (Implicit) `POST /api/v1/approvals/{id}/decide` is reachable but no
  in-tree TS client uses it; the explicit `/workflows/approvals/...`
  surface is the one the SvelteKit app calls.

The migration must keep `POST /workflows/approvals/{id}/decision`
working (UI calls it). Tarea 7.3 strategy: convert that handler from
a Temporal signal into an HTTP call to
`approvals-service POST /api/v1/approvals/{id}/decide`.

---

## 5. Approval-type taxonomy — plan vs. code

The migration plan §7.1 lists four approval kinds:

* **Single approver** — one person decides.
* **Multi-approver** — every member of `approver_set` must approve.
* **Threshold-based** — N of M must approve.
* **Time-based escalation** — after a deadline, escalate to a wider
  approver set.

Today's code implements **only single-approver**. The evidence:

* `ApprovalRequestInput.approver_set` is an opaque `[]string`
  ([contract.go:13-18](../../../workers-go/approvals/internal/contract/contract.go))
  — the workflow body never iterates it, never counts approvals, and
  never re-emits the signal channel after one decision.
* `selector.Select(ctx)` (single call, no loop) means the FIRST
  `decide` signal terminates the workflow regardless of how many
  approvers were nominated.
* `Activities.EmitAuditEvent.attributes.approver_set` is just metadata
  passed through to the audit row — no quorum semantics.
* No "escalation" code path exists. The 24h timer leads directly to
  `expired`; there is no fan-out to a wider approver set.

The Rust adapter's `OpenApprovalRequest::approver_set` is also free-
form. The HTTP handler in `approvals-service::create_approval`
populates it with `[assigned_to]` (a single user) at most
([handlers/approvals.rs:155-166](../../../services/approvals-service/src/handlers/approvals.rs)),
confirming the single-approver pattern in practice.

**Implication for FASE 7**:

* Tarea 7.2's state machine columns can be designed for single-approver
  semantics today and accept multi-approver semantics in a follow-up
  (introduce a `quorum` JSON column with `{required: N, decisions: [...]}`
  or similar). Don't bake threshold logic into the schema yet.
* "Time-based escalation" is the migration plan's `Escalated` state
  enum value. Today it has no producer; reserve it in the enum so the
  schema supports the future flow without another migration when it
  ships.

---

## 6. Where state lives today

| Surface | Today |
|---|---|
| In-flight approval state | **Temporal workflow event history** (per `ApprovalRequestWorkflow` execution). |
| Decided approval audit | `audit-compliance-service::audit_events` row written by the `EmitAuditEvent` activity. |
| Read-side projection | **None implemented.** `approvals-service::list_approvals` returns `{"data": [], "source": "temporal"}` ([handlers/approvals.rs:49-69](../../../services/approvals-service/src/handlers/approvals.rs)). |
| Legacy SQL tables | Dropped. The `approvals-service` k8s README describes the cluster as "non-authoritative projection" only. |

So the live truth is in Temporal and only there. **A FASE 7 migration
that retires Temporal must establish the entire state plane** — Tarea
7.2 introduces `pg-policy.audit_compliance.approval_requests` as the
new source of truth (the audit ledger stays untouched).

---

## 7. Migration table — Temporal pieces → State machine + cron targets

Aligned with FASE 7 of the migration plan and ADR-0022 (outbox) +
ADR-0037 (Foundry-pattern). Each row is a discrete piece of behaviour
observable in `workers-go/approvals/` (or in the adjacent Rust adapter
that funnels into it) today; the right-hand column is the post-
migration owner.

| # | Today (Temporal) | Tomorrow (State machine + cron) | Notes / target |
|---|---|---|---|
| 1 | `ApprovalRequestWorkflow` registered on `openfoundry.approvals` | **Removed.** Each approval is a row in `audit_compliance.approval_requests` driven by the `state_machine::PgStore` helper. | Tarea 7.2 schema. |
| 2 | Selector blocking on `decide` signal + 24h timer | **`state` column** with `CHECK ('pending', 'approved', 'rejected', 'expired', 'escalated')` + `expires_at TIMESTAMPTZ` for the deadline. The "block" disappears — the row just sits in `pending` until a decision row update or the cron sweep fires. | Mirror of FASE 5 / `automation_runs`. |
| 3 | `decide` signal payload (`{outcome, approver, comment?}`) | **`POST /api/v1/approvals/{id}/decide` on `approvals-service`** — synchronous state transition `pending → approved/rejected`, idempotent on second call, atomic with outbox publish of `approval.completed.v1`. | HTTP wins; signal goes away. |
| 4 | 24h hard timeout (Temporal timer) | **k8s `CronJob` running every 5 min** invoking `state_machine::PgStore::timeout_sweep` — every row with `expires_at <= now() AND state = 'pending'` transitions to `expired`. The CronJob mirrors the existing `schedules-tick` binary pattern from FASE 3 / Tarea 3.5. | Tarea 7.4 deliverable. |
| 5 | Hard-coded `24*time.Hour` constant | `expires_at` set per-row at insert time from a per-tenant policy column (or the request's `expires_at` field if the caller supplied one). | Long-pending TODO in the legacy code finally lands. |
| 6 | `EmitAuditEvent` activity (one HTTP POST per terminal transition) | **Outbox event `approval.completed.v1`** — `audit-compliance-service` becomes a Kafka consumer of that topic and writes the same `audit_events` row from there. Same wire shape. | The activity goes; the audit ledger keeps the same content. |
| 7 | `audit_correlation_id` propagated via Temporal search attribute + activity HTTP header | Same UUID flows on the `approval.requested.v1` / `approval.completed.v1` event headers and onto every effect call's `x-audit-correlation-id` header. | Cross-cutting. |
| 8 | Path A — `POST /approvals` → `ApprovalsAdapter::open_approval` → Temporal start | Same HTTP API; handler INSERTs the `approval_requests` row + outbox publish of `approval.requested.v1` in the same TX. Returns 202. | Producer change inside the same handler. |
| 9 | Path B-1 — `POST /approvals/{id}/decide` → Temporal signal | Same HTTP API; handler runs `state_machine::PgStore::apply(Decide)` + outbox publish of `approval.completed.v1` in one TX. | Producer change. |
| 10 | Path B-2 — `POST /workflows/approvals/{id}/continue` on `workflow-automation-service` (signals Temporal) | Becomes a thin **HTTP proxy** to `approvals-service::POST /api/v1/approvals/{id}/decide`. The Temporal `WorkflowClient` field on `workflow-automation-service::AppState` can then be removed (unblocks FASE 8 / Tarea 8.3). | Cross-service change; small. |
| 11 | UI flow — `apps/web/.../workflows/{id}/decision` POST → workflow-automation-service | UI URL stays; only the substrate behind `workflow-automation-service` changes (HTTP proxy instead of Temporal signal). | UI does not change. |
| 12 | Workflow id `approval:{request_id}` (Temporal-side) | `approval_requests.id` (Postgres-side) — same UUID. | Same identity. |
| 13 | List + read endpoints return empty stubs | Same endpoints query `audit_compliance.approval_requests` directly and return real rows. | Read-side comes alive. |
| 14 | Kafka topic catalog already declares `approval.events.v1` | Tarea 7.2 adds the typed `.v1` topics: `approval.requested.v1`, `approval.decided.v1`, `approval.completed.v1`, `approval.expired.v1` (subset of `approval.completed.v1`, kept separate so SLO alerts can fire on it without filtering). The legacy `approval.events.v1` stays as broadcast feed. | New helm topics. |
| 15 | "Multi-approver / threshold / escalation" plan-level taxonomy | Schema reserves the `escalated` state enum value; semantic implementation deferred (no in-tree caller needs it today). | Forward-compat only. |

---

## 8. Target service structure (`approvals-service` 2.0)

Per Tarea 7.3, the service grows from "thin REST→Temporal adapter"
to a self-contained state-machine runtime. Two concurrent tokio tasks
under the same `main.rs`.

```text
                       ┌─────────────────────────────────────────────────────┐
                       │              approvals-service                       │
                       │  (one Rust binary, two concurrent tokio tasks)      │
                       │                                                       │
   user / svc-to-svc ─►│  (a) HTTP API (axum)                                  │
                       │      - POST /approvals          (open new approval)   │
                       │      - POST /approvals/{id}/decide  (approve/reject)  │
                       │      - GET  /approvals          (list)                │
                       │      - GET  /approvals/{id}     (read state)          │
                       │      └─► writes saga.state row + outbox row in TX  ──┐│
                       │                                                       ││
   approval.decided.v1 │  (b) Decision consumer (rdkafka) — OPTIONAL           ││
   (Kafka)        ─────│      Out-of-scope for FASE 7 strict; the inbound      ││
                       │      "manager decided externally" path the migration  ││
                       │      plan §7.3 mentions has no producer in tree, so   ││
                       │      this consumer is a future hook. The HTTP route   ││
                       │      above covers every in-tree producer today.       ││
                       │                                                       ││
                       └────────────────────────────────────────────────────┬──┘│
                                                                            │   │
                              outbox.events  ──── Debezium ──► Kafka ◄──────┘   │
                                                                                │
                             ┌──────────────────────────────────────────────────┘
                             ▼
            ┌────────────────────────────────────────────────────────┐
            │ approval.requested.v1 / approval.completed.v1 /        │
            │ approval.expired.v1                                     │
            │                                                         │
            │ Downstream consumers:                                   │
            │   - audit-compliance-service (writes audit_events row)  │
            │   - workflow-automation-service (resumes parent workflow│
            │     when an approval the workflow waits on completes —  │
            │     but only when the workflow-automation runtime grows │
            │     a "wait for approval" step. FASE 5 single-step      │
            │     condition consumer does not need it.)               │
            │   - notification-alerting-service (UI live feed)        │
            └────────────────────────────────────────────────────────┘

   k8s CronJob (every 5 min) ─►  approvals-timeout-sweep binary
                                   │ libs/state_machine::PgStore::timeout_sweep
                                   │ for every row pending past expires_at:
                                   │   apply Expire event → status='expired'
                                   │   outbox.events ← approval.expired.v1
                                   ▼
                              same outbox path as the HTTP handlers
```

Component cardinality:

| Component | Substrate | Notes |
|---|---|---|
| HTTP API | axum, port `:50071` (unchanged) | Same routes today; semantics flip from "Temporal signal" to "Postgres state-machine apply". |
| State machine | `state_machine::PgStore<ApprovalRequest>` over `audit_compliance.approval_requests` | Same pattern as FASE 5 `automation_runs`. |
| Outbox | Per-bounded-context `outbox.events` in pg-policy | ADR-0022 outbox-per-cluster rule. Already provisioned for `audit-compliance-service` (FASE 6 / Tarea 6.2 outbox dependency). |
| Idempotency | `audit_compliance.processed_events` | New for FASE 7. |
| Timeout sweep | k8s `CronJob` running `approvals-timeout-sweep` (new binary in `libs/state-machine/src/bin/` or under `services/approvals-service/`) every 5 min | Tarea 7.4 deliverable. |
| Decision consumer | (deferred) rdkafka consumer of `approval.decided.v1` | The inbound "manager decided externally" path the plan mentions has no in-tree producer; design the topic in Tarea 7.2 but skip the consumer wiring until a real producer exists. |

What goes away:

- `domain/temporal_adapter.rs` — gone with FASE 8.
- `temporal-client` workspace dep on `approvals-service` and on
  `workflow-automation-service` — gone with FASE 8 / Tarea 8.3 (this
  task unblocks both).
- The hard-coded 24h timer.

What stays:

- HTTP routes + JWT middleware on them.
- `domain/runtime.rs` (the apply-approval-proposal flow + JWT issuance
  helpers — entirely independent of the Temporal substrate).
- The `WorkflowApproval` model shape (UI compatibility).

---

## 9. Net call graph (today)

```text
   user (UI)  ──POST /api/v1/workflows/approvals/{id}/decision──►
                              workflow-automation-service
                                  │ ApprovalsClient::decide
                                  ▼
   user (admin) ─POST /api/v1/approvals──► approvals-service
                                              │ ApprovalsAdapter::open
                                              │  → ApprovalsClient::open
                                              ▼
                                    Temporal frontend (gRPC)
                                    ns=default, tq=of.approvals
                                              │ start_workflow / signal_workflow
                                              ▼
                                ┌──────────────────────────────────┐
                                │ workers-go/approvals worker      │
                                │   ApprovalRequestWorkflow        │
                                │   ├── selector { decide signal,  │
                                │   │              24h timer       │
                                │   │            }                  │
                                │   └── EmitAuditEvent activity    │
                                └──────────────────────────────────┘
                                              │ HTTP POST /api/v1/audit/events
                                              ▼
                                ┌──────────────────────────────────┐
                                │ audit-compliance-service         │
                                │ writes audit_events row          │
                                └──────────────────────────────────┘
   user (admin) ─POST /api/v1/approvals/{id}/decide──► approvals-service
                                                          │ ApprovalsAdapter::decide
                                                          │  → ApprovalsClient::decide
                                                          ▼
                                          (joins the Temporal signal path above)
```

## 10. Net call graph (post-migration target, for orientation only)

```text
   user (UI)  ──POST /workflows/approvals/{id}/decision──► workflow-automation-service
                              │ HTTP proxy (no more Temporal client)
                              ▼
                         approvals-service POST /api/v1/approvals/{id}/decide
                              │   1. UPDATE audit_compliance.approval_requests
                              │      via state_machine::PgStore::apply
                              │      (Decide event)
                              │   2. INSERT outbox.events
                              │      (approval.completed.v1)
                              │   3. return 202
                              │
                          Debezium ─► approval.completed.v1 ─► audit-compliance-service
                                                              (writes audit_events row)
                                                          notification-alerting-service
                                                              (UI live feed)
                                                          workflow-automation-service
                                                              (future: resume waiting
                                                               saga step)

   user (admin) ─POST /api/v1/approvals──► approvals-service
                                              │   1. INSERT approval_requests row
                                              │      (state=pending, expires_at=...)
                                              │   2. INSERT outbox.events
                                              │      (approval.requested.v1)
                                              │   3. return 202
                                              ▼
                          Debezium ─► approval.requested.v1 ─► audit-compliance-service

   k8s CronJob (every 5 min) ─► approvals-timeout-sweep
                                  │   for row in saga.state where state='pending'
                                  │       and expires_at <= now():
                                  │       apply(Timeout) → state='expired'
                                  │       outbox ← approval.expired.v1
                                  ▼
                          Debezium ─► approval.expired.v1 ─► audit-compliance-service
                                                          notification-alerting-service
                                                          workflow-automation-service
```

---

## 11. Risks / non-functional notes

* **Cross-service entanglement is the gating concern**. After FASE 7,
  `workflow-automation-service::handlers/approvals.rs::continue_after_approval`
  becomes a thin HTTP proxy. That's a small change but it is on a
  hot path the UI uses; verify behaviour parity with a request /
  response contract test in Tarea 7.3.
* **Multi-approver / threshold / escalation are not in scope**. The
  plan lists them; the code implements none. The Tarea 7.2 schema
  must reserve enum values + a `quorum` JSONB column slot but should
  not implement quorum logic — that lands in a follow-up when a real
  caller asks for it. Documenting this gap explicitly here keeps
  Tarea 7.3 scoped.
* **24h timeout is hard-coded today**. Per-tenant overrides are
  flagged as a TODO in the worker code-comment. Tarea 7.2's
  `expires_at TIMESTAMPTZ` lets the per-tenant policy be applied at
  insert time; the migration is the right moment to introduce it.
* **`audit-compliance-service` becomes a Kafka consumer**. Today it
  writes `audit_events` from a synchronous HTTP POST issued by the
  worker activity. Post-FASE-7 it must also consume
  `approval.completed.v1` (or its consumer must call the existing
  HTTP endpoint internally). Choose one substrate — the cleanest is
  to make the HTTP endpoint the single ingest point and have the
  Kafka consumer call it locally; that avoids a second write path.
* **`audit_compliance.processed_events` collision**. The schema name
  is shared with the existing `audit_events` table — namespace the
  new dedup table inside `audit_compliance` schema explicitly so
  there is no ambiguity.
* **Decision consumer (Kafka) is deferred**. The migration plan §7.3
  mentions a Kafka consumer for "manager decided" events. There's no
  in-tree producer of such an event. Design the topic
  (`approval.decided.v1`) in Tarea 7.2 so it exists for future use,
  but don't wire the consumer in Tarea 7.3 — it's dead code without
  a producer.
* **The legacy `approval.events.v1` topic** (already in helm) is a
  broadcast feed. Tarea 7.2 keeps it for backwards compatibility
  with whatever offline analytics pipeline reads it; the typed `.v1`
  topics are the new substrate the runtime uses.
* **`approver_set` validation** — today's adapter accepts any
  `[]string`. Tarea 7.2 should validate the format on insert so a
  future quorum/escalation feature has a stable input shape to read
  from.

---

## 12. Acceptance for Tarea 7.1

* This document exists at `docs/architecture/refactor/approvals-worker-inventory.md`.
* §2 documents the single workflow with its full input / output /
  signal / timer / Temporal-feature footprint (first worker in the
  codebase that uses signals + timers).
* §3 documents the single activity (`EmitAuditEvent`) with its full
  HTTP contract.
* §4 lists every producer path (Path A — `open`; Path B-1 — UI on
  approvals-service; Path B-2 — UI on workflow-automation-service)
  and explains the cross-service entanglement that motivates the
  FASE 8 / Tarea 8.3 cleanup.
* §5 catalogues the approval-type taxonomy gap between the plan
  ("single, multi-approver, threshold-based, time-based escalation")
  and the code (only single-approver). Flagged as out-of-scope for
  FASE 7 implementation but reserved in the schema enum.
* §7 maps every Temporal primitive in use today to its state-machine
  + cron replacement, ready to drive Tareas 7.2 (schema), 7.3
  (service refactor), 7.4 (CronJob sweeper) and 7.5 (worker
  deletion).
* §8 sketches the post-migration `approvals-service` 2.0 shape so
  Tarea 7.3 has a starting blueprint.
