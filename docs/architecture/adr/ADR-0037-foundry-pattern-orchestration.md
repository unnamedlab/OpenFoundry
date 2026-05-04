# ADR-0037: Foundry-pattern orchestration (replaces Temporal-on-Cassandra)

- **Status:** Accepted
- **Date:** 2026-05-04
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes:** [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md) —
  Temporal on Cassandra with business workers in Go.
- **Related ADRs:**
  - [ADR-0011](./ADR-0011-control-vs-data-bus-contract.md) — Control bus
    (NATS) vs data bus (Kafka). Foundry-pattern orchestration uses Kafka as
    the only cross-service coordination bus.
  - [ADR-0013](./ADR-0013-kafka-kraft-no-spof-policy.md) — Kafka KRaft no-SPOF
    policy. Required because Kafka becomes the orchestration substrate.
  - [ADR-0020](./ADR-0020-cassandra-as-operational-store.md) — Cassandra as
    operational store. The `temporal` and `temporal_visibility` keyspaces
    are scheduled for drop as part of this migration.
  - [ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md) —
    Transactional outbox + Debezium → Kafka. This ADR generalises that
    pattern from "domain event publishing" to **the** orchestration
    substrate (saga choreography, state-machine outcomes, schedule fan-out).
  - [ADR-0024](./ADR-0024-postgres-consolidation.md) — Postgres consolidation.
    State machines persist in service-owned Postgres schemas under CNPG.
  - [ADR-0025](./ADR-0025-eliminate-custom-scheduler.md) — Eliminate the
    in-house scheduler. Replaced now by K8s `CronJob` → Kafka event, not by
    Temporal `ScheduleClient`.
- **Implementation plan:**
  [docs/architecture/migration-plan-foundry-pattern-orchestration.md](../migration-plan-foundry-pattern-orchestration.md).

## Context

[ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md) accepted, on
2026-05-02, the introduction of a Temporal cluster (frontend / history /
matching / worker) backed by the platform Cassandra (default + visibility
keyspaces) and a fleet of five Go workers under
[`workers-go/`](../../../workers-go/) (`pipeline`, `reindex`,
`workflow-automation`, `automation-ops`, `approvals`). Three Rust services
(`pipeline-schedule-service`, `approvals-service`,
`automation-operations-service`) consume Temporal via the in-tree
`libs/temporal-client/` crate.

That decision was correct given the alternatives evaluated at the time
(custom scheduler vs. Temporal). Two facts have since become decisive:

1. **No workflow is in production use yet.** Per
   [`docs/architecture/migration-plan-cassandra-foundry-parity.md` §0](../migration-plan-cassandra-foundry-parity.md),
   the platform is pre-production. The cost of changing direction is at its
   minimum exactly now; it grows monotonically once any tenant depends on
   `WorkflowID` semantics or Temporal's history replay contract.
2. **The platform already standardised on the building blocks of a
   distributed, Foundry-style orchestration substrate** — Spark Operator
   (`infra/helm/infra/spark-operator/`), Kafka KRaft
   (`infra/helm/infra/kafka-cluster/`, ADR-0013), Debezium
   (`infra/helm/infra/debezium/`), CNPG Postgres
   (`infra/helm/infra/postgres-clusters/`, ADR-0010/0024) and the
   transactional outbox (`libs/outbox/`, ADR-0022). Temporal duplicates
   coordination that these components already provide and adds a second
   cluster (4 services + dedicated Cassandra footprint) that must be
   sized, patched and DR-planned independently.

### Pros / cons evaluated at PB scale

| Dimension | Temporal-on-Cassandra (ADR-0021) | Foundry-pattern (this ADR) |
| --------- | -------------------------------- | -------------------------- |
| Cross-service coordination | Temporal cluster (SPOF class: orchestrator) | Outbox → Debezium → Kafka (already required by ADR-0022) |
| Long-running pipelines (TB–PB) | Workflow drives Spark via activity → adds a hop and a determinism contract on the driver | `SparkApplication` CR managed by Spark Operator; status watch publishes outcome event |
| Time-based triggers | Temporal `ScheduleClient` in worker process | K8s `CronJob` → Kafka topic (auditable, declarative, no in-process tick loops) |
| Human-wait flows (approvals, N days) | Temporal timer + workflow state | Postgres state machine + `CronJob` sweep + `approval.events.v1` |
| Saga / compensation | Workflow code with `defer`-style compensation | Choreography: each step publishes outcome via outbox; failure publishes `stepN.failed`, compensations react |
| Determinism contract | **Required** in business code (footgun) | Not required; idempotency by `event_id` (UUID v5 over deterministic keys) |
| Operational footprint | Temporal (4 svc) + dedicated Cassandra keyspaces + Go worker fleet | Reuses already-provisioned Spark Operator, Kafka, Debezium, CNPG, NATS |
| Skill surface | Temporal SDK (Go + Rust client) + Cassandra ops | Kafka + Postgres + K8s CRDs (already required by every other team) |
| DR posture | Cross-region Temporal+Cassandra replication (open question in ADR-0021) | Falls under existing Kafka MirrorMaker / Postgres logical replication / Iceberg DR (ADR-0023) plans |
| Audit / observability | Temporal Web UI; weak coupling to platform audit | Each step writes to `audit_compliance` schema; per-event Prometheus counters; replay via Kafka offsets |

The decisive negatives of Temporal at our scale are: (a) a second
**SPOF-class** cluster duplicating coordination that ADR-0022 already
mandates; (b) Cassandra capacity dedicated to orchestration metadata,
opposed to the consolidation direction of ADR-0024; (c) the determinism
contract leaking into business-domain code authored by teams that do not
specialise in workflow engines.

## Decision

OpenFoundry refactors orchestration to a **distributed, Foundry-pattern**
substrate, with **no centralised orchestrator**:

- **Pipeline workloads** → `SparkApplication` Custom Resources managed by
  Spark Operator. `pipeline-build-service` creates the CR; the Operator
  drives execution; a status watcher publishes
  `pipeline.run.completed|failed` to Kafka.
- **Reindex / fan-out** → pure Kafka producer + consumer. Triggered by
  events on `ontology.changes.v1`; consumer paginates and emits to
  `ontology.reindex.v1`.
- **Workflow-automation** → Automate pattern: condition consumer on
  `automate.condition.<id>` → invokes `ontology-actions-service` →
  publishes `automate.outcome.<id>`. Run state persisted in a Postgres
  state machine.
- **Automation-ops** → saga **choreography** via the transactional outbox
  (ADR-0022). Each step persists state and publishes its outcome in the
  same DB transaction; downstream steps consume; failures publish
  `stepN.failed` and compensation consumers react.
- **Approvals** → Postgres state machine
  (`Pending → AwaitingApproval → Approved | Rejected | TimedOut`) +
  K8s `CronJob` sweep for timeouts + `approval.events.v1` topic for
  notification fan-out.
- **Time-based triggers** → K8s `CronJob` resources that publish events.
  No in-process tick loops; no Temporal `Schedule`.

Hard rules applied to every consumer/handler:

1. **Each workflow lives where the data lives** — no orchestrator-side
   state separate from the owning service's database.
2. **Outbox + Debezium + Kafka is the only cross-service coordination
   bus.** NATS remains restricted to control-plane hot paths
   (ADR-0011).
3. **Idempotency is mandatory.** Every event carries `event_id` (UUID
   v5 over deterministic keys); consumers dedupe via per-schema
   `processed_events` tables.
4. **Saga choreography, not orchestration.** No central node directs
   the flow.
5. **Compensations are explicit events.** A failure at step _n_
   publishes `stepN.failed`; compensation consumers for steps
   _n−1 … 1_ react.
6. **Per-event observability.** Each step writes audit rows under the
   `audit_compliance` schema and emits Prometheus counters.

The Temporal cluster, the `libs/temporal-client/` crate, the
`workers-go/` fleet (or its Kafka-consumer rewrite) and the
`temporal_*` Cassandra keyspaces are removed as a consequence of this
ADR. Sequencing and exact paths are tracked in the
[migration plan](../migration-plan-foundry-pattern-orchestration.md).

## Consequences

### Positive

- **Eliminates an SPOF-class cluster.** No Temporal frontend / history /
  matching / worker pods to size, patch or DR-plan. Coordination rides
  on Kafka, which is already governed by ADR-0013 (no-SPOF policy).
- **Lower operational and economic cost.** No dedicated Cassandra
  keyspaces (`temporal`, `temporal_visibility`); no second cluster on
  the on-call rotation; reuses infra teams already operate.
- **Removes the determinism footgun from business code.** Service
  authors write ordinary Rust against Postgres + Kafka; correctness
  comes from idempotency and outbox atomicity, not from a hidden replay
  contract.
- **Architectural alignment.** ADR-0022 already declared outbox +
  Debezium as the publication path; this ADR makes it the orchestration
  path too, so the platform has _one_ pattern instead of two.
- **Auditability and replay.** Every step is an event with a stable
  `event_id` and an audit row; replay = re-read Kafka offsets, which
  every other consumer in the platform already supports.
- **Foundry parity.** The pattern matches what
  [`docs_original_palantir_foundry/`](../../../docs_original_palantir_foundry/)
  describes for Foundry's Automate / Builds / Datasets surfaces.

### Negative

- **Refactor cost.** Three Rust services lose their `temporal_*`
  modules; five Go workers are deleted (or rewritten as Kafka
  consumers); Helm charts, CI workflows, justfile recipes and several
  ADRs / migration documents need updates. The
  [migration plan](../migration-plan-foundry-pattern-orchestration.md)
  estimates 6–10 weeks for one senior engineer or 3–5 weeks for two in
  parallel.
- **Durable execution becomes a manual contract.** Temporal gave
  durable execution and replay "for free"; under this ADR, the same
  guarantees come from the combination of (outbox atomic write) +
  (consumer idempotency by `event_id`) + (Postgres state-machine
  optimistic locking). The new shared crates (`libs/state-machine/`,
  `libs/saga/`, `libs/event-scheduler/`) absorb most of the boilerplate
  but **must** be used consistently.
- **No first-class "workflow UI".** Temporal Web is replaced by Grafana
  views over the `audit_compliance` schema and per-flow runbooks under
  `docs/operations/`. Debugging cross-step flows requires trace-id
  propagation discipline.
- **Topic / schema sprawl risk.** Each saga step adds a topic. Mitigated
  by the naming convention and `KafkaTopic` CRs being declared in
  `infra/helm/infra/kafka-cluster/` with explicit retention and Schema
  Registry compatibility (`BACKWARD`).
- **Spark Operator is now load-bearing for pipeline workloads.** Its
  failure modes (CRD reconciliation lag, driver pod eviction) become a
  first-class concern; covered by chaos tests under ADR-0032.

### Neutral

- The platform retains Cassandra for the operational stores defined in
  ADR-0020; only the `temporal*` keyspaces are dropped.
- NATS remains the control-plane bus per ADR-0011; nothing in this ADR
  changes that boundary.

## Migration plan

The full step-by-step plan, with file paths, verification commands and
failure modes per task, lives in
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../migration-plan-foundry-pattern-orchestration.md).
At a glance:

- Phase 0 — Decision + ADRs + inventory (this ADR is task 0.1).
- Phase 1 — New Rust support libs (`state-machine`, `saga`,
  `event-scheduler`, idempotency helpers).
- Phase 2 — Infra: `KafkaTopic` CRs, `SparkApplication` templates,
  Postgres schema migrations, K8s `CronJob`s, Debezium connectors.
- Phase 3 — Per-service refactor behind an `orchestration_backend`
  feature flag (`pipeline-schedule`, `approvals`,
  `automation-operations`, `workflow-automation`, reindex consumer).
- Phase 4 — Delete `workers-go/` and `libs/temporal-client/`.
- Phase 5 — Helm-uninstall Temporal; drop `temporal*` Cassandra
  keyspaces; remove Temporal dashboards/alerts.
- Phase 6 — Smoke + chaos validation; Grafana views and runbooks for
  the new substrate.

## Alternatives considered

- **Stay on Temporal (status quo of ADR-0021).** Rejected for the
  reasons in *Context*: duplicates ADR-0022's coordination substrate,
  adds an SPOF-class cluster and a Cassandra footprint we are otherwise
  shrinking, and leaks a determinism contract into business code.
- **Replace Temporal with another centralised engine
  (Cadence / Conductor / Argo Workflows as orchestrator).** Rejected
  for the same structural reason: any centralised engine reintroduces
  an SPOF class and a second coordination substrate alongside Kafka +
  outbox.
- **Keep Temporal only for human-wait flows (approvals).** Rejected
  because the operational cost of running Temporal is dominated by the
  cluster itself, not by the number of workflow types; running it for
  one flow is the worst trade-off.
- **Argo Workflows for pipelines, keep Temporal for the rest.**
  Rejected because we already operate Spark Operator and would gain
  nothing by introducing a third workflow engine.

## Notes on numbering

The migration plan
[`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../migration-plan-foundry-pattern-orchestration.md)
references this decision as "ADR-0027" in its prose. That number was
already taken by
[`ADR-0027-cedar-policy-engine.md`](./ADR-0027-cedar-policy-engine.md);
per the rule in [`README.md`](./README.md) ("never reuse a previous
number even if the ADR was retracted"), this ADR was filed under the
next free slot, **ADR-0037**. The migration-plan prose will be updated
to point at ADR-0037 in a follow-up doc-update task.
