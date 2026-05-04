# Foundry-pattern orchestration

> **Canonical reference for the post-Temporal substrate.** This
> document is the architectural ground truth for how OpenFoundry
> services orchestrate work after FASE 0–10 of the migration plan.
> ADRs and READMEs link here when they need a concrete answer to
> "where does state live, how do retries / compensations work, how
> do I add a new workflow."
>
> Companion documents:
>
> - [ADR-0037 — Foundry-pattern orchestration](adr/ADR-0037-foundry-pattern-orchestration.md) — the supersession decision.
> - [ADR-0038 — Event contract + idempotency](adr/ADR-0038-event-contract-and-idempotency.md) — wire format, deterministic UUIDv5 derivation, record-before-process invariant.
> - [ADR-0022 — Transactional outbox + Debezium](adr/ADR-0022-transactional-outbox-postgres-debezium.md) — the outbox / Debezium / EventRouter SMT contract.
> - [Migration plan](migration-plan-foundry-pattern-orchestration.md) — per-task implementation log.
> - [ADR-0021 — Temporal on Cassandra](adr/ADR-0021-temporal-on-cassandra-go-workers.md) — superseded predecessor.

## 1. The five orchestration patterns

OpenFoundry deliberately collapses every orchestration need into
**five named patterns**. Adding a new workflow always means picking
one of the five, not inventing a sixth. Each pattern has a single
canonical implementation under `services/`; new code reuses that
shape rather than rolling its own scheduler.

| # | Pattern | Use case | Canonical implementation | Substrate |
|---|---|---|---|---|
| 1 | **Pipeline batch** | Long-running batch transforms (Iceberg writes, large scans, compute-intensive Python / Spark / SQL nodes) | [`services/pipeline-build-service`](../../services/pipeline-build-service) submitting [`services/pipeline-runner`](../../services/pipeline-runner) SparkApplication CRs | Spark Operator + Iceberg + per-node `pipeline_runs` state in `pg-runtime-config.pipeline_authoring` |
| 2 | **Reindex / backfill** | Pull every row of a Cassandra keyspace into Kafka without starving the live consumer group | [`services/reindex-coordinator-service`](../../services/reindex-coordinator-service) | Kafka `ontology.reindex.requested.v1` + `pg-runtime-config.reindex_jobs.resume_token` cursor + Cassandra paged scan |
| 3 | **Automate (single-step condition → effect)** | "When X happens, call Y" — the Foundry "Automate" primitive (cron / event-driven action invocation) | [`services/workflow-automation-service`](../../services/workflow-automation-service) | Kafka `automate.condition.v1` consumer + `workflow_automation.automation_runs` state machine + `automate.outcome.v1` outbox publish |
| 4 | **Saga (multi-step with compensation)** | Multi-step business flow where step N's failure must roll back step N-1, N-2, … in LIFO order (cleanup, retention, dependency-aware operations) | [`services/automation-operations-service`](../../services/automation-operations-service) using `libs/saga::SagaRunner` | Kafka `saga.step.requested.v1` consumer + `saga.state` table per service + `saga.step.{completed,failed,compensated}.v1` outbox events |
| 5 | **Approval (long-lived human-in-the-loop)** | Wait for a human (or set of humans) to decide; deadline → expire | [`services/approvals-service`](../../services/approvals-service) + `approvals-timeout-sweep` k8s CronJob | `audit_compliance.approval_requests` state machine + `approval.{requested,completed,expired}.v1` outbox events |

The five patterns map one-for-one onto the five Go Temporal workers
the migration retired: `pipeline`, `reindex`, `workflow-automation`,
`automation-ops`, `approvals` — pattern 1, 2, 3, 4, 5 respectively.

If the work you have to do does **not** fit one of these patterns,
that is a signal to reconsider the design before reaching for a new
substrate. The substrate count was deliberately reduced from one
(Temporal) to five named patterns to maximise reuse of `libs/saga`,
`libs/state-machine`, `libs/outbox`, `libs/idempotency`, and
`libs/event-bus-data` — adding a sixth substrate erodes that.

## 2. Conventions

Every Foundry-pattern implementation follows the same set of
conventions. Treat them as load-bearing — services that deviate
break the assumptions downstream consumers (audit, lineage, UI,
notifications) make about events.

### 2.1 Deterministic `event_id` (UUIDv5)

Every outbox event carries a deterministic `event_id` derived from
the aggregate identity + the event kind. The pattern is:

```rust
pub fn derive_outbox_event_id(aggregate_id: Uuid, kind: &str) -> Uuid {
    let mut buf = Vec::with_capacity(17 + kind.len());
    buf.extend_from_slice(aggregate_id.as_bytes());
    buf.push(b'|');
    buf.extend_from_slice(kind.as_bytes());
    Uuid::new_v5(&SERVICE_NAMESPACE, &buf)
}
```

`SERVICE_NAMESPACE` is a hard-coded `Uuid::from_bytes([…])` constant
generated once with `uuidgen` and pinned forever in the service's
`event.rs`. The same `(aggregate_id, kind)` pair always produces
the same `event_id`, which is what makes the outbox helper's
`INSERT … ON CONFLICT DO NOTHING` collapse retries onto one row.

Reference implementations:

- `services/approvals-service::event::derive_outbox_event_id`
- `services/automation-operations-service::event::derive_request_event_id`
- `services/workflow-automation-service::event::derive_event_id_for_run`
- `services/reindex-coordinator-service::event::derive_request_event_id`

### 2.2 Record-before-process idempotency

Every Kafka consumer that does anything side-effecting (HTTP calls,
state-machine writes, DB updates other than the dedup row itself)
records the inbound event's `event_id` in
`<bounded_context>.processed_events` **before** dispatching the
side effect. The `libs/idempotency::PgIdempotencyStore` is the
canonical helper:

```rust
match self.idempotency.check_and_record(event_id).await? {
    Outcome::AlreadyProcessed => {
        // Skip — another delivery already did the work.
        return Ok("deduped");
    }
    Outcome::FirstSeen => {}
}
// proceed with side effect...
```

`processed_events` is a single-column `event_id uuid PRIMARY KEY`
table. Each service owns its own copy in its bounded-context
schema. A row in the table means "this event_id has been claimed by
*some* consumer instance for processing; never run the side effect
for it again." A consumer that crashes between recording and
finishing the work leaves the side effect partially done; the next
delivery short-circuits at the dedup check, so the side effect runs
**at most once** even though the message itself is delivered
at-least-once. Saga / state machine / approval flows then make the
"completed half" observable through their own row state.

Tradeoff: a consumer that fails after recording the event_id but
before completing the work leaves the saga in an in-flight state
that only operator action can resolve. This is the explicit
trade-off ADR-0038 §3 spells out — never silently replay a
half-applied side effect.

### 2.3 Outbox + Debezium publishes

Domain events go to Kafka via the per-service `outbox.events` table
(captured by Debezium's EventRouter SMT), never via direct Kafka
producer calls inside the same transaction as a state write.

```rust
let mut tx = pool.begin().await?;

// 1. Apply the state-machine transition.
state_machine_apply(&mut tx, ...).await?;

// 2. Enqueue the outbox event in the SAME transaction.
let event = OutboxEvent::new(event_id, "automation_run", run_id, topic, payload)
    .with_header("x-audit-correlation-id", correlation_id);
outbox::enqueue(&mut tx, event).await?;

// 3. Commit. The Postgres WAL now carries both writes; Debezium
//    will publish the outbox row to Kafka.
tx.commit().await?;
```

The invariant is that the state-machine write and the matching
outbox event commit atomically. A consumer downstream sees
"row in terminal state" if and only if "outbox event is enqueued",
which Debezium will eventually deliver.

Reference: `libs/outbox::enqueue` is the canonical helper. It
INSERTs and immediately DELETEs the row in the same transaction —
the EventRouter SMT picks the INSERT off the WAL even though the
row never lands in the table at steady state. Consequence: the
table is steady-state empty on a healthy cluster.

### 2.4 Retry / backoff envelopes

Effect dispatchers (HTTP calls into other services from a saga
step or a Foundry-pattern condition consumer) use **explicit
retry envelopes**, not Temporal-style automatic activity retries.
The pattern is `tokio::time::sleep` + a max-attempts counter:

```rust
const MAX_ATTEMPTS: u32 = 5;
let mut delay = Duration::from_secs(1);

for attempt in 1..=MAX_ATTEMPTS {
    match effect_dispatcher.call(&request).await {
        Ok(response) => return Ok(response),
        Err(err) if !err.is_retryable() => return Err(err),
        Err(err) if attempt == MAX_ATTEMPTS => {
            return Err(EffectDispatchError::Exhausted { attempts: attempt, source: err });
        }
        Err(_) => {
            tokio::time::sleep(delay).await;
            delay = (delay * 2).min(Duration::from_secs(60));
        }
    }
}
```

4xx (other than 429) is **non-retryable** — the request is
malformed and re-issuing won't help. 5xx and 429 are retryable
within the envelope. After exhaustion the saga / automation run
lands in `failed`; idempotency at the `processed_events` table
prevents the retry attempts from re-charging on Kafka redelivery.

Reference implementation:
[`services/workflow-automation-service::domain::effect_dispatcher`](../../services/workflow-automation-service/src/domain/effect_dispatcher.rs).

### 2.5 State machine wrapping (`libs/state-machine::PgStore`)

Every multi-state aggregate persisted in a Postgres row uses the
`libs/state-machine` helper rather than hand-rolled `UPDATE`
statements. The helper provides:

- Optimistic concurrency via a `version BIGINT` column bumped on
  every apply — two callers racing on the same row produce at
  most one successful UPDATE, the loser sees `StoreError::Stale`
  and reloads.
- A `state TEXT` column that stays in sync with the JSON `state_data`
  blob via the `StateMachine::state_str` mapping.
- A `timeout_sweep(now)` helper for cron-driven `Pending → Expired`
  transitions (used by `approvals-timeout-sweep`).

Reference table shape lives in
[`libs/state-machine/migrations/0001_state_machine_template.sql`](../../libs/state-machine/migrations/0001_state_machine_template.sql).
Each consuming service ships a migration that mirrors the
template + adds projected columns the read-side query needs.

### 2.6 Saga orchestration (`libs/saga::SagaRunner`)

Multi-step flows with compensation use the saga helper instead of
chained state machines. The runner:

- Persists progress to `saga.state` after each step (idempotent
  retry: a re-driven saga with the same `saga_id` reads back
  `completed_steps` and short-circuits the already-finished
  prefix).
- Emits one `saga.step.completed.v1` event per successful step
  via outbox.
- On step failure, runs every previously-completed step's
  `compensate` body in **LIFO order**, emitting one
  `saga.step.compensated.v1` per success.
- Final terminal status is `completed`, `failed`, or
  `compensated` (the last when at least one compensation ran);
  emits `saga.completed.v1` or `saga.aborted.v1` accordingly.

The chaos test at
[`services/automation-operations-service/tests/saga_chaos.rs`](../../services/automation-operations-service/tests/saga_chaos.rs)
is the executable specification of this contract — forces step 2
of a 3-step saga to fail and asserts the outbox emission order
+ final `saga.state.status='compensated'`.

### 2.7 Search-attribute / correlation propagation

Every Foundry-pattern event carries an `x-audit-correlation-id`
header that propagates from the inbound HTTP request span all the
way to every downstream effect call's HTTP header. The Rust
helpers (`outbox::enqueue` + every reference effect dispatcher) do
this automatically when the consumer constructs the outbox event
with the appropriate header. Audit consumers stitch a single
flow together by `correlation_id` rather than by Kafka offsets.

### 2.8 Topic naming convention

All event topics use `<domain>.<entity>.v<N>`. Versioning is
**explicit** — never republish on the same topic with a wire-shape
change; introduce `v2` and run the consumer dual-shape until every
producer migrates. Current `v1` topics:

- `pipeline.run.requested.v1`, `pipeline.builds.v1`
- `ontology.reindex.requested.v1`, `ontology.reindex.v1`, `ontology.reindex.completed.v1`
- `automate.condition.v1`, `automate.outcome.v1`
- `saga.step.requested.v1`, `saga.step.completed.v1`, `saga.step.failed.v1`, `saga.step.compensated.v1`, `saga.completed.v1`, `saga.aborted.v1`, `saga.compensate.v1`
- `approval.requested.v1`, `approval.completed.v1`, `approval.expired.v1`, `approval.decided.v1`

DLQ topics mirror the production topic name with a `__dlq.` prefix
and 14-day retention.

## 3. How to add a new workflow

Pick a pattern from §1 first. The decision tree:

```text
Is the work batch (single deterministic transform, can take >1 min)?
  → Pattern 1 (pipeline). Add a `transform_type` to
    pipeline-build-service::domain::engine; ship a SparkApplication
    template if Spark-runnable.

Is it a one-shot full-keyspace scan (push every record through Kafka)?
  → Pattern 2 (reindex). Probably means extending
    reindex-coordinator-service with a new request topic + a new
    target keyspace; the cursor/throttle mechanics stay.

Is it "when condition X fires, call HTTP endpoint Y"?
  → Pattern 3 (automate). Add an entry to the
    workflow-automation-service consumer's effect dispatcher; the
    condition consumer + outbox publishing are already wired.

Is it "a sequence of steps, where step N's failure must roll back
N-1, N-2, … in LIFO order"?
  → Pattern 4 (saga). Implement `libs/saga::SagaStep` for each step
    + a step graph in `automation-operations-service::domain::dispatcher`.
    Compensation MUST be safe to invoke after a successful execute.

Does it wait for a human (or set of humans) to decide, with a
deadline?
  → Pattern 5 (approval). Use approvals-service's HTTP API; the
    state machine, the outbox, and the timeout-sweep CronJob are
    already wired.
```

For any of the five patterns, the code-shape is roughly:

1. Define the wire format in the service's `src/event.rs` —
   typed structs with serde `#[derive]`s and round-trip tests.
2. Add the topic constants to `src/topics.rs` with a
   `topic_constants_match_helm_provisioning` test.
3. Declare the topic + DLQ in
   [`infra/helm/infra/kafka-cluster/values.yaml`](../../infra/helm/infra/kafka-cluster/values.yaml).
4. Migrate the state-machine table or saga schema (per
   §2.5 / §2.6).
5. Wire the consumer / handler / cron in `src/main.rs` (or
   `src/bin/<entrypoint>.rs`).
6. Ship a chaos test for the failure path (mirror the saga chaos
   test from `automation-operations-service`).

Anti-patterns to reject in code review:

- **Direct Kafka producer calls inside an HTTP handler that also
  writes a state-machine row.** Always go through `outbox::enqueue`
  in the same transaction; the direct producer breaks the atomicity
  invariant of §2.3.
- **Hand-rolled `UPDATE state SET status = 'completed' WHERE id =
  …` without a `version` guard.** Use `libs/state-machine::PgStore`
  or an equivalent optimistic-concurrency UPDATE with `version`
  columns.
- **`tokio::spawn` of fire-and-forget side-effecting tasks from
  inside a handler.** The detached future has no idempotency
  story; route through outbox + a consumer instead.
- **`task_type` (or equivalent dispatch key) as a free-form string
  with no enum / match arm.** Every Foundry-pattern dispatcher has
  a compile-time match on the dispatch key; an unknown value lands
  the work in `failed` rather than silently no-op.

## 4. Comparison with Palantir Foundry

The five patterns map onto Foundry's three orchestration
primitives + two extensions. The mapping is informational — we did
not set out to clone Foundry, but the substrate names give
operators familiar with Foundry a quick mental model.

| OpenFoundry pattern | Foundry primitive | Notes |
|---|---|---|
| 1 — Pipeline batch | **Builds** | Foundry runs builds as Spark jobs against managed datasets. Our `pipeline-runner` SparkApplication CRs are the operational equivalent; both eventually emit Iceberg snapshots. We additionally surface a per-node DAG so non-Spark transforms can run inline (Rust SQL via DataFusion, Python via compute-modules-runtime). |
| 2 — Reindex | _(no direct Foundry analogue)_ | Foundry indexes datasets implicitly through its catalog refresh. Our equivalent is a separate Kafka feed because the OpenFoundry ontology object store is Cassandra (not Iceberg) and needs an explicit pull-and-publish path that does not starve the live `ontology.object.changed.v1` consumer. |
| 3 — Automate | **Automate** | Foundry's Automate fires actions on cron + on dataset changes. Our `automate.condition.v1` topic carries both shapes; the consumer dispatches to the appropriate ontology action. The wire-format is closer to AWS EventBridge than to Foundry's UI-driven action graph, but the conceptual fit is exact. |
| 4 — Saga | **Functions** + workflow chaining | Foundry chains Functions implicitly through its dataset graph + Automate triggers. We need an explicit saga because some flows (`cleanup.workspace`, future `retention.sweep`) span services that do not share dataset lineage; the LIFO compensation contract is what gives us the rollback Foundry's approach implicitly skips. |
| 5 — Approval | **Approvals** | Foundry's approvals are a built-in workflow primitive; ours is the same idea (state machine + deadline) implemented as a Postgres row + CronJob sweep. The Foundry approvals UI is rougher — we keep the simple shape on purpose. |

The two patterns OpenFoundry has that Foundry does not are **Reindex**
(driven by the Cassandra-vs-Iceberg substrate split) and the explicit
**Saga** with LIFO compensation (driven by our cross-service
business processes that do not share dataset lineage). Everything
else is a direct equivalent, deliberately kept similar so an operator
moving from a Foundry deployment finds the mental model intact.

## 5. Operational invariants

A short list of things that **must** hold across every
Foundry-pattern implementation. Audit / SRE / on-call playbooks
assume these without re-checking:

1. **Outbox is steady-state empty.** Any row in `outbox.events`
   for more than a few seconds is either a Debezium outage or a
   producer bug. Both
   [`infra/helm/apps/of-platform/values.yaml::approvalsTimeoutSweep`](../../infra/helm/apps/of-platform/values.yaml)
   and the existing per-service Prometheus rules alert on this.
2. **Every event has a deterministic `event_id`.** Operators
   replaying a Kafka topic for forensic purposes can trust that
   re-publishing yields no duplicate side effects.
3. **State-machine and outbox writes commit atomically.** No
   service ever has a "row in completed but no completion event
   was published" state.
4. **Compensations are LIFO.** An operator inspecting the audit
   ledger sees the rollback events in reverse order of the
   forward steps, every time.
5. **Saga `Escalated` is reserved.** No code emits the state
   today; it exists in the schema CHECK constraint so the
   future time-based-escalation pattern can land without another
   migration.

## 6. Where to look first when something breaks

| Symptom | First place to look |
|---|---|
| A pipeline run never finishes | `pipeline-build-service` engine logs + `pipeline_runs` row state. SparkApplication CR status if the run dispatched Spark. |
| A reindex job stops mid-keyspace | `reindex-coordinator-service` logs + `pg-runtime-config.reindex_jobs.resume_token`. The cursor advances after every successful page publish. |
| An automation run fired but the effect never landed | `workflow-automation-service` consumer logs + `automation_runs.status`. Check the effect dispatcher's retry-envelope log entries for the failure pattern. |
| A saga ended in `compensated` (not `completed`) | `saga.state.failed_step` for the step that broke the chain + the matching `saga.step.failed.v1` event in the audit ledger (`audit_compliance.saga_audit_log`). Compensation events are in the same ledger. |
| An approval expired without anyone clicking | `approvals-timeout-sweep` CronJob job logs (~5 min cadence) + `approval_requests.expires_at` value vs the timestamp on the `approval.expired.v1` event. |
| Cross-service trace stitching fails | `correlation_id` propagation gap. Every Foundry-pattern event carries `x-audit-correlation-id`; missing it is always a producer bug. |

## 7. Test harness

Two integration test surfaces cover the runtime substrate:

- **`libs/saga` + `libs/state-machine` + `libs/outbox` + `libs/idempotency`**
  each ship `it-postgres`-feature integration tests that boot a
  Postgres testcontainer, apply the migration, and round-trip the
  primitive end-to-end.
- **`services/automation-operations-service::saga_chaos`** is the
  full-stack chaos test: forces step 2 of a 3-step saga to fail
  and verifies the outbox emission order + the final
  `saga.state.status = 'compensated'`. CI runs both surfaces via
  the [`integration-foundry-pattern.yml`](../../.github/workflows/integration-foundry-pattern.yml)
  workflow on every PR that touches the substrate.

Spark-side end-to-end (real SparkApplication submission against a
kind cluster + Spark Operator) is FASE 11 — the unit-level
coverage of the Spark-submit path lives in
`pipeline-build-service`'s existing tests and is sufficient for
PR-time CI.

## 8. Glossary

* **Aggregate id** — the `id` of a state-machine row or a saga
  instance; used as the partition key for outbox events and as
  one of the inputs to the deterministic `event_id`.
* **Bounded context** — a service's owning Postgres schema
  (e.g. `audit_compliance`, `automation_operations`,
  `workflow_automation`). Each owns its own
  `outbox.events` table and `processed_events` dedup table.
* **DLQ** — `__dlq.<topic>`. Receives messages a consumer
  cannot process after N redeliveries. 14-day retention.
* **Effect** — the side-effecting HTTP call a Foundry-pattern
  consumer makes. Always typed as the `EffectDispatcher` trait
  in the consumer's `src/domain/`.
* **EventRouter SMT** — Debezium's Single Message Transform that
  turns an `outbox.events` INSERT WAL record into a Kafka record
  on the `topic` field's value.
* **Outbox event** — a row written to `outbox.events` inside the
  same transaction as a domain write; surfaced on Kafka by
  Debezium.
* **Process-once-record-before-process** — the idempotency
  invariant: record the inbound `event_id` BEFORE executing the
  side effect, so a crash between record and complete still skips
  the side effect on next delivery (operator action resolves the
  half-applied state).
* **Saga** — a sequence of `libs/saga::SagaStep`s with LIFO
  compensation on failure. Each step is pure async (no DB
  access); the runner persists progress to `saga.state` between
  steps.
* **State machine** — a `libs/state-machine::PgStore`-backed row
  with explicit allowed-transition rules + `version` for
  optimistic concurrency.

## 9. Authoritative reference

When in doubt, read the source. The reference implementations
that own each pattern:

- Pattern 1: [`services/pipeline-build-service`](../../services/pipeline-build-service)
- Pattern 2: [`services/reindex-coordinator-service`](../../services/reindex-coordinator-service)
- Pattern 3: [`services/workflow-automation-service`](../../services/workflow-automation-service)
- Pattern 4: [`services/automation-operations-service`](../../services/automation-operations-service)
- Pattern 5: [`services/approvals-service`](../../services/approvals-service)

Helper crates (consumed by every pattern):

- [`libs/saga`](../../libs/saga)
- [`libs/state-machine`](../../libs/state-machine)
- [`libs/outbox`](../../libs/outbox)
- [`libs/idempotency`](../../libs/idempotency)
- [`libs/event-bus-data`](../../libs/event-bus-data)
- [`libs/event-scheduler`](../../libs/event-scheduler)

Migration plan: [`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](migration-plan-foundry-pattern-orchestration.md).
