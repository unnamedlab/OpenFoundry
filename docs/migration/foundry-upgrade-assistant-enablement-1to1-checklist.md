# Foundry Upgrade Assistant, Flow Capture, Adoption, and Enablement 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's enablement and
platform-evolution surfaces: the Upgrade Assistant that detects deprecated
APIs and resource shapes and generates per-resource migration plans with
dry-run, batched apply, and rollback; Flow Capture record-and-replay for
end-user workflows across Workshop, Slate, Object Explorer and Vertex;
organization-level Foundry adoption metrics and leaderboards; per-tenant
Custom documentation sites with TOC, embedded resources, and publication
workflow; in-product Walkthroughs with step targeting and completion
tracking; the Use case examples catalog and the use-case lifecycle and
pattern library; and the Consumer mode read-only portal for external
viewers.

> **Scope distinction.** This file collects the enablement and
> platform-admin surfaces outside core security/governance and resource
> management. Retention windows, email admin templates, and
> organization settings live in
> [`foundry-resource-management-1to1-checklist.md`](./foundry-resource-management-1to1-checklist.md).
> Approvals, Checkpoints, and the Sensitive Data Scanner are owned by
> [`foundry-approvals-checkpoints-sensitive-1to1-checklist.md`](./foundry-approvals-checkpoints-sensitive-1to1-checklist.md).
> The Marketplace product format consumed by the Upgrade Assistant's
> re-publish step is owned by
> [`foundry-devops-marketplace-1to1-checklist.md`](./foundry-devops-marketplace-1to1-checklist.md).

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
| `P0` | Required for minimum viable enablement: Upgrade Assistant detection + dry-run, Custom documentation CRUD, Walkthrough resource, Use-case template registry, Consumer mode toggle. |
| `P1` | Required for credible Foundry-style enablement: batched apply with rollback, Flow Capture record-and-replay, adoption dashboard, walkthrough completion tracking. |
| `P2` | Advanced parity: code-mod integration with Code Repositories, Flow Capture sharing and redaction, per-team adoption rollups, full lifecycle phases and pattern library, branded consumer portal. |

## Official Palantir documentation library

### Upgrade Assistant

- [Upgrade Assistant overview](https://www.palantir.com/docs/foundry/upgrade-assistant/overview)
- [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans)

### Flow Capture

- [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture)

### Foundry adoption

- [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption)

### Custom documentation

- [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation)

### Walkthroughs

- [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs)

### Use case lifecycle and patterns

- [Use case examples](https://www.palantir.com/docs/foundry/enablement/use-case-examples)
- [Use case lifecycle](https://www.palantir.com/docs/foundry/enablement/use-case-lifecycle)
- [Use case patterns](https://www.palantir.com/docs/foundry/enablement/use-case-patterns)

### Consumer mode

- [Consumer mode](https://www.palantir.com/docs/foundry/enablement/consumer-mode)

## Milestone A: minimum viable Upgrade Assistant + enablement parity

### Upgrade Assistant detection and dry-run

- [ ] `UAE.1` Deprecation rule registry (`P0`, `todo`)
  - Versioned registry of deprecation rules, each declaring: rule id, target resource kind, detector (predicate over resource manifest, SDK call AST, RID scheme, or auth pattern), severity, remediation template, since-version, removal-version.
  - Rules are loaded from `libs/upgrade-rules/` at startup and exposed read-only over an API.
  - Docs: [Upgrade Assistant overview](https://www.palantir.com/docs/foundry/upgrade-assistant/overview).

- [ ] `UAE.2` Resource scan job (`P0`, `todo`)
  - Per-organization scan that enumerates every Compass-discoverable resource (datasets, pipelines, Workshop modules, Slate apps, Functions, Code Repositories, Marketplace products) and evaluates every active deprecation rule against each resource.
  - Emits findings into `upgrade_finding` with rule id, resource RID, detected version, recommended remediation, and detection timestamp.
  - Docs: [Upgrade Assistant overview](https://www.palantir.com/docs/foundry/upgrade-assistant/overview).

- [ ] `UAE.3` Migration plan resource (`P0`, `todo`)
  - A `migration_plan` groups a set of findings into an executable plan: ordered steps, per-step dependencies, per-step rollback hook, owner, target completion date, status (`draft`/`ready`/`applying`/`applied`/`rolled_back`/`failed`).
  - Stable RID, Compass-discoverable, audited.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

- [ ] `UAE.4` Per-resource diff preview (`P0`, `todo`)
  - For every step the plan can render a structured diff (before/after manifest, before/after code snippet, before/after RID scheme) without applying the change.
  - Diff is anchored to a content hash so re-running the preview after upstream drift surfaces a conflict.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

- [ ] `UAE.5` Dry-run apply (`P0`, `todo`)
  - The full plan can be executed in dry-run mode: each step's resolver runs, produces the new manifest, and validates it against the target resource service without writing.
  - Dry-run output lists per-step `would_succeed`/`would_fail` with the failure reason.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

### Custom documentation site and document CRUD

- [ ] `UAE.6` Documentation site resource (`P0`, `todo`)
  - CRUD a `doc_site` per organization with: name, slug, owning project, default landing page, theme tokens, search-index inclusion flag.
  - One organization may host multiple sites (e.g. internal handbook + external partner site).
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

- [ ] `UAE.7` Document resource (`P0`, `todo`)
  - CRUD a `doc_page` with: title, markdown body, parent doc site, parent section, slug, status (`draft`/`published`), authored-by, last-edited-at.
  - Stable RID, Compass-discoverable, governed by the same marking/role checks as any Compass resource.
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

- [ ] `UAE.8` TOC / space structure (`P0`, `todo`)
  - Doc sites carry a hierarchical TOC of sections and pages; reorder by drag in the admin UI; serialized as a tree document with stable section ids.
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

- [ ] `UAE.9` Embedded resource blocks (`P0`, `todo`)
  - Markdown supports embed directives that resolve a Compass RID into a live preview block (dataset preview, object preview, dashboard, Slate app frame); embeds inherit the viewer's view requirements rather than the author's.
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

### Walkthrough resource and entry triggers

- [ ] `UAE.10` Walkthrough resource (`P0`, `todo`)
  - CRUD a `walkthrough` with: title, target app (`workshop`/`slate`/`object-explorer`/`vertex`/`compass`/`code-repos`), ordered steps, version, status (`draft`/`published`/`retired`).
  - Each step declares: title, body markdown, target selector (CSS selector or named anchor exposed by the target app), advance trigger (`click`/`navigation`/`manual`), optional gate predicate.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

- [ ] `UAE.11` Entry trigger registry (`P0`, `todo`)
  - Walkthroughs declare entry triggers: route match, feature flag, user role, first-time-seeing-this-app flag, manual launch from a docs link, or auto-launch on opening a use-case template.
  - The web client evaluates triggers on every route change and offers (not forces) the walkthrough.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

### Use-case template registry

- [ ] `UAE.12` Use-case template resource (`P0`, `todo`)
  - CRUD a `use_case_template` with: name, description, owning vertical/tag set, contained artifact manifest (datasets, ontology snippets, Workshop pages, Slate apps, AIP Logic flows), required permissions, target environment hints.
  - Stable RID, Compass-discoverable, governed by markings.
  - Docs: [Use case examples](https://www.palantir.com/docs/foundry/enablement/use-case-examples).

- [ ] `UAE.13` Template instantiation API (`P0`, `todo`)
  - `POST /use-case-templates/{rid}/instantiate` provisions a new project (or scopes into an existing project) with the template's artifacts, rewriting embedded RIDs to the new project's scope and recording the template+version on the resulting `use_case_instance`.
  - Docs: [Use case examples](https://www.palantir.com/docs/foundry/enablement/use-case-examples).

### Consumer mode

- [ ] `UAE.14` Consumer mode toggle (`P0`, `todo`)
  - Per-organization flag `consumer_mode_enabled` plus a per-user `is_consumer` claim; combination forces a restricted UI: no edit affordances, no admin surfaces, no Code Repositories, no Pipeline Builder, no Vertex authoring.
  - Toggle is recorded in the audit trail and emits a tenancy event.
  - Docs: [Consumer mode](https://www.palantir.com/docs/foundry/enablement/consumer-mode).

- [ ] `UAE.15` Restricted resource set (`P0`, `todo`)
  - Consumer mode exposes only resources of kind: published Workshop modules, published Slate apps, published doc pages, published dashboards, and Object Explorer views explicitly marked `consumer_visible`.
  - Any other RID returns `404 NotFound` to a consumer-mode session even if the underlying ACL would permit it.
  - Docs: [Consumer mode](https://www.palantir.com/docs/foundry/enablement/consumer-mode).

## Milestone B: credible Foundry-style enablement parity

### Upgrade Assistant batched apply and rollback

- [ ] `UAE.16` Batched apply executor (`P1`, `todo`)
  - Applies a migration plan in dependency order with bounded concurrency per resource kind; per-step progress reported as a Pulse-compatible job.
  - Each step records a `migration_step_run` with start, end, result, before-hash, after-hash, and the rollback artifact.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

- [ ] `UAE.17` Per-step rollback (`P1`, `todo`)
  - Every applied step persists a reversible rollback artifact (the previous manifest + a back-mod recipe); `POST /migration-plans/{rid}/steps/{id}/rollback` restores the prior state if the resource still matches the after-hash.
  - Rollback emits its own audit event and is itself reversible (re-apply).
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

- [ ] `UAE.18` Plan audit trail (`P1`, `todo`)
  - Every plan creation, edit, dry-run, apply, partial failure, and rollback emits an audit event with actor, plan RID, step id, before/after content hashes, and outcome.
  - Surfaced in the security/governance audit explorer.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

### Flow Capture record-and-replay

- [ ] `UAE.19` Flow capture recorder (`P1`, `todo`)
  - Browser-side recorder captures: route changes, named user actions (`click`, `input`, `select`, `submit`), target selector path, app context (`workshop`/`slate`/`object-explorer`/`vertex`), and screenshot thumbnails at step boundaries.
  - Records are serialized as a `flow_capture` resource with timeline metadata; raw DOM is not retained, only the named-action event log.
  - Docs: [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture).

- [ ] `UAE.20` Cross-app capture stitching (`P1`, `todo`)
  - A single capture can span Workshop, Slate, Object Explorer, and Vertex; the recorder tags each event with the originating app context and resolves cross-app navigation as continuous flow segments rather than separate captures.
  - Docs: [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture).

- [ ] `UAE.21` Replay as automated test (`P1`, `todo`)
  - A capture can be exported as a deterministic replay script that re-issues the recorded actions in order; the runtime asserts that each step's target selector still resolves and surfaces drift.
  - Replay results stored as `flow_capture_run` with pass/fail per step.
  - Docs: [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture).

- [ ] `UAE.22` Replay as walkthrough generation (`P1`, `todo`)
  - A capture can be promoted into a draft walkthrough: each captured step becomes a walkthrough step with the recorded selector and a generated body; the author then edits copy and publishes.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

### Foundry adoption dashboard

- [ ] `UAE.23` Active-user metric (`P1`, `todo`)
  - Aggregate DAU/WAU/MAU per organization, broken down by app surface (Workshop, Slate, Object Explorer, AIP, Pipeline Builder, Code Repositories) sourced from the audit event stream.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

- [ ] `UAE.24` Application and dataset coverage (`P1`, `todo`)
  - Aggregate counts of: projects with at least one Workshop module, projects with at least one Slate app, projects with at least one published ontology object type, projects with at least one AIP Logic flow; trend over 90 days.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

- [ ] `UAE.25` Ontology and AIP uptake (`P1`, `todo`)
  - Per-organization counters: number of object types, number of action types, number of AIP-enabled functions, number of Logic flows in production; surfaced as a single adoption widget on the org dashboard.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

- [ ] `UAE.26` Training completion tracking (`P1`, `todo`)
  - Track per-user completion of named walkthroughs and use-case templates; surface a per-organization training rollup and per-user transcript.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

### Walkthrough completion tracking

- [ ] `UAE.27` Per-user completion ledger (`P1`, `todo`)
  - Each walkthrough start, step transition, abandonment, and completion is appended to `walkthrough_run` with user, walkthrough RID, version, step index, timestamp; one row per event.
  - Indexed for per-user transcript queries and per-walkthrough funnel reports.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

- [ ] `UAE.28` Funnel report (`P1`, `todo`)
  - Per-walkthrough funnel: starts → step-1 reached → step-N reached → completed; segmented by user role and organization; surfaced in the walkthrough authoring view.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

### Document publication workflow

- [ ] `UAE.29` Draft / review / publish (`P1`, `todo`)
  - Doc pages and doc sites carry a publication state machine with explicit `draft`, `in_review`, `published`, `retired`; transitions emit audit events and may be gated by an approver group.
  - Published pages snapshot their markdown so consumer-mode viewers see a stable version.
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

- [ ] `UAE.30` Search indexing (`P1`, `todo`)
  - Published doc pages are indexed into Compass quicksearch with title, headings, and body; consumer-mode sessions see only consumer-visible doc pages in the index.
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

- [ ] `UAE.31` Version history (`P1`, `todo`)
  - Every edit creates a `doc_page_version` row; UI exposes diff between any two versions and a one-click revert.
  - Docs: [Custom documentation](https://www.palantir.com/docs/foundry/enablement/custom-documentation).

## Milestone C: advanced parity

### Code-mod integration

- [ ] `UAE.32` Code Repositories code-mod hook (`P2`, `todo`)
  - When a migration step's target is a Code Repository (legacy SDK call, deprecated import, legacy auth pattern), the apply executor opens a pull request in the target repo with the suggested code-mod patch, links it from the migration step, and waits for merge before marking the step `applied`.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

- [ ] `UAE.33` Marketplace re-publish hook (`P2`, `todo`)
  - When a migration step modifies a resource that is part of a published Marketplace product, the executor bumps the product's manifest, emits a re-publish job, and links the resulting product version from the migration step.
  - Docs: [Migration plans](https://www.palantir.com/docs/foundry/upgrade-assistant/migration-plans).

### Flow Capture sharing and redaction

- [ ] `UAE.34` Share link with permission scoping (`P2`, `todo`)
  - A flow capture can be shared via a tokenized link that grants read-only replay access to a named set of users or groups; the link enforces marking checks and expires by default after 30 days.
  - Docs: [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture).

- [ ] `UAE.35` Per-field redaction (`P2`, `todo`)
  - Recorded captures pass through a redaction pass that masks input values matching configured selectors (e.g. password fields, PII columns) before persistence; redaction rules are organization-scoped and versioned.
  - Docs: [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture).

- [ ] `UAE.36` Screenshot blur policy (`P2`, `todo`)
  - Screenshot frames may be blurred according to per-app selectors (e.g. blur object-row content on Object Explorer, blur dataset preview cells); enforced at capture time, not display time.
  - Docs: [Flow Capture](https://www.palantir.com/docs/foundry/enablement/flow-capture).

### Per-team adoption rollups and leaderboard

- [ ] `UAE.37` Per-team rollup (`P2`, `todo`)
  - Adoption metrics aggregated by team (Compass project ownership group) with quarterly trend, top-growing app surfaces, and stalled-project flags.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

- [ ] `UAE.38` Leaderboard (`P2`, `todo`)
  - Optional opt-in leaderboard ranking teams or projects by a configurable composite (active users, app coverage, training completion); leaderboard publishes are audited.
  - Docs: [Foundry adoption](https://www.palantir.com/docs/foundry/enablement/foundry-adoption).

### Use-case lifecycle and pattern library

- [ ] `UAE.39` Lifecycle phase resource (`P2`, `todo`)
  - Use-case instances carry a lifecycle phase: `incubation` → `pilot` → `production` → `expansion` → `retire`; transitions emit audit events, may be gated by approvers, and are surfaced on the use-case detail page.
  - Docs: [Use case lifecycle](https://www.palantir.com/docs/foundry/enablement/use-case-lifecycle).

- [ ] `UAE.40` Lifecycle gate criteria (`P2`, `todo`)
  - Each phase declares required artifacts (e.g. `pilot` requires at least one Workshop module + one ontology object type + one published doc page); the transition API enforces criteria before advancing.
  - Docs: [Use case lifecycle](https://www.palantir.com/docs/foundry/enablement/use-case-lifecycle).

- [ ] `UAE.41` Pattern library (`P2`, `todo`)
  - Curated library of `use_case_pattern` entries grouping templates that solve a common shape (e.g. "operational decision app", "data triage queue", "AIP-assisted review loop"); each pattern links its templates, walkthroughs, doc pages.
  - Docs: [Use case patterns](https://www.palantir.com/docs/foundry/enablement/use-case-patterns).

- [ ] `UAE.42` Pattern recommendation (`P2`, `todo`)
  - The use-case dashboard recommends candidate patterns for an organization based on its current adoption profile and active templates.
  - Docs: [Use case patterns](https://www.palantir.com/docs/foundry/enablement/use-case-patterns).

### Branded consumer portal

- [ ] `UAE.43` Branded entry portal (`P2`, `todo`)
  - Per-organization consumer-portal config: logo asset, primary color tokens, welcome copy, landing layout (featured Workshop modules, featured doc pages); applied on every consumer-mode session.
  - Docs: [Consumer mode](https://www.palantir.com/docs/foundry/enablement/consumer-mode).

- [ ] `UAE.44` A/B variant assignment for walkthroughs (`P2`, `todo`)
  - A walkthrough may declare variants; users are deterministically bucketed by a hashed identifier and the funnel report breaks down per variant.
  - Docs: [Walkthroughs](https://www.palantir.com/docs/foundry/enablement/walkthroughs).

- [ ] `UAE.45` Consumer audit narrowing (`P2`, `todo`)
  - Consumer-mode sessions emit a constrained audit subset (page-view + replay-event) rather than the full operator-grade audit; surfaced separately in the audit explorer.
  - Docs: [Consumer mode](https://www.palantir.com/docs/foundry/enablement/consumer-mode).

## Implementation inventory to collect before coding

- [ ] `INV.1` Inventory every resource kind currently registered in the resource registry and decide which kinds the Upgrade Assistant must reason about in milestone A vs milestones B/C.
- [ ] `INV.2` Inventory the existing deprecation signals (SDK warning logs, legacy RID prefixes, legacy auth callers) and map them to candidate detector implementations.
- [ ] `INV.3` Inventory the audit event taxonomy and reserve event names for `migration_plan_*`, `flow_capture_*`, `walkthrough_*`, `use_case_lifecycle_*`, and `consumer_mode_*`.
- [ ] `INV.4` Inventory the apps/web route table and decide which routes need named anchors so walkthrough step targeting is selector-stable.
- [ ] `INV.5` Inventory the Compass quicksearch indexer's hook contract and design the doc-page indexing entry point.
- [ ] `INV.6` Inventory the Marketplace product manifest schema to confirm the re-publish hook can bump a version cleanly.
- [ ] `INV.7` Inventory the Code Repositories pull-request API surface required for the code-mod hook and identify per-language code-mod tooling we can adopt.
- [ ] `INV.8` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `upgrade-assistant-service` | Deprecation rule registry, resource scan, migration plan CRUD, dry-run, batched apply, per-step rollback, audit emission. |
| `flow-capture-service` | Recorder ingestion, capture storage, replay runtime, redaction pipeline, share-link issuance, screenshot blur policy. |
| `adoption-metrics-service` (or extend `audit-compliance-service`) | Aggregation jobs over the audit stream, per-org / per-team / per-user rollups, training transcripts, leaderboard publishes. |
| `documentation-site-service` | Doc site + doc page + TOC CRUD, publication workflow, version history, embedded-resource resolution, search indexer hook. |
| `walkthrough-service` | Walkthrough resource CRUD, entry-trigger evaluation, per-user completion ledger, funnel report, A/B variant assignment. |
| `use-case-catalog-service` | Use-case template + instance + pattern CRUD, instantiation API, lifecycle state machine, gate criteria enforcement, pattern recommendations. |
| `apps/web` | Upgrade Assistant admin, doc site renderer + author, walkthrough host, adoption dashboard, use-case catalog, consumer-mode portal shell. |

## Acceptance criteria

- Every milestone-A item has a tracking issue, an owner, and either a service implementation or an explicit `blocked` reason.
- Upgrade Assistant detection emits findings for at least three real deprecation rules and dry-run produces a deterministic diff per finding.
- Custom documentation supports a full author → review → publish loop with version history and quicksearch indexing.
- Walkthrough resource renders against a real apps/web route with stable named anchors and records a `walkthrough_run` for each session.
- A use-case template instantiates a new project with rewritten RIDs and is browsable in Compass.
- Consumer mode toggle hides all authoring surfaces and returns `404` for any resource not marked `consumer_visible`.
- Every migration plan apply and rollback is reflected in the security/governance audit trail.
- A flow capture recorded in Workshop and replayed in CI produces the same step results and asserts selector drift on UI changes.

## Test plan expectations

- Unit tests for the deprecation rule registry, the migration plan state machine, the per-step rollback resolver, the walkthrough entry-trigger matcher, and the use-case lifecycle gate evaluator.
- Integration tests (`integration` build tag) that scan a seeded project, generate a migration plan, dry-run, apply, and rollback against real resource services.
- Integration tests that record a flow capture across two app contexts, persist, replay, and assert per-step results.
- Integration tests that publish a doc page, index it into quicksearch, and assert visibility differences between an operator session and a consumer-mode session.
- Integration tests that instantiate a use-case template and verify embedded RIDs are rewritten to the new project's scope.
- Frontend tests (`vitest` + Playwright) for the walkthrough host (step targeting, advance triggers, completion event), the doc site renderer (TOC, embed resolution), the adoption dashboard widgets, and the consumer-mode portal shell.
- Audit assertions: every `migration_plan_*`, `walkthrough_*`, `consumer_mode_toggle`, `doc_page_publish`, and `use_case_lifecycle_transition` emits a single, well-formed audit event with the expected actor and resource RID.
