# Foundry Pilot, Autopilot, Solution Designer, Workflow Lineage, and use-case enablement 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's use-case
development surfaces that are not owned by other migration checklists:
**Pilot** (operational what-if piloting over ontology objects with
embedded analytics), **Autopilot** (agentic long-running workflow
execution and policy-bound action proposals), **Solution Designer**
(visual canvas that composes datasets, ontology, transforms, agents,
dashboards, and applications into a single solution map exportable to
Marketplace), **Workflow Lineage** (cross-application lineage report,
delete-impact preview, cycle detection, branch-aware lineage and
lineage diff for promotion), and **Use-case enablement** (templates,
walkthroughs, adoption metrics, recommended workflow steps).

> **Scope distinction.** This file collects four otherwise-unowned
> use-case workflow surfaces. **Workshop** stays in
> [foundry-workshop-pipeline-1to1-checklist.md](./foundry-workshop-pipeline-1to1-checklist.md);
> **Slate / Carbon** stays in
> [foundry-slate-carbon-1to1-checklist.md](./foundry-slate-carbon-1to1-checklist.md);
> **Automate** stays in
> [foundry-automate-rules-1to1-checklist.md](./foundry-automate-rules-1to1-checklist.md).
> The Workflow Lineage section here cross-links to the
> data-foundation P2 handoff in
> [foundry-data-foundation-1to1-checklist.md](./foundry-data-foundation-1to1-checklist.md)
> and promotes it into its own owner here.

This document is intentionally implementation-oriented. It does not
attempt to clone Palantir branding, private source code, proprietary
assets, screenshots, or any non-public behavior. The target is
**functional parity based on public Palantir Foundry documentation**:
the same product concepts, comparable workflows, compatible resource
models where useful, and OpenFoundry-native implementation details
that can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir
documentation, but contributors must not copy private source,
decompile bundles, import tenant-specific exports, use Palantir
branding, or reuse proprietary assets. The product target is
functional parity in an OpenFoundry-native implementation, not a
pixel-perfect clone.

This checklist depends on the Ontology checklist for object/link
models, the Object Storage V2 checklist for scenario / pilot reads,
the Global Branching checklist for what-if branches, the Marketplace
checklist for Solution Designer export, the Security/Governance
checklist for permission-aware lineage and agent policies, and the
AIP Logic / Functions / Automate checklists for Autopilot
integrations. It does not redefine those models; it specifies the use
case orchestration and lineage UX that sit on top of them.

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
| `P0` | Required for credible Pilot/Autopilot/Solution Designer/Workflow Lineage parity: resource CRUD, scenario read, agent CRUD, canvas nodes, cross-app lineage read, template list. |
| `P1` | Required for Foundry-style parity: pilot what-if branches and KPI panels, autopilot policy and observation loop, Marketplace export, lineage diff and delete-impact, walkthrough resource. |
| `P2` | Advanced parity: pilot action proposals, autopilot feedback learning, solution readiness and cost rollup, branch-aware lineage, adoption metrics dashboards. |

## Official Palantir documentation library

### Pilot

- [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview)
- [Pilot scenarios](https://www.palantir.com/docs/foundry/pilot/scenarios)

### Autopilot

- [Autopilot overview](https://www.palantir.com/docs/foundry/autopilot/overview)
- [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow)

### Solution Designer

- [Solution Designer overview](https://www.palantir.com/docs/foundry/solution-designer/overview)
- [Solution Designer canvas](https://www.palantir.com/docs/foundry/solution-designer/canvas)

### Workflow Lineage

- [Workflow Lineage overview](https://www.palantir.com/docs/foundry/workflow-lineage/overview)
- [Cross-app lineage report](https://www.palantir.com/docs/foundry/workflow-lineage/cross-app-report)

### Use-case enablement

- [Use-case lifecycle overview](https://www.palantir.com/docs/foundry/use-case-lifecycle/overview)
- [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs)
- [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption)

## Milestone A: minimum viable Pilot/Autopilot/Solution Designer/Workflow Lineage parity

### Pilot resource and scenarios

- [ ] `PAS.1` Pilot resource (`P0`, `todo`)
  - CRUD a `pilot` resource with title, description, seed object set, KPI binding list, scenario set, branch context, owning project, organizations, and markings.
  - Stable RID, Compass-discoverable, view/edit permissions follow project rules.
  - Docs: [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview).

- [ ] `PAS.2` Pilot scenario read (`P0`, `todo`)
  - `pilot_scenario` rows attached to a pilot with: name, description, list of staged property edits and simulated action invocations (read-only in milestone A).
  - Server returns a `baseline | scenario` projection for the bound object set, leaving main untouched.
  - Docs: [Pilot scenarios](https://www.palantir.com/docs/foundry/pilot/scenarios).

- [ ] `PAS.3` Pilot embedded analytics panel (`P0`, `todo`)
  - Render simple KPI tiles (count, sum, avg, min, max, ratio) computed over the bound object set in baseline mode.
  - Tiles bind to ontology aggregations or saved Quiver / Contour views.
  - Docs: [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview).

- [ ] `PAS.4` Pilot cohort selection (`P0`, `todo`)
  - Filter the bound object set into named cohorts (typed object set filters) shown side-by-side in the KPI panel.
  - Persist cohort definitions on the pilot resource.
  - Docs: [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview).

### Autopilot agent CRUD

- [ ] `PAS.5` Autopilot agent resource (`P0`, `todo`)
  - CRUD an `autopilot_agent` resource with name, owning project, bound object type or object set, trigger schedule, AIP Logic / Function reference, status (`enabled | paused | disabled`), and policy reference.
  - Stable RID, Compass-discoverable, organizations and markings honored.
  - Docs: [Autopilot overview](https://www.palantir.com/docs/foundry/autopilot/overview).

- [ ] `PAS.6` Autopilot run history (`P0`, `todo`)
  - Persist `autopilot_run` rows: agent RID, started_at, finished_at, status, observed object set hash, proposed actions, applied actions, error if any.
  - Expose paginated read API and a per-agent timeline view.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

- [ ] `PAS.7` Autopilot manual trigger (`P0`, `todo`)
  - `POST /autopilot/agents/{rid}/run` invokes one synchronous run with policy in `dry_run` mode by default; result is a run row plus a list of proposed actions.
  - Permission check: caller must hold `autopilot.run` on the project.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

### Solution Designer canvas and nodes

- [ ] `PAS.8` Solution resource (`P0`, `todo`)
  - CRUD a `solution` resource with name, owning project, list of nodes, list of edges, canvas layout state, readiness summary, and Marketplace export reference (P1+).
  - Compass-discoverable, stable RID, version-on-save.
  - Docs: [Solution Designer overview](https://www.palantir.com/docs/foundry/solution-designer/overview).

- [ ] `PAS.9` Solution node taxonomy (`P0`, `todo`)
  - Supported node kinds in milestone A: `dataset`, `ontology_type`, `transform`, `agent`, `dashboard`, `application`.
  - Each node carries a kind, target RID, label, position, and arbitrary node-level config bag.
  - Docs: [Solution Designer canvas](https://www.palantir.com/docs/foundry/solution-designer/canvas).

- [ ] `PAS.10` Solution edge model (`P0`, `todo`)
  - Edges encode `produces | consumes | references | invokes` semantics with source/target node IDs and edge-level config.
  - Validation rejects cycles in `produces`/`consumes` subgraphs.
  - Docs: [Solution Designer canvas](https://www.palantir.com/docs/foundry/solution-designer/canvas).

- [ ] `PAS.11` Canvas auto-layout (`P0`, `todo`)
  - Server returns an initial layered layout (left-to-right) when no user layout is saved; frontend free-form drag persists per-node positions.
  - Docs: [Solution Designer canvas](https://www.palantir.com/docs/foundry/solution-designer/canvas).

### Workflow Lineage cross-app read

- [ ] `PAS.12` Cross-app lineage API (`P0`, `todo`)
  - `GET /workflow-lineage/{rid}` returns upstream and downstream nodes spanning datasets, transforms, ontology types, Workshop modules, Slate / Quiver views, and dashboards.
  - Each node row includes kind, RID, label, and pagination cursor for further expansion.
  - Docs: [Workflow Lineage overview](https://www.palantir.com/docs/foundry/workflow-lineage/overview).

- [ ] `PAS.13` Cross-app lineage UI (`P0`, `todo`)
  - Frontend explorer with collapsible upstream/downstream panels, type filter, RID search, and click-through into the owning app.
  - Permission-aware: nodes the caller cannot see are summarized as opaque "N hidden" counts.
  - Docs: [Cross-app lineage report](https://www.palantir.com/docs/foundry/workflow-lineage/cross-app-report).

### Use-case template registry

- [ ] `PAS.14` Use-case template list (`P0`, `todo`)
  - `GET /enablement/use-case-templates` returns curated templates: title, summary, ontology types involved, recommended apps (Pilot, Workshop, Slate, Vertex, Autopilot), and required permissions.
  - Templates are read-only platform resources, seeded by admins, scoped per organization.
  - Docs: [Use-case lifecycle overview](https://www.palantir.com/docs/foundry/use-case-lifecycle/overview).

- [ ] `PAS.15` Template instantiation stub (`P0`, `todo`)
  - `POST /enablement/use-case-templates/{id}/instantiate` creates an empty project plus a `solution` resource pre-populated with the template's recommended node skeleton.
  - No deep cloning of datasets/transforms in milestone A; just a placeholder graph.
  - Docs: [Use-case lifecycle overview](https://www.palantir.com/docs/foundry/use-case-lifecycle/overview).

## Milestone B: credible Foundry-style parity

### Pilot what-if branches and KPI panels

- [ ] `PAS.16` Pilot what-if branch binding (`P1`, `todo`)
  - When a branch is active, pilot scenarios write their staged edits as branch-scoped object/property versions instead of ephemeral overlays.
  - Pilot UI shows the active branch banner; readers without branch access fall back to baseline.
  - Docs: [Pilot scenarios](https://www.palantir.com/docs/foundry/pilot/scenarios).

- [ ] `PAS.17` Pilot KPI panel with diff (`P1`, `todo`)
  - KPI tiles render baseline vs scenario values with delta, percent change, and a sparkline over a configurable time window.
  - Persist KPI panel definitions on the pilot resource; share via project link.
  - Docs: [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview).

- [ ] `PAS.18` Pilot goal tracking (`P1`, `todo`)
  - Per-pilot goal entries: KPI reference, target value, deadline, status (`on_track | at_risk | off_track`) computed from latest KPI reading.
  - Surface in the pilot landing page and the Compass card.
  - Docs: [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview).

- [ ] `PAS.19` Workshop / Slate pilot embed (`P1`, `todo`)
  - Workshop widget and Slate component bound to a pilot RID render the pilot's KPI panel and current scenario in read-only mode.
  - Docs: [Pilot overview](https://www.palantir.com/docs/foundry/pilot/overview).

### Autopilot policy and observation loop

- [ ] `PAS.20` Autopilot policy resource (`P1`, `todo`)
  - `autopilot_policy` resource: scope (object types, project), allowed actions, dry-run vs apply mode, rate limit, business-hours window, and approval requirement.
  - Bound to an agent at create time; updates require re-approval if the policy widens.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

- [ ] `PAS.21` Autopilot scheduled observation loop (`P1`, `todo`)
  - Worker that wakes per agent schedule, reads the bound object set, invokes the agent's AIP Logic / Function, and either proposes or applies actions per policy.
  - Loop is idempotent: a run id is computed from agent rid + object-set hash + timestamp bucket.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

- [ ] `PAS.22` Autopilot action proposal queue (`P1`, `todo`)
  - When policy says `propose_only`, proposed actions land in a per-agent queue with reviewer assignment, approve/reject/comment actions, and audit trail.
  - Auto-expire proposals after a configurable TTL.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

- [ ] `PAS.23` Autopilot Automate trigger (`P1`, `todo`)
  - Automate rule may trigger an Autopilot run on object set change; failing runs surface in Automate's failure log.
  - Reverse direction: Autopilot may register an Automate rule via API to schedule itself.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

### Solution Designer Marketplace export

- [ ] `PAS.24` Marketplace export descriptor (`P1`, `todo`)
  - `POST /solution-designer/{rid}/export` produces a Marketplace product descriptor referencing the solution's nodes, edges, declared parameters, and required permissions.
  - Descriptor is the input to the Marketplace publish flow (cross-link).
  - Docs: [Solution Designer overview](https://www.palantir.com/docs/foundry/solution-designer/overview).

- [ ] `PAS.25` Solution parameter declarations (`P1`, `todo`)
  - Nodes may declare inputs (dataset binding, ontology type binding, marking choice); the solution gathers them as solution-level parameters.
  - Marketplace install resolves parameters at install time.
  - Docs: [Solution Designer canvas](https://www.palantir.com/docs/foundry/solution-designer/canvas).

- [ ] `PAS.26` Solution diff against installed copy (`P1`, `todo`)
  - When a solution is installed from Marketplace, the source canvas can diff against the installed instance and surface drift.
  - Docs: [Solution Designer overview](https://www.palantir.com/docs/foundry/solution-designer/overview).

### Workflow Lineage diff and delete-impact

- [ ] `PAS.27` Lineage diff for promotion (`P1`, `todo`)
  - Compute the lineage subgraph reachable from a branch's changed resources and diff against main; flag added/removed/changed nodes.
  - Surface in the Global Branching promotion review UI.
  - Docs: [Cross-app lineage report](https://www.palantir.com/docs/foundry/workflow-lineage/cross-app-report).

- [ ] `PAS.28` Delete-impact preview (`P1`, `todo`)
  - Before deleting a dataset, ontology type, transform, or dashboard, compute downstream consumers and require confirmation if any exist.
  - Show a tree of impacted nodes with click-through.
  - Docs: [Workflow Lineage overview](https://www.palantir.com/docs/foundry/workflow-lineage/overview).

- [ ] `PAS.29` Cycle detection (`P1`, `todo`)
  - Lineage indexer flags cycles introduced by config changes (e.g., transform A reads B which writes A) and exposes them as a workflow lineage alert.
  - Docs: [Workflow Lineage overview](https://www.palantir.com/docs/foundry/workflow-lineage/overview).

### Walkthrough resource

- [ ] `PAS.30` Walkthrough resource (`P1`, `todo`)
  - `walkthrough` resource: title, ordered steps, per-step target (route, anchor, or RID), per-step prose, completion criteria.
  - Per-user progress state recorded server-side; resume from last step.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

- [ ] `PAS.31` Walkthrough integration with Custom documentation (`P1`, `todo`)
  - Custom documentation pages may embed walkthrough triggers; clicking starts the walkthrough overlay in the target app.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

## Milestone C: advanced parity

### Pilot action proposals from UI

- [ ] `PAS.32` Pilot action proposals (`P2`, `todo`)
  - From the pilot UI, convert a scenario's staged edits into a sequence of Action invocations queued for approval against main or a target branch.
  - Approval workflow reuses Actions' existing approver model.
  - Docs: [Pilot scenarios](https://www.palantir.com/docs/foundry/pilot/scenarios).

- [ ] `PAS.33` Pilot scenario fork (`P2`, `todo`)
  - Fork a scenario into a new pilot for parallel investigation without copying private user state; lineage records the fork relationship.
  - Docs: [Pilot scenarios](https://www.palantir.com/docs/foundry/pilot/scenarios).

### Autopilot feedback learning

- [ ] `PAS.34` Autopilot outcome feedback loop (`P2`, `todo`)
  - For each applied action, the agent records a measurable outcome (KPI delta, downstream event) within a configurable observation window.
  - Outcomes are surfaced in the agent timeline and exposed via API for offline analysis.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

- [ ] `PAS.35` Autopilot reviewer signals (`P2`, `todo`)
  - Approve / reject decisions on proposed actions feed back into the agent's prompt context as labeled examples; future runs reference them.
  - No private model training; just retrieval-style few-shot context.
  - Docs: [Autopilot agent workflow](https://www.palantir.com/docs/foundry/autopilot/agent-workflow).

### Solution readiness tracking and cost rollup

- [ ] `PAS.36` Per-node readiness (`P2`, `todo`)
  - Each solution node carries a readiness state (`missing | draft | in_review | production`) computed from the target resource's own state and from required upstream readiness.
  - Solution-level readiness aggregates node states with a worst-of-N rule.
  - Docs: [Solution Designer overview](https://www.palantir.com/docs/foundry/solution-designer/overview).

- [ ] `PAS.37` Solution cost rollup (`P2`, `todo`)
  - Aggregate dataset storage, transform compute, and agent runtime costs across nodes into a per-solution rollup; surface on the canvas as a budget badge.
  - Docs: [Solution Designer overview](https://www.palantir.com/docs/foundry/solution-designer/overview).

### Workflow Lineage branch-aware report

- [ ] `PAS.38` Branch-aware lineage (`P2`, `todo`)
  - Lineage view honors the active branch: branched object types, datasets, and transforms show their branch versions and clearly mark divergence from main.
  - Docs: [Cross-app lineage report](https://www.palantir.com/docs/foundry/workflow-lineage/cross-app-report).

- [ ] `PAS.39` Lineage drill into pilot / autopilot (`P2`, `todo`)
  - Pilot, Autopilot agent, and Solution Designer nodes appear as lineage citizens with the right kind tag and click-through into their app.
  - Docs: [Workflow Lineage overview](https://www.palantir.com/docs/foundry/workflow-lineage/overview).

### Adoption metrics dashboard

- [ ] `PAS.40` Adoption metrics collection (`P2`, `todo`)
  - Per-org counters for active users, active projects, walkthroughs completed, templates instantiated, pilots created, agents enabled, solutions exported.
  - Counters update from existing audit/event streams; no new client-side telemetry.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

- [ ] `PAS.41` Adoption dashboard (`P2`, `todo`)
  - Read-only dashboard for org admins surfacing the counters above plus per-app trend lines and top use-case templates by instantiations.
  - Honors org / project / marking permissions on the underlying audit stream.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

- [ ] `PAS.42` Recommended next steps (`P2`, `todo`)
  - Per-project recommendation list: based on the project's current solution graph and walkthrough progress, suggest the next walkthrough, pilot template, or autopilot agent to set up.
  - Suggestions are deterministic rules in milestone C; no model calls.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the existing branching, scenario-staging, and object-set projection surface to reuse for Pilot what-if reads.
- [ ] `INV.2` Identify the AIP Logic / Functions invocation surface that Autopilot will call, and the Automate trigger registry.
- [ ] `INV.3` Identify the action-proposal queue model already used by Workshop / Actions and whether Autopilot and Pilot can share it.
- [ ] `INV.4` Identify the Marketplace export descriptor schema and the Compass resource indexer hooks Solution Designer must populate.
- [ ] `INV.5` Identify the current lineage indexer surface (dataset → transform → ontology) and the deltas required to span Workshop / Slate / Pilot / Autopilot nodes.
- [ ] `INV.6` Identify the audit / event stream OpenFoundry already exposes for adoption counters, to avoid new telemetry.
- [ ] `INV.7` Identify the Custom documentation page model and its embed contract for walkthrough triggers.
- [ ] `INV.8` Identify the `apps/web` route shells that need to host Pilot, Autopilot console, Solution canvas, and Lineage explorer.
- [ ] `INV.9` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `pilot-service` | Pilot + scenario + KPI panel + cohort + goal CRUD, scenario projection, action-proposal handoff to Actions, Workshop / Slate embed APIs. |
| `autopilot-service` | Agent + policy + run + proposal queue CRUD, scheduled observation loop, AIP Logic / Function invocation, outcome feedback recording. |
| `solution-designer-service` | Solution + node + edge CRUD, canvas layout, readiness rollup, cost rollup, Marketplace export descriptor, drift diff. |
| `workflow-lineage-service` (or extend `lineage-service`) | Cross-app lineage indexer, lineage diff, delete-impact preview, cycle detection, branch-aware traversal. |
| `enablement-service` | Use-case templates, walkthrough resource + per-user progress, recommended-next-step rules, adoption counter rollup. |
| `apps/web` | Pilot UI, Autopilot console, Solution canvas, Lineage explorer, walkthrough overlay, adoption dashboard. |

## Acceptance criteria

- All `P0` items have a passing unit test and at least one integration test against the relevant service (`pilot-service`, `autopilot-service`, `solution-designer-service`, `workflow-lineage-service`, `enablement-service`).
- All `P0` items expose stable RIDs and are Compass-discoverable with view/edit permissions wired to project rules, organizations, and markings.
- All `P1` items have an end-to-end smoke test that exercises the UI route plus the backing API (pilot what-if branch, autopilot dry-run with policy, Marketplace export descriptor, lineage diff, walkthrough resume).
- Workflow Lineage results are permission-aware: hidden nodes appear as opaque counts and the caller cannot infer their RID, kind, or label.
- Autopilot runs are idempotent on `(agent_rid, object_set_hash, schedule_bucket)` and emit one audit event per run.
- Solution Designer export descriptors round-trip: export then re-import produces the same node/edge graph (modulo RID rebinding) under a Marketplace install.
- Adoption counters draw exclusively from existing audit/event streams; no new client-side telemetry is introduced.

## Test plan expectations

- Unit tests in each new service cover resource CRUD, validation (cycle rejection, policy widening checks), and permission enforcement.
- Integration tests use the `integration` build tag and `libs/testing` testcontainers helpers for Postgres-backed resource state.
- End-to-end tests in `apps/web` (Vitest + Playwright where present) cover: create a pilot from a template, run a what-if scenario on a branch, queue and approve an Autopilot proposal, export a solution to a Marketplace descriptor, traverse cross-app lineage with at least one hidden node, complete a two-step walkthrough.
- Contract tests verify the lineage indexer's node-kind taxonomy stays in sync with the Workshop / Slate / Pilot / Autopilot resource registries.
- Security review (per `CLAUDE.md`) is required when Autopilot policy evaluation, restricted-view-aware lineage, or Marketplace export descriptors change.
