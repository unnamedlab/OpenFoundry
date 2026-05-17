# Foundry Vertex 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Vertex graph
exploration app: system graphs, link traversal, neighbor expansion, scenario
planning and what-if analysis, time-aware event layers, media layers,
saved analyses, branched graphs, server-side traversal against Object Storage
V2, and integrations with Workshop, Object Views, Object Explorer, and
AIP Logic.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets,
screenshots, or any non-public behavior. The target is **functional parity
based on public Palantir Foundry documentation**: the same product concepts,
comparable workflows, compatible resource models where useful, and
OpenFoundry-native implementation details that can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native
implementation, not a pixel-perfect clone.

This checklist covers the Vertex graph application. It depends on the
Ontology checklist for object/link models, the Object Storage V2 checklist
for traversal pushdown, the Global Branching checklist for branched graphs,
and the Security/Governance checklist for permission-aware traversal. It
does not redefine those models; it specifies the graph UX and the
traversal/scenario APIs that sit on top of them.

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
| `P0` | Required for credible Vertex with neighbor expansion, layout, filtering, and saved analyses. |
| `P1` | Required for Foundry-style graph analytics: scenario planning, event timelines, media layers, branched graphs. |
| `P2` | Advanced, governance-heavy, or scale-oriented parity (graph cost insights, traversal pushdown, restricted-view enforcement on edges). |

## Official Palantir documentation library

### Product overview

- [Vertex overview](https://www.palantir.com/docs/foundry/vertex/overview)
- [Vertex application](https://www.palantir.com/docs/foundry/vertex/application)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Concepts

- [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal)
- [Scenario planning](https://www.palantir.com/docs/foundry/vertex/scenarios)
- [System graphs](https://www.palantir.com/docs/foundry/vertex/system-graphs)
- [Event timelines](https://www.palantir.com/docs/foundry/vertex/timelines)
- [Media layers](https://www.palantir.com/docs/foundry/vertex/media-layers)

### Integrations

- [Workshop Vertex embed](https://www.palantir.com/docs/foundry/workshop/widgets/vertex)
- [Object Views graph panel](https://www.palantir.com/docs/foundry/object-views/graph-panel)
- [AIP Logic graph reasoning](https://www.palantir.com/docs/foundry/logic/graph-reasoning)

## Milestone A: credible graph exploration

### Analysis resource and lifecycle

- [ ] `VTX.1` Vertex analysis resource (`P0`, `todo`)
  - CRUD a `vertex_analysis` resource with title, description, seed object set, layout state, layer configuration, scenario set, branch context, owning project, organizations, and markings.
  - Auto-save layout changes per user; explicit save creates a shared version readable by other users with view permission.
  - Stable RID and Compass-discoverable.
  - Docs: [Vertex overview](https://www.palantir.com/docs/foundry/vertex/overview), [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

- [ ] `VTX.2` Saved versions and forks (`P0`, `todo`)
  - Version a Vertex analysis on explicit save with author, timestamp, and changelog message.
  - Fork an analysis to a new owner without copying private user state.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

### Seeding and neighbor expansion

- [ ] `VTX.3` Seed selection (`P0`, `todo`)
  - Seed the graph from a single object, an object set, an Object Explorer selection, or a Workshop variable.
  - Show seed metadata in the sidebar (type, count, applied filters).
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

- [ ] `VTX.4` Neighbor expansion API (`P0`, `todo`)
  - Server endpoint that returns neighbors for a node set, filtered by link type, target object type, link properties, and hop depth (1-3 by default).
  - Page neighbors and return aggregate counts when the cap is exceeded; let the user opt into more rows.
  - Push down to Object Storage V2 indices when available.
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

- [ ] `VTX.5` Multi-hop traversal (`P0`, `todo`)
  - Support typed multi-hop traversal patterns (e.g. `Person -[owns]-> Account -[transacted]-> Person`) with property filters per hop.
  - Show traversal plan in the sidebar and warn on unbounded fan-out.
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

### Layout, filtering, styling

- [ ] `VTX.6` Layout engine (`P0`, `todo`)
  - Layouts: force-directed (cose), breadth-first, concentric, grid, hierarchical.
  - Allow per-node pinning; preserve pinned positions across re-layout.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

- [ ] `VTX.7` Filtering and grouping (`P0`, `todo`)
  - Filter nodes/edges by type, property, and degree.
  - Group nodes by type or property with collapsible group bubbles.
  - Show counts on collapsed groups.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

- [ ] `VTX.8` Node and edge styling (`P0`, `todo`)
  - Style nodes by icon, color, size, label property; style edges by color, width, dash pattern, label.
  - Style expressions reference object/edge property values.
  - Provide a per-type style preset and an analysis-level override.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

### Search and detail panel

- [ ] `VTX.9` Inline search (`P0`, `todo`)
  - Search visible graph by property or RID with keyboard shortcut focus.
  - Highlight matching nodes and pan to first match.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

- [ ] `VTX.10` Selection detail panel (`P0`, `todo`)
  - Sidebar showing selected node/edge properties, applicable Actions, link to Object View, recent timeline events, and traversal options.
  - Multi-select shows shared property summary and bulk Actions.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

## Milestone B: scenarios, timelines, media, branched graphs

### Scenarios and what-if analysis

- [ ] `VTX.11` Scenario resource (`P1`, `todo`)
  - `vertex_scenario` rows attached to an analysis with: name, description, list of staged edits (object property changes, simulated link adds/removes, Action invocations in dry-run mode).
  - Persist scenarios as branch-scoped staged edits when a branch is active; otherwise as ephemeral overlays not written to main.
  - Docs: [Scenario planning](https://www.palantir.com/docs/foundry/vertex/scenarios).

- [ ] `VTX.12` Scenario diff and impact summary (`P1`, `todo`)
  - Show diff between baseline and scenario: changed nodes, changed edges, added/removed elements, and computed metrics (degree, centrality, cluster size).
  - Highlight impacted nodes in the canvas; toggle baseline/scenario layers.
  - Docs: [Scenario planning](https://www.palantir.com/docs/foundry/vertex/scenarios).

- [ ] `VTX.13` Scenario promotion to Actions (`P1`, `todo`)
  - Convert a scenario's staged edits into a sequence of Action invocations that can be reviewed and applied on a branch or main with proper approvals.
  - Docs: [Scenario planning](https://www.palantir.com/docs/foundry/vertex/scenarios).

### Event timelines

- [ ] `VTX.14` Event timeline overlay (`P1`, `todo`)
  - Bind one or more event object types (with timestamp properties) to a timeline overlaying the graph.
  - Filter graph view to elements present at the timeline cursor.
  - Docs: [Event timelines](https://www.palantir.com/docs/foundry/vertex/timelines).

- [ ] `VTX.15` Timeline playback (`P1`, `todo`)
  - Play/pause, speed selection, range brushing, and per-event-type toggles.
  - Sync timeline cursor across multiple Vertex tabs in the same analysis.
  - Docs: [Event timelines](https://www.palantir.com/docs/foundry/vertex/timelines).

### Media layers and system graphs

- [ ] `VTX.16` Media layer overlay (`P1`, `todo`)
  - Attach images, videos, or PDFs (media set items) to nodes/edges as a side panel and inline thumbnails.
  - Respect media-set permissions and markings.
  - Docs: [Media layers](https://www.palantir.com/docs/foundry/vertex/media-layers).

- [ ] `VTX.17` System graphs (`P1`, `todo`)
  - Predefined graph templates that auto-seed and traverse common patterns (e.g., supply chain, fraud rings, infrastructure dependencies).
  - Template registry with versioning and per-org enablement.
  - Docs: [System graphs](https://www.palantir.com/docs/foundry/vertex/system-graphs).

### Branched graphs and Workshop embed

- [ ] `VTX.18` Branch-aware analysis (`P1`, `todo`)
  - Honor the active branch from the Global Branching taskbar: traversal reads branched object/link versions when set, otherwise main.
  - Mark analyses opened on a non-main branch with a banner and forbid promote-to-main without proposal flow.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

- [ ] `VTX.19` Workshop Vertex widget (`P1`, `todo`)
  - Workshop widget bound to an object set variable that mirrors the standalone Vertex with read-only or full-edit toggles.
  - Two-way binding for selection and hovered element.
  - Docs: [Workshop Vertex embed](https://www.palantir.com/docs/foundry/workshop/widgets/vertex).

- [ ] `VTX.20` Object View graph panel (`P1`, `todo`)
  - Reusable graph panel for Object Views seeded from the current object with a configurable hop budget.
  - Docs: [Object Views graph panel](https://www.palantir.com/docs/foundry/object-views/graph-panel).

## Milestone C: scale, governance, AIP

### Pushdown and cost insights

- [ ] `VTX.21` Traversal pushdown to Object Storage V2 (`P2`, `todo`)
  - Translate neighbor and multi-hop queries into Object Storage V2 link-index lookups.
  - Avoid scanning the full object set; emit `EXPLAIN` plans on demand for power users.
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

- [ ] `VTX.22` Graph cost insights (`P2`, `todo`)
  - Surface estimated and actual cost (CPU·s, rows scanned, indices hit) per expansion in the sidebar.
  - Throttle expansions that exceed an analysis-level budget; require explicit user confirmation to continue.
  - Docs: [Vertex application](https://www.palantir.com/docs/foundry/vertex/application).

### Governance

- [ ] `VTX.23` Permission and marking enforcement on traversal (`P2`, `todo`)
  - Every neighbor expansion enforces the caller's clearances and link-level permissions.
  - Hidden neighbors are reported as opaque counts ("12 neighbors not visible") rather than silently dropped.
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

- [ ] `VTX.24` Restricted-view enforcement on edges (`P2`, `todo`)
  - Restricted views applied to link types filter edges per caller.
  - Vertex never returns an edge that the caller cannot see in Object Explorer.
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

### AIP integration

- [ ] `VTX.25` AIP Logic graph reasoning blocks (`P2`, `todo`)
  - Expose neighbor expansion, path-finding, and centrality as AIP Logic blocks consumable by Agents.
  - Each block enforces caller permissions; agents do not bypass restricted views.
  - Docs: [AIP Logic graph reasoning](https://www.palantir.com/docs/foundry/logic/graph-reasoning).

- [ ] `VTX.26` Path-finding and centrality measures (`P2`, `todo`)
  - Shortest path, k-shortest paths, betweenness, eigenvector centrality computed over the current analysis subgraph.
  - Cache results keyed by subgraph hash.
  - Docs: [Graphs and traversal](https://www.palantir.com/docs/foundry/vertex/graphs-and-traversal).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current Vertex frontend route and the libraries used for rendering (Cytoscape, sigma, etc.).
- [ ] `INV.2` Identify the neighbor expansion API in the ontology-query/object-database services.
- [ ] `INV.3` Identify the link-index status in Object Storage V2.
- [ ] `INV.4` Identify the branch-aware traversal contract with the dataset-versioning service.
- [ ] `INV.5` Identify the marking-aware filter path for traversal results.
- [ ] `INV.6` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `vertex-service` | Analysis + scenario + saved-version CRUD, layout state, timeline state, expansion-cost accounting. |
| `ontology-query-service` | Neighbor expansion, multi-hop traversal, path-finding, centrality measures, with Object Storage V2 pushdown. |
| `object-storage-v2` | Link indices, spatial indices when geo properties are involved, permission-aware filters. |
| `media-sets-service` | Media-layer fetch with marking enforcement. |
| `apps/web` | Vertex app shell, sidebar, scenario panel, timeline, media overlay, Workshop embed, Object View graph panel. |
