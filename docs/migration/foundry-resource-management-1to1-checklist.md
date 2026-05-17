# Foundry Resource Management 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Resource Management
layer: resource queues with vCPU/vGPU quotas, project-to-queue assignment,
job submission and accounting for builds/schedules/functions/agents/notebook
sessions/code workspaces/compute modules, cost insights per project and
per branch, fair scheduling and preemption, queue admin UI, and
integrations with Apollo (per-environment caps), Compass (project as the
queue ownership boundary), and the Security/Governance audit trail.

> **Scope distinction.** This checklist covers the **queue and quota**
> layer. Per-service worker pools (e.g. Spark executors for Pipeline
> Builder, isolate pools for Functions, kernel sessions for Notebooks)
> remain owned by their respective checklists; this checklist defines
> the cross-cutting accounting + admission control that sits in front
> of them.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets, or any
non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `todo` | Not implemented or not yet verified in OpenFoundry. |
| `partial` | Some surface exists, but behavior is incomplete or not wired end-to-end. |
| `blocked` | Requires a platform dependency, public documentation, or product decision. |
| `done` | Implemented, tested, documented, and verified through UI or API smoke tests. |

## Priority vocabulary

| Priority | Meaning |
| --- | --- |
| `P0` | Required for credible queues: queue resource, project assignment, vCPU/memory/vGPU caps, admission control on builds/functions/notebooks. |
| `P1` | Required for Foundry-style parity: cost insights per project and per branch, fair scheduling, preemption, queue admin UI, alerting on saturation. |
| `P2` | Advanced parity: spot/preemptible classes, cross-environment caps (Apollo handoff), GPU sharing, autoscaling policies, forecast and budget alerts. |

## Official Palantir documentation library

### Product overview

- [Resource Management overview](https://www.palantir.com/docs/foundry/resource-management/overview)
- [Resource Queues](https://www.palantir.com/docs/foundry/resource-management/resource-queues)

### Concepts

- [Queue quotas](https://www.palantir.com/docs/foundry/resource-management/queue-quotas)
- [Project assignment](https://www.palantir.com/docs/foundry/resource-management/project-assignment)
- [Cost insights](https://www.palantir.com/docs/foundry/resource-management/cost-insights)
- [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling)

### Integrations

- [Branch cost insights](https://www.palantir.com/docs/foundry/foundry-branching/cost-insights)
- [Build accounting](https://www.palantir.com/docs/foundry/data-integration/build-accounting)
- [Functions cost](https://palantir.com/docs/foundry/functions/cost)
- [Agents and notebook cost](https://www.palantir.com/docs/foundry/resource-management/ai-cost)

## Milestone A: credible queues with admission control

### Queue resource and policies

- [ ] `RM.1` Queue resource (`P0`, `todo`)
  - CRUD a `resource_queue` with: name, description, owning organization, vCPU cap, memory cap (GiB), vGPU cap, max concurrent jobs, max job duration, default priority, markings.
  - Stable RID, Compass-discoverable.
  - Docs: [Resource Queues](https://www.palantir.com/docs/foundry/resource-management/resource-queues).

- [ ] `RM.2` Queue worker classes (`P0`, `todo`)
  - Each queue lists worker classes (e.g. `small`, `medium`, `large`, `gpu-small`) with per-class vCPU/memory/vGPU shapes and per-class concurrency caps.
  - Jobs request a class; the queue admits when that class has free capacity.
  - Docs: [Queue quotas](https://www.palantir.com/docs/foundry/resource-management/queue-quotas).

- [ ] `RM.3` Reservations and overflow policy (`P0`, `todo`)
  - Each queue declares reserved capacity per worker class and an overflow policy (queue, reject, burst with cost premium).
  - Reservations are honored even when other queues are saturated.
  - Docs: [Queue quotas](https://www.palantir.com/docs/foundry/resource-management/queue-quotas).

### Project assignment

- [ ] `RM.4` Project-to-queue binding (`P0`, `todo`)
  - Each Compass project is bound to exactly one default queue per resource kind (compute, AI, notebook, function).
  - Optional per-resource override (e.g. a specific pipeline binds to a higher-tier queue).
  - Docs: [Project assignment](https://www.palantir.com/docs/foundry/resource-management/project-assignment).

- [ ] `RM.5` Branch overrides (`P0`, `todo`)
  - Branches inherit the parent project's queue by default; a branch may temporarily bind to a sandbox/low-cost queue while in development.
  - Docs: [Branch cost insights](https://www.palantir.com/docs/foundry/foundry-branching/cost-insights).

### Admission control

- [ ] `RM.6` Job submission API (`P0`, `todo`)
  - Builds, schedule runs, function executions, notebook session starts, code workspace starts, compute module starts, agent runs, and Quiver server-side recomputes all route their submission through the queue's admission API.
  - Submission returns `admitted` (with a slot id) or `queued` (with a wait estimate) or `rejected` (with reason).
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

- [ ] `RM.7` Per-resource accounting (`P0`, `todo`)
  - Every admitted slot reports start time, end time, peak vCPU·s, peak memory·s, vGPU·s used, exit reason, and the originating resource RID and caller.
  - Stored in `resource_run_accounting` with retention of at least 90 days.
  - Docs: [Build accounting](https://www.palantir.com/docs/foundry/data-integration/build-accounting).

- [ ] `RM.8` Hard caps and back-pressure (`P0`, `todo`)
  - When a queue is saturated, new submissions either wait or reject per the queue's overflow policy.
  - Callers receive a typed `QueueFull` error with the queued count and a wait estimate.
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

## Milestone B: cost insights, fair scheduling, preemption, admin UI

### Cost insights

- [ ] `RM.9` Per-project cost view (`P1`, `todo`)
  - Aggregate vCPU·s, memory·GiB·s, vGPU·s per project per day and per month, with breakdown by resource kind (build/function/notebook/etc.) and by user.
  - Surface in the project overview and in the queue admin.
  - Docs: [Cost insights](https://www.palantir.com/docs/foundry/resource-management/cost-insights).

- [ ] `RM.10` Per-branch cost view (`P1`, `todo`)
  - Same metrics aggregated per branch with delta vs. main and per-author breakdown.
  - Surface in the Global Branching app's branch detail.
  - Docs: [Branch cost insights](https://www.palantir.com/docs/foundry/foundry-branching/cost-insights).

- [ ] `RM.11` Cost currency translation (`P1`, `todo`)
  - Per-queue cost rates in configurable currency (€/$/£) for CPU·s, GiB·s, GPU·s, with an enrollment-level conversion table.
  - Reports show both raw resource units and currency-equivalent.
  - Docs: [Cost insights](https://www.palantir.com/docs/foundry/resource-management/cost-insights).

### Fair scheduling and priorities

- [ ] `RM.12` Priority classes (`P1`, `todo`)
  - Built-in priority classes: `interactive`, `default`, `batch`, `low`.
  - Higher priority preempts lower priority within the same queue when reservations are full and the higher-priority job has been waiting beyond a threshold.
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

- [ ] `RM.13` Fair-share across projects (`P1`, `todo`)
  - When multiple projects share a queue, scheduler maintains a per-project fair share so one project cannot starve others.
  - Configurable weights per project.
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

### Preemption

- [ ] `RM.14` Graceful preemption (`P1`, `todo`)
  - Preempted jobs receive a SIGTERM-equivalent and a configurable grace period (default 60s) to checkpoint and exit.
  - Builds and function executions implement checkpoint-and-resume hooks where possible; otherwise they are retried under their own retry policy.
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

### Admin UI and alerting

- [ ] `RM.15` Queue admin UI (`P1`, `todo`)
  - Page per queue with: utilization charts (live + 30-day), reservation usage, top consumers (project, user, resource), queued jobs, recently completed, alerting rules.
  - Edit queue caps, classes, reservations, and overflow policy from the UI.
  - Docs: [Resource Queues](https://www.palantir.com/docs/foundry/resource-management/resource-queues).

- [ ] `RM.16` Saturation alerts (`P1`, `todo`)
  - Per-queue alerts (Pulse notifications) when utilization > 90% for > 15 min, when queue wait > 5 min, when failures spike.
  - Subscribers configurable per queue.
  - Docs: [Cost insights](https://www.palantir.com/docs/foundry/resource-management/cost-insights).

## Milestone C: spot, multi-region, GPU sharing, forecasts

### Cost classes and spot

- [ ] `RM.17` Spot/preemptible classes (`P2`, `todo`)
  - Worker classes can declare a spot tier with reduced cost and explicit preempt-at-any-moment semantics.
  - Build runners that opt in must handle preemption; other resource kinds default to non-spot.
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

- [ ] `RM.18` GPU sharing (`P2`, `todo`)
  - GPU worker classes support shared use (e.g. NVIDIA MIG slices) with per-tenant isolation; per-job vGPU allocation tracked precisely.
  - Docs: [Queue quotas](https://www.palantir.com/docs/foundry/resource-management/queue-quotas).

### Multi-environment and Apollo handoff

- [ ] `RM.19` Per-environment queue caps (`P2`, `todo`)
  - Apollo pipelines may declare per-environment caps and reservations as part of the product manifest's config overlay (see [Apollo checklist](./foundry-apollo-1to1-checklist.md)).
  - Docs: [Resource Queues](https://www.palantir.com/docs/foundry/resource-management/resource-queues).

- [ ] `RM.20` Cross-region quota visibility (`P2`, `todo`)
  - Admin sees aggregated queue utilization across regions with per-region drill-down.
  - Cross-region overflow is opt-in and audited.
  - Docs: [Resource Queues](https://www.palantir.com/docs/foundry/resource-management/resource-queues).

### Autoscaling, forecast, budget alerts

- [ ] `RM.21` Queue autoscaling (`P2`, `todo`)
  - A queue can declare min/max worker pool sizes; the scheduler scales backing capacity within bounds based on utilization.
  - Hooks to underlying infrastructure (Kubernetes node groups, cloud autoscaling groups) are pluggable.
  - Docs: [Scheduling and preemption](https://www.palantir.com/docs/foundry/resource-management/scheduling).

- [ ] `RM.22` Cost forecast (`P2`, `todo`)
  - Per-project monthly cost forecast based on the trailing 14 days; trend lines and projected month-end.
  - Surface in project overview and in the queue admin.
  - Docs: [Cost insights](https://www.palantir.com/docs/foundry/resource-management/cost-insights).

- [ ] `RM.23` Budget alerts (`P2`, `todo`)
  - Per-project budgets with notifications at 50%, 80%, 100% of the configured cap.
  - Optional hard stop at 110% (block new submissions; interactive override by admin).
  - Docs: [Cost insights](https://www.palantir.com/docs/foundry/resource-management/cost-insights).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify every resource kind that currently consumes compute (builds, function exec, agent runs, notebook sessions, code workspaces, compute modules, Quiver, etc.) and inventory their submission paths.
- [ ] `INV.2` Identify the accounting tables already present (overlap with `pipeline_run_metrics`) and design a unified schema.
- [ ] `INV.3` Identify the per-service worker pool managers and define the admission hook contract.
- [ ] `INV.4` Identify the Pulse notification integration for saturation alerts.
- [ ] `INV.5` Identify the audit emission path for queue admin changes.
- [ ] `INV.6` Identify the cost-rate config owner (per enrollment) and the currency conversion source.
- [ ] `INV.7` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `resource-queue-service` | Queue + class + reservation CRUD, admission API, accounting writer, fair-share scheduler, preemption coordinator. |
| `resource-accounting-service` | Long-term storage of per-run accounting, per-project/per-branch aggregates, cost insights queries. |
| `pulse-notification-service` | Saturation and budget alerts (uses the existing notification-alerting-service). |
| Each compute-producing service | Implements the admission hook + emits per-run accounting records on completion. |
| `apps/web` | Queue admin UI, project cost overview, branch cost insights panel, alert subscription UI. |
