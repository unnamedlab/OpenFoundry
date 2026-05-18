# Foundry Dynamic Scheduling, Machinery, and Object Explorer 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for the three Ontology-tier
applications that sit between the ontology definition layer and the
end-user analytical surfaces: **Dynamic Scheduling** (triggered refresh
of object sets and dataset builds), **Machinery** (state-machine
definitions over ontology objects with transitions, guards, and
effects), and **Object Explorer** (the end-user browsing surface over
ontology objects with filters, saved views, search, and link traversal).

> Scope distinction: Ontology definition (object types, properties,
> links, action types, interfaces) lives in
> [foundry-ontology-manager-object-views-1to1-checklist.md](./foundry-ontology-manager-object-views-1to1-checklist.md).
> Vertex (graph exploration) is covered by
> [foundry-vertex-1to1-checklist.md](./foundry-vertex-1to1-checklist.md).
> Map (geospatial exploration) is covered by
> [foundry-map-1to1-checklist.md](./foundry-map-1to1-checklist.md).
> Object Views (object detail pages) live in
> [foundry-ontology-manager-object-views-1to1-checklist.md](./foundry-ontology-manager-object-views-1to1-checklist.md).
> Automate / Foundry Rules (the rule-engine surface) lives in
> [foundry-automate-rules-1to1-checklist.md](./foundry-automate-rules-1to1-checklist.md).
> This file covers the three remaining Ontology-tier applications.
> Queue admission for Dynamic Scheduling jobs is cross-linked to
> [foundry-resource-management-1to1-checklist.md](./foundry-resource-management-1to1-checklist.md).

This document is intentionally implementation-oriented. It does not
attempt to clone Palantir branding, private source code, proprietary
assets, screenshots, or any non-public behavior. The target is
**functional parity based on public Palantir Foundry documentation**:
the same product concepts, comparable workflows, compatible resource
models where useful, and OpenFoundry-native implementation details that
can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native
implementation, not a pixel-perfect clone.

This checklist covers the three remaining Ontology-tier applications. It
depends on the Ontology checklist for object/link/action models, on the
Resource Management checklist for queue admission of scheduled jobs, on
the Security/Governance checklist for permission-aware enforcement of
transitions and views, and on the Pipeline Builder checklist for the
build-success trigger contract.

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
| `P0` | Required for credible Dynamic Scheduling, Machinery, and Object Explorer with a time-triggered schedule, a single machine definition + instance, and Object Explorer browse + filter. |
| `P1` | Required for Foundry-style parity: full trigger catalog, transition guards/effects, saved views, share-links. |
| `P2` | Advanced parity: SLA + backpressure, branched machine definitions, cross-machine references, exploration history, export-to-dataset, Vertex integration. |

## Official Palantir documentation library

### Dynamic Scheduling

- [Dynamic Scheduling overview](https://www.palantir.com/docs/foundry/dynamic-scheduling/overview)
- [Configure a schedule](https://www.palantir.com/docs/foundry/dynamic-scheduling/configure)
- [Schedule triggers](https://www.palantir.com/docs/foundry/dynamic-scheduling/triggers)

### Machinery

- [Machinery overview](https://www.palantir.com/docs/foundry/machinery/overview)
- [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines)
- [Transitions](https://www.palantir.com/docs/foundry/machinery/transitions)
- [Workshop integration](https://www.palantir.com/docs/foundry/machinery/workshop-integration)

### Object Explorer

- [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview)
- [Saved views](https://www.palantir.com/docs/foundry/object-explorer/saved-views)
- [Search](https://www.palantir.com/docs/foundry/object-explorer/search)

## Milestone A: minimum viable Dynamic Scheduling / Machinery / Object Explorer parity

### Dynamic Scheduling: schedule resource and time trigger

- [ ] `ONA.1` Schedule resource (`P0`, `todo`)
  - CRUD a `dynamic_schedule` resource with title, description, target kind (object set, dataset build, pipeline plan), target RID, trigger list, enabled flag, owning project, organizations, and markings.
  - Stable RID and Compass-discoverable.
  - Audit-trail entries on create, edit, enable/disable, and delete.
  - Docs: [Dynamic Scheduling overview](https://www.palantir.com/docs/foundry/dynamic-scheduling/overview), [Configure a schedule](https://www.palantir.com/docs/foundry/dynamic-scheduling/configure).

- [ ] `ONA.2` Time-based trigger (`P0`, `todo`)
  - Cron-style and interval-based time triggers with timezone-aware evaluation.
  - Skip a fire if the previous run is still pending; record the skipped event.
  - Docs: [Schedule triggers](https://www.palantir.com/docs/foundry/dynamic-scheduling/triggers).

- [ ] `ONA.3` Manual run + last-successful-run surface (`P0`, `todo`)
  - "Run now" button that enqueues a one-off job with the same payload contract as a triggered run.
  - Sidebar shows last successful run timestamp, last failure (with error code), and next planned fire.
  - Docs: [Configure a schedule](https://www.palantir.com/docs/foundry/dynamic-scheduling/configure).

- [ ] `ONA.4` Resource Management queue admission (`P0`, `todo`)
  - Every scheduled run is submitted through the Resource Management admission API with project queue and priority.
  - Reject runs that exceed queue caps with a clear error code so the schedule surface can show "queue full".
  - Cross-link: [foundry-resource-management-1to1-checklist.md](./foundry-resource-management-1to1-checklist.md).
  - Docs: [Dynamic Scheduling overview](https://www.palantir.com/docs/foundry/dynamic-scheduling/overview).

### Machinery: state-machine definition and per-object instance

- [ ] `ONA.5` Machine definition resource (`P0`, `todo`)
  - CRUD a `machine_definition` resource bound to an ontology object type with: states, initial state, terminal states, transitions, and metadata.
  - Validate that every transition references defined states and that there is exactly one initial state.
  - Stable RID and Compass-discoverable.
  - Docs: [Machinery overview](https://www.palantir.com/docs/foundry/machinery/overview), [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines).

- [ ] `ONA.6` Per-object machine instance (`P0`, `todo`)
  - Each ontology object of the bound type has at most one active instance per machine definition.
  - Instance row records: current state, last transition timestamp, last transition actor, definition version.
  - Auto-create the instance on first transition request; reject requests for non-bound object types.
  - Docs: [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines).

- [ ] `ONA.7` Transition execution (`P0`, `todo`)
  - Execute a named transition on an instance: validate the current state matches the transition's `from`, write the new state, and append a history row.
  - Reject transitions that would leave the machine in an undefined state or that violate the per-object linearization contract.
  - Docs: [Transitions](https://www.palantir.com/docs/foundry/machinery/transitions).

### Object Explorer: browse and filter

- [ ] `ONA.8` Object Explorer shell + object-type picker (`P0`, `todo`)
  - Top-level page listing accessible object types grouped by ontology namespace.
  - Selecting a type loads a default view: paginated table with the type's primary properties.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.9` Filter sidebar (`P0`, `todo`)
  - Per-property filters (range, set, text contains, is-null) composed with AND/OR groups.
  - Filters compile to an object-set definition and are reflected in the URL query string.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.10` Full-text search bar (`P0`, `todo`)
  - Global search across the active object type's searchable properties; surface ranked matches with highlighted spans.
  - Honor caller permissions, markings, and restricted views.
  - Docs: [Search](https://www.palantir.com/docs/foundry/object-explorer/search).

- [ ] `ONA.11` Per-object preview pane (`P0`, `todo`)
  - Side panel rendering primary properties, applicable Actions, and a link to the full Object View for the selected row.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

## Milestone B: credible Foundry-style parity

### Dynamic Scheduling: full trigger catalog

- [ ] `ONA.12` Object-set-change trigger (`P1`, `todo`)
  - Trigger fires when an object-set definition's materialization changes (rows added, removed, or property values changed for a watched property list).
  - Debounce by a configurable settle window; coalesce bursts into a single run.
  - Docs: [Schedule triggers](https://www.palantir.com/docs/foundry/dynamic-scheduling/triggers).

- [ ] `ONA.13` Upstream build-success trigger (`P1`, `todo`)
  - Trigger fires when a watched dataset transaction or pipeline build completes successfully.
  - Optionally require all watched upstreams within a time window before firing.
  - Docs: [Schedule triggers](https://www.palantir.com/docs/foundry/dynamic-scheduling/triggers).

- [ ] `ONA.14` External webhook trigger (`P1`, `todo`)
  - Stable signed webhook URL per schedule; verify HMAC, replay window, and optional IP allowlist.
  - Surface the last 50 webhook invocations with status code, payload digest, and resulting run RID.
  - Docs: [Schedule triggers](https://www.palantir.com/docs/foundry/dynamic-scheduling/triggers).

- [ ] `ONA.15` Trigger composition (`P1`, `todo`)
  - A schedule may bind multiple triggers; each fires independently unless a join policy ("all", "any-within-window") is set.
  - Docs: [Configure a schedule](https://www.palantir.com/docs/foundry/dynamic-scheduling/configure).

### Machinery: transition guards and effects

- [ ] `ONA.16` Cedar policy guard on transitions (`P1`, `todo`)
  - Each transition may declare a Cedar policy guard evaluated against the caller principal, the object, and the requested target state.
  - Forbidden transitions return a structured "guard denied" error with the policy id.
  - Docs: [Transitions](https://www.palantir.com/docs/foundry/machinery/transitions).

- [ ] `ONA.17` Function-call guard on transitions (`P1`, `todo`)
  - Optional Functions reference that evaluates a boolean guard with read-only access to the object.
  - Function execution timeouts and failures translate to a deterministic "guard error" decision.
  - Docs: [Transitions](https://www.palantir.com/docs/foundry/machinery/transitions).

- [ ] `ONA.18` Transition effects (`P1`, `todo`)
  - On a successful transition, run a declared effect list: Action invocations, Function calls, notification emission.
  - Effects run inside the same transaction as the state write when targeting on-platform writes; external effects are best-effort with retries and dead-letter logging.
  - Docs: [Transitions](https://www.palantir.com/docs/foundry/machinery/transitions).

- [ ] `ONA.19` Machine history + audit (`P1`, `todo`)
  - Per-instance ordered history of transitions: from, to, actor, timestamp, guard decisions, effect outcomes, definition version.
  - History is immutable and reflected in the central audit trail.
  - Docs: [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines).

- [ ] `ONA.20` Machinery visualizer (`P1`, `todo`)
  - Graph view of a machine definition with states, transitions, and guard/effect badges.
  - Per-instance overlay: highlight current state and recently traversed edges.
  - Docs: [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines).

- [ ] `ONA.21` Workshop Machinery widget (`P1`, `todo`)
  - Workshop widget bound to an object variable rendering the current state, allowed transitions, and a one-click trigger control.
  - Two-way binding: widget reflects state changes triggered elsewhere.
  - Docs: [Workshop integration](https://www.palantir.com/docs/foundry/machinery/workshop-integration).

### Object Explorer: saved views and share-links

- [ ] `ONA.22` Saved view resource (`P1`, `todo`)
  - CRUD a `saved_view` resource with object type, filter spec, column list, sort order, owning project, organizations, and markings.
  - Personal vs. shared visibility; shared views require view permission on the underlying object type.
  - Docs: [Saved views](https://www.palantir.com/docs/foundry/object-explorer/saved-views).

- [ ] `ONA.23` Share-link generation (`P1`, `todo`)
  - One-click "copy link" producing a URL that resolves to the same object type, filters, columns, sort, and selection.
  - Share links honor the recipient's permissions; nothing is leaked through the link itself.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.24` Link-traversal sidebar (`P1`, `todo`)
  - For the selected object, list link types with neighbor counts; click a link to navigate to a filtered Object Explorer view of the neighbor object type.
  - Honor restricted views and markings on link traversal.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.25` Object Views integration (`P1`, `todo`)
  - Per-row "open Object View" action and per-type default Object View binding.
  - When an Object View defines a preview layout, the Object Explorer preview pane uses it.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

## Milestone C: advanced parity

### Dynamic Scheduling: SLA and backpressure

- [ ] `ONA.26` Per-schedule SLA (`P2`, `todo`)
  - Declare an SLA target (max latency from trigger to completion) per schedule.
  - Emit warning and breach signals to observability and to the schedule sidebar; flag breaching schedules in admin views.
  - Docs: [Configure a schedule](https://www.palantir.com/docs/foundry/dynamic-scheduling/configure).

- [ ] `ONA.27` Backpressure handling (`P2`, `todo`)
  - When the target queue is saturated, hold pending fires up to a bounded backlog and shed oldest fires beyond the bound.
  - Surface backlog depth and shed counts; alert above an admin-configured threshold.
  - Docs: [Dynamic Scheduling overview](https://www.palantir.com/docs/foundry/dynamic-scheduling/overview).

- [ ] `ONA.28` Run observability (`P2`, `todo`)
  - Per-schedule run history with status, duration, queue-wait time, target output RID, and a link to the executing service's logs.
  - Filter and search runs by status, time range, and triggering event id.
  - Docs: [Dynamic Scheduling overview](https://www.palantir.com/docs/foundry/dynamic-scheduling/overview).

### Machinery: branching and cross-machine references

- [ ] `ONA.29` Branched machine definitions (`P2`, `todo`)
  - Edit a machine definition on a branch with diff against main; preview branch behavior against branched object data.
  - Promote a branched definition to main through the standard proposal flow.
  - Docs: [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines).

- [ ] `ONA.30` Definition versioning + instance pinning (`P2`, `todo`)
  - Each definition has immutable versions; a deployed version is pinned to existing instances until an admin migrates them.
  - Migration job rewrites instance state pointers and validates that the current state still exists in the new version.
  - Docs: [State machines](https://www.palantir.com/docs/foundry/machinery/state-machines).

- [ ] `ONA.31` Cross-machine references (`P2`, `todo`)
  - A transition effect may request a transition on a related object's machine, subject to that machine's guards.
  - Detect and reject cycles; emit a structured "cycle blocked" event.
  - Docs: [Transitions](https://www.palantir.com/docs/foundry/machinery/transitions).

### Object Explorer: history, export, Vertex integration

- [ ] `ONA.32` Exploration history (`P2`, `todo`)
  - Per-user back/forward navigation across object-type selections, filters, and selected rows.
  - Surface recent objects and recent saved views in a quick-access list.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.33` Export selected objects to dataset (`P2`, `todo`)
  - Export the current selection or the full filtered object set to a new dataset transaction with column projection.
  - The export job runs through Resource Management queue admission and writes to a project-scoped output.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.34` Export selection to Workshop (`P2`, `todo`)
  - Push the current selection as an object set variable into a chosen Workshop module.
  - Confirm permission to write the variable; otherwise show a clear denial.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `ONA.35` Vertex integration (`P2`, `todo`)
  - "Open in Vertex" action that seeds a new Vertex analysis from the current selection or filtered object set.
  - Two-way deep link: Vertex selections can return to Object Explorer with the same selection.
  - Cross-link: [foundry-vertex-1to1-checklist.md](./foundry-vertex-1to1-checklist.md).
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current scheduler in `services/pipeline-build-service` (or the dedicated `dynamic-scheduling-service` if already extracted) and the queue admission contract with Resource Management.
- [ ] `INV.2` Identify the current ontology object-type permission, marking, and restricted-view enforcement path in `ontology-query-service`.
- [ ] `INV.3` Identify the Cedar policy evaluation entry point in `authorization-policy-service` and how it is invoked from inside a write transaction.
- [ ] `INV.4` Identify the Functions runtime invocation contract for read-only and write-effect calls.
- [ ] `INV.5` Identify the Workshop variable-binding contract used by other widgets (Vertex, Map) for reuse by the Machinery widget.
- [ ] `INV.6` Identify the audit-trail emission contract used by Actions and reuse it for machine transitions and schedule fires.
- [ ] `INV.7` Identify the saved-resource pattern used by Object Views and Vertex analyses for reuse by Object Explorer saved views.
- [ ] `INV.8` Produce a parity matrix sibling JSON entry under `foundry-feature-parity-matrix.json` once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `dynamic-scheduling-service` (or extend the scheduler in `pipeline-build-service`) | Schedule + trigger CRUD, trigger evaluation loop, run history, SLA + backpressure accounting, Resource Management admission calls, webhook signature verification. |
| `machinery-service` | Machine definition CRUD, per-object instance state, transition execution, guard evaluation calls to authorization-policy-service and Functions, effect dispatch, history + audit emission, definition versioning and instance migration. |
| `object-explorer-service` (or extend `ontology-query-service`) | Object-type listing, filter compilation to object-set definitions, full-text search, saved-view CRUD, share-link resolution, link-traversal listing, export-to-dataset job orchestration. |
| `authorization-policy-service` | Cedar guard evaluation for Machinery transitions; restricted-view enforcement for Object Explorer filters, search, and link traversal. |
| `apps/web` | Object Explorer page, Machinery designer + visualizer + Workshop widget, Dynamic Scheduling admin (schedule list, trigger config, run history, SLA dashboard). |

## Acceptance criteria for first complete Dynamic Scheduling / Machinery / Object Explorer milestone

- A builder can create a `dynamic_schedule` with a time trigger that targets an object-set materialization or a dataset build, run it manually, observe last successful run, and see the run submitted through Resource Management queue admission.
- A builder can define a `machine_definition` bound to an ontology object type with states, transitions, and an initial state; create instances by executing a transition on a real object; and read back instance state.
- A user can open Object Explorer, pick an object type, apply filters in the sidebar, run a full-text search, select a row, see a preview pane with applicable Actions, and follow a link to a related object type.
- All Dynamic Scheduling fires, machine transitions, and Object Explorer search/export operations emit audit-trail events and honor caller permissions, markings, and restricted views.
- Share-links to Object Explorer views resolve safely under the recipient's permissions and never leak data through the link itself.
- The Machinery Workshop widget reflects state changes triggered elsewhere within one polling interval.

## Test plan expectations

- Unit tests for schedule CRUD validation, cron + interval parsing, debounce/coalescing, webhook HMAC verification, machine definition validation (initial state uniqueness, transition reachability), transition execution preconditions, Cedar/Function guard wiring, filter spec compilation, saved-view permissions, and share-link resolution.
- Unit tests for backpressure bounds (shedding policy), SLA breach evaluation, instance-pinning migration validation, and cross-machine cycle detection.
- API tests for `dynamic_schedule` CRUD + run-now + run-history, trigger registration (time / object-set-change / build-success / webhook), `machine_definition` CRUD + versioning + branched promotion, instance transition execution, Object Explorer object-type listing, filter + search endpoints, saved-view CRUD, link-traversal listing, and export-to-dataset.
- Integration tests for end-to-end fire of each trigger kind landing in Resource Management admission and producing a target output, machine transitions that invoke Actions / Functions / notifications as effects, Workshop Machinery widget binding, and Object Explorer "open in Vertex" / "send to Workshop" round-trips.
- Security tests for marking-aware filter and search results, restricted-view enforcement on link traversal, Cedar guard denials returning structured error codes, webhook replay rejection, share-link recipient permission narrowing, and audit-event completeness on every transition and schedule fire.
- Load tests for many small schedules firing concurrently, bursty object-set-change triggers with debounce, high-fanout transition effects, and large object-type listings with deep filters in Object Explorer.
