# Foundry-pattern migration — closing audit

- **Status:** Final.
- **Date:** 2026-05-04.
- **Scope:** FASE 11 / Tarea 11.4 of
  [`migration-plan-foundry-pattern-orchestration.md`](./migration-plan-foundry-pattern-orchestration.md).
- **Supersession:** [ADR-0037](./adr/ADR-0037-foundry-pattern-orchestration.md)
  supersedes [ADR-0021](./adr/ADR-0021-temporal-on-cassandra-go-workers.md).

This document is the formal close-out of the 11-phase migration that
retired Apache Temporal + the Go workers in `workers-go/` in favour of
the Foundry-pattern substrate (Postgres state machines + `libs/saga` +
`libs/state-machine` + transactional outbox + Debezium + Kafka +
SparkApplication CRs + k8s `CronJob`s).

It is **not** the design document — that is
[`foundry-pattern-orchestration.md`](./foundry-pattern-orchestration.md).
This file captures the grep-gate evidence that the cutover is real and
no live code path falls back to the old runtime.

## Decommissioned artefacts

The following were physically removed from the tree (verified absent
at the time of writing):

- `libs/temporal-client/` — Rust client for the Temporal frontend.
- `workers-go/` — Go worker workspace (pipeline, workflow-automation,
  approvals, reindex).
- `infra/helm/infra/temporal/` — Helm chart for the Temporal
  frontend / history / matching / worker pods.
- `infra/helm/apps/of-platform/templates/temporal-workers.yaml` —
  Deployment manifest for the Rust-side Temporal workers.
- `infra/test-tools/chaos/temporal-history-kill.yaml` — ChaosMesh
  schedule pointed at the retired Temporal history pods.
- `libs/testing/src/temporal.rs` and `libs/testing/src/go_workers.rs`
  — testcontainer harnesses for the legacy runtime.
- `.github/workflows/go-workers.yml` — Go workers CI matrix.
- `infra/helm/apps/{of-platform,of-data-engine,of-apps-ops}/values*.yaml`
  `temporal:` / `temporalWorkers:` blocks.
- `infra/helm/apps/{of-platform,of-data-engine,of-apps-ops}/templates/services.yaml`
  `TEMPORAL_HOST_PORT` / `TEMPORAL_NAMESPACE` /
  `TEMPORAL_REQUIRE_REAL_CLIENT` / `TEMPORAL_TASK_QUEUE_*` env
  injection blocks.
- `infra/compose/docker-compose.yml` `temporal` + `temporal-ui`
  services and their Cassandra bootstrap dependency edge.

## Replacement substrate

| Concern                 | Pre-migration (ADR-0021)              | Post-migration (ADR-0037)                                              |
|-------------------------|---------------------------------------|------------------------------------------------------------------------|
| Pipeline runs           | Temporal `PipelineRun` workflow       | `pipeline-build-service` → SparkApplication CR via Spark Operator      |
| Cron / Time triggers    | Temporal Schedules                    | `schedules-tick` Kubernetes `CronJob` (`libs/event-scheduler`)         |
| Workflow automation     | Temporal `automation_run` workflow    | `workflow-automation-service` Postgres state machine + Kafka consumer  |
| Saga / cleanup          | Temporal cleanup-workspace workflow   | `automation-operations-service` + `libs/saga` (LIFO compensation)      |
| Approvals               | Temporal approvals workflow + signal  | `approvals-service` 5-state machine + `approvals-timeout-sweep` cron   |
| Reindex                 | Temporal `OntologyReindex` (Go)       | `reindex-coordinator-service` Kafka-driven coordinator                 |
| Idempotency             | Temporal workflow history dedupe      | `libs/idempotency::PgIdempotencyStore` (record-before-process)         |
| Outbound events         | Direct emit from Temporal activity    | `libs/outbox` INSERT+DELETE same-TX + Debezium EventRouter SMT         |
| Cassandra keyspaces     | `temporal_persistence`, `temporal_visibility` | Six application keyspaces only (per ADR-0020)                  |

## Adoption metrics (snapshot)

```
$ grep -rn "PgIdempotencyStore\|use idempotency" --include='*.rs' services/ libs/ | wc -l
24
$ grep -rn "use saga::\|libs/saga" --include='*.rs' services/ libs/ | wc -l
19
$ grep -rn "PgStore\|libs/state-machine" --include='*.rs' services/ libs/ | wc -l
34
```

These three numbers track adoption of the three Foundry-pattern
primitives across the service tree. Every refactored service consumes
at least one.

## Grep gates

### Gate 1 — decommissioned directories

```
$ test -d libs/temporal-client && echo EXISTS || echo absent
absent
$ test -d workers-go && echo EXISTS || echo absent
absent
$ test -d infra/helm/infra/temporal && echo EXISTS || echo absent
absent
```

Pass.

### Gate 2 — `temporal-client` references in build inputs

```
$ grep -rn "temporal-client\|temporal_client" --include='*.toml' --include='*.rs' services/ libs/
services/pipeline-schedule-service/src/main.rs:28:    The `temporal-client` adapter ... has been replaced.
```

The single remaining hit is a doc-comment describing the migration
itself; no `Cargo.toml` lists `temporal-client` as a dep. Pass.

### Gate 3 — `TEMPORAL_*` env vars in active code

```
$ grep -rn "TEMPORAL_" --include='*.rs' --include='*.toml' --include='*.yaml' services/ libs/ infra/
```

All hits fall into two buckets: (a) `GEOTEMPORAL_OBSERVATIONS` /
`GEOTEMPORAL_OBS_SENT` enum variants in
`monitoring-rules-service` (geo-temporal data, not Temporal) and
(b) explanatory retirement-annotation blocks in the three
`infra/helm/apps/*/templates/services.yaml` files. No service reads a
`TEMPORAL_*` env var at runtime. Pass.

### Gate 4 — Cassandra keyspace bootstrap

```
$ grep -nE "temporal_(persistence|visibility)" infra/helm/infra/cassandra-cluster/templates/keyspaces-job.yaml
14:#   * The legacy `temporal_persistence` / `temporal_visibility`
105:    -- legacy `temporal_*` keyspaces are not provisioned (Temporal
```

Both hits are negative-assertion comments: the bootstrap Job never
issues `CREATE KEYSPACE` for `temporal_*`. The brownfield-cluster
DROP procedure lives in
[`infra/runbooks/temporal.md`](../../infra/runbooks/temporal.md).
Pass.

### Gate 5 — surviving `temporal` mentions, classified

54 hits remain across `services/`, `libs/`, `infra/` (excluding
`infra/runbooks/temporal.md` which is the retirement runbook itself).
Triage:

- **Historical-context comments** (~38 hits). Doc-comments of the form
  *"Foundry-pattern replacement of the Temporal X workflow"* in the
  refactored services. Required: they document why the new module
  exists.
- **False positives** (~16 hits):
  - Flink temporal-interval joins in `event-streaming-service`.
  - Vega-Lite `"type": "temporal"` in `libs/ontology-kernel/src/domain/time_series.rs`.
  - Spanish *"orden temporal"* (chronological order) in
    `dataset-versioning-service`.
  - `Geotemporal*` enum variants in `monitoring-rules-service`.
  - `time-series-data-service/Cargo.toml` description ("temporal
    workloads").

No hit references a live Temporal client, server, task queue,
keyspace or workflow. Pass.

### Gate 6 — `workers-go` references in active code

```
$ grep -rn "workers-go" --include='*.rs' --include='*.toml' --include='*.yaml' \
    services/ libs/ infra/helm/ | wc -l
26
```

All 26 are doc-comments documenting the cutover — *"Rust replacement
for `workers-go/reindex`"*, *"same payload shape as the
`workers-go/workflow-automation` activity"*. The directory itself is
absent (Gate 1). Pass.

## ADR ledger

The following ADRs were modified to reflect the migration:

- `ADR-0021` — Temporal on Cassandra. Status moved to **Superseded by
  ADR-0037**, with a Migration log appendix.
- `ADR-0037` — Foundry-pattern orchestration. **Accepted**, the
  canonical decision for this migration.
- `ADR-0038` — Event contract + idempotency. **Accepted**, sibling to
  ADR-0037.

ADR-0027 (Cedar policy engine) is **unrelated** to this migration —
earlier drafts of the migration plan mistakenly cited it; those typos
were fixed in Tarea 10.2.

## What this audit does not cover

- **Live cluster verification.** Tareas 11.1–11.3 author the smoke
  scenario, perf bench and ChaosMesh manifests; running them belongs
  to the operator, not to this audit.
- **CHANGELOG + tag.** That is Tarea 11.5.
- **Historical migration-plan documents.** `migration-plan-cassandra-foundry-parity.md`
  still references Temporal extensively because it pre-dates ADR-0037
  and documents the now-superseded plan. We intentionally do not
  rewrite history.

## Sign-off

The grep gates above are the closing evidence the migration plan
required. With them green, FASE 11 / Tarea 11.4 is complete and the
remaining 11.x tareas (11.1–11.3 cluster-dependent specs, 11.5
versioning + tag) can proceed.
