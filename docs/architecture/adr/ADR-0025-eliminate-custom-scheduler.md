# ADR-0025: Eliminate the custom pipeline scheduler

- **Status:** Accepted
- **Date:** 2026-05-02
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes / supplements:**
  - The bespoke scheduler in
    [services/pipeline-schedule-service/src/main.rs](../../../services/pipeline-schedule-service/src/main.rs)
    and the in-process tick loop in
    [services/workflow-automation-service/src/domain/executor.rs](../../../services/workflow-automation-service/src/domain/executor.rs).
- **Related ADRs:**
  - [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md) —
    Temporal becomes the platform's durable execution engine; this
    ADR is its operational corollary on the scheduler side.

## Context

The current platform schedules pipelines and recurring jobs through
`pipeline-schedule-service`, a Rust Deployment that:

- Holds a `cron = "0.12"` table in process memory.
- Uses a per-replica `tokio::time::interval` tick loop.
- Persists "next fire" markers in Postgres.
- Is **scaled to a single replica**, because two replicas would
  double-fire every job. There is no leader election, no fencing
  token, no cross-replica coordination.

Operational reality:

- **It is a strict SPOF.** A pod crash, a node drain or a rolling
  upgrade silently delays every scheduled job by however long the
  reschedule takes (worst case: a node failure with PV detach delay,
  measured in minutes).
- **It cannot be horizontally scaled** without rewriting the whole
  coordination model (leader election, exactly-once fencing, work
  stealing).
- **It cannot survive a regional failure**: the single replica lives
  in one zone, the Postgres state lives in one cluster, and there is
  no failover protocol.
- **It overlaps in scope with `workflow-automation-service`**, whose
  in-process executor reimplements similar primitives. Two
  half-implementations of the same idea.

The audit in
[docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md)
already flagged both pieces as critical correctness and availability
risks.

[ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md) makes
**Temporal Schedules** available as a first-class platform primitive:
durable, exactly-once cron with retries, multi-DC failover, history,
visibility, and pause/unpause out of the box.

We need to decide the **fate of `pipeline-schedule-service`** as a
Kubernetes Deployment now that Temporal Schedules covers its
functionality.

## Options considered

### Option A — Delete the Deployment, retire the executable, replace with Temporal Schedules (chosen)

- Every recurring job is rewritten as a Temporal Schedule that
  triggers a Workflow defined in `workers-go/pipeline/`.
- The "API surface" (start / pause / resume / list / next-run) is
  served by Temporal's own gRPC and is wrapped by a thin Rust client
  in `libs/temporal-client` for UI / CLI consumers.
- The Rust crate, the binary, the Helm chart, the Postgres tables and
  the alerts disappear from the repository in the same change set.
- Schedule definitions are **declared in code** under
  `workers-go/pipeline/schedules/` and registered at worker startup
  via `client.ScheduleClient().Create(ctx, …)`, idempotently.

### Option B — Rewrite `pipeline-schedule-service` to be HA (rejected)

- Adds leader election (etcd lease or Postgres advisory lock), fencing
  tokens, work stealing, persistence of next-fire markers across
  replicas, replay safety.
- Reimplements 80% of what Temporal Schedules already gives us, with
  a fraction of the production hardening.
- Locks the platform into maintaining a bespoke scheduler indefinitely.

### Option C — Keep `pipeline-schedule-service` as a thin proxy in front of Temporal (rejected)

- A Rust service that receives schedule CRUD calls and forwards them
  to Temporal.
- Adds a hop without adding value: Temporal's own gRPC is the proxy
  surface, and `libs/temporal-client` already gives Rust callers a
  typed client.
- Keeps a Deployment alive only to host glue that does not need to
  exist.

### Option D — Use a Kubernetes `CronJob` per scheduled pipeline (rejected)

- Pushes scheduling into Kubernetes itself.
- Lacks the per-job state that pipelines need (last successful run,
  cumulative back-pressure, signals, retries with bounded backoff).
- Multiplies the manifest count and ties scheduling to cluster
  upgrades.

## Decision

We adopt **Option A**: the **`pipeline-schedule-service` Deployment is
deleted** and the source code, Helm chart, Postgres tables and alerts
are retired in the same change set. **Temporal Schedules**
([ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md)) is the
platform's only scheduled-execution mechanism going forward.

The same decision applies to the in-process tick loop in
`workflow-automation-service/src/domain/executor.rs`: every recurring
trigger it owns becomes a Temporal Schedule.

## Migration path

1. **Inventory** every entry in the current scheduler's Postgres
   table (`pipeline_schedule_service.schedules`). Output: a CSV
   (`docs/architecture/migration/pipeline-schedules.csv`) with the
   cron expression, owning project, target pipeline, parameters and
   ownership metadata.
2. **For each entry**, author a Temporal Schedule registration in
   `workers-go/pipeline/schedules/` that maps to the corresponding
   Workflow. Idempotent: re-running registration converges.
3. **Cutover per project**: pause the entry in the legacy scheduler,
   create the Temporal Schedule, observe one or two firings, delete
   the legacy entry.
4. After every project has cut over and **one full release cycle has
   passed without traffic** to `pipeline-schedule-service`:
   - Delete the Helm release.
   - Delete the source crate.
   - Drop the Postgres tables (they live in a soon-to-be-decommissioned
     cluster per [ADR-0024](./ADR-0024-postgres-consolidation.md);
     deletion happens as part of that consolidation).
   - Remove the Cargo workspace member entry.
   - Remove the `compose.yaml` service.

The cutover is gated by the same one-release-cycle "decommissioned
but present" rule as the Postgres consolidation
([ADR-0024](./ADR-0024-postgres-consolidation.md)) so that any missed
schedule reveals itself before the code is gone.

## Operational consequences

- One fewer Deployment, one fewer Helm release, one fewer Cargo
  member, one fewer set of alerts.
- Schedule visibility moves to Temporal Web (already provisioned
  by [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md)).
- A new section in `infra/runbooks/temporal.md` covers the
  schedule-management surface (create / pause / resume / backfill,
  the last via Temporal's first-class backfill API rather than
  any bespoke replay mechanism).
- New CI check: a denylist in workspace lints that fails the build
  if `pipeline-schedule-service` reappears as a workspace member or
  as a Helm release.
- `compose.yaml` no longer references `pipeline-schedule-service`;
  the dev environment uses the Temporal docker compose file the
  Helm chart ships.

## Consequences

### Positive

- Eliminates a strict SPOF that was scaled to one replica by design.
- Recurring jobs gain durable execution, exactly-once cron semantics,
  bounded-backoff retries, multi-DC failover and a UI for free.
- One fewer service to operate, monitor, secure and upgrade.
- Closes the "two half-schedulers" overlap with
  `workflow-automation-service`.

### Negative

- Schedule **definitions live in Go** (under
  `workers-go/pipeline/schedules/`) rather than in a Rust service. The
  team accepted this trade-off in
  [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md); this
  ADR inherits the trade-off.
- Pipelines that previously relied on side effects of being inside
  the scheduler process (none should, but a chaos audit must
  confirm) need to be lifted to a real activity.

### Neutral

- Schedules become observable in the same surface as workflow
  executions. Operators get one place to look instead of two.

## Follow-ups

- Implement migration plan task **S2.7** (port schedules and delete
  the service).
- Author the schedule inventory CSV.
- Wire the deletion into the same release that lands
  [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md) end-to-end
  so the platform is never in a state where neither scheduler is
  authoritative.
- Add the workspace-member denylist CI check.
