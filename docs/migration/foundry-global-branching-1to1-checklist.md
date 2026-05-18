# Foundry Global Branching 1:1 parity checklist

Date: 2026-05-11
Scope: public-docs-based parity plan for OpenFoundry's Global Branching
surface: branch creation and security, branch taskbar, centralized branching
application, branched resources, preview readiness, proposal creation, merge
checks, reviewer workflows, approvals, rebasing and conflict resolution,
side-effect controls, branch retention, and cross-application integrations for
Pipeline Builder, Code Repositories, Ontology Manager, Workshop, Functions, AIP
Logic, Object Views, datasets, views, and restricted views.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
end-to-end workflows, compatible resource models where useful, and
OpenFoundry-native implementation details that can be tested locally.

> **Current OpenFoundry implementation note (2026-05-18).**
> `services/global-branch-service` is no longer just a scaffold. Milestone A
> hosts tenant-scoped branch lifecycle CRUD, service participation rows,
> conflict-aware merge coordination, audit event emission, and integration
> tests for create → add-participation → merge flows. The remaining P0 gap is
> integration: the edge gateway and frontend still use the legacy
> `code-repository-review-service` route shape until the cutover task moves
> branch product routes to this binary.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native implementation,
not a pixel-perfect clone.

This checklist covers the cross-application Global Branching layer. It should
coordinate with the specialized checklists for the underlying resources:
Pipeline Builder, Workshop, Ontology, Functions, AIP Logic, Object Views, Data
Foundation, Data Connection, and DevOps. It should not redefine each
application's full resource model; instead, it defines the shared branch,
proposal, approval, preview, merge, rebase, retention, and side-effect contracts
that make those resources work together on one branch.

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
| `P0` | Required for safe end-to-end development of the Trail Running demo across pipelines, ontology, actions, functions, and Workshop without writing to production state. |
| `P1` | Required for credible Foundry-style multi-application branching beyond a single demo. |
| `P2` | Advanced, governance-heavy, cost/retention, or scale-oriented parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### Global Branching concepts and scope

- [Global Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview/)
- [Global Branching core concepts](https://www.palantir.com/docs/foundry/foundry-branching/core-concepts/)
- [Global Branching supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/)
- [Global Branching best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/)
- [Global Branching integrations](https://www.palantir.com/docs/foundry/foundry-branching/integrations/)

### Navigation and lifecycle

- [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/)
- [Branch taskbar](https://www.palantir.com/docs/foundry/foundry-branching/branch-taskbar/)
- [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/)
- [Rebasing and conflict resolution](https://www.palantir.com/docs/foundry/global-branching/rebasing-and-conflict-resolution)
- [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention)

### Security, approvals, and side effects

- [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security)
- [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources)
- [Managing side effects via actions on branches](https://www.palantir.com/docs/foundry/global-branching/side-effects-on-branches/)

### Application-specific branching behavior

- [Pipeline Builder branches overview](https://www.palantir.com/docs/foundry/pipeline-builder/branches-overview/)
- [Code Repositories branch settings](https://www.palantir.com/docs/foundry/code-repositories/branch-settings)
- [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology)
- [Branching functions](https://www.palantir.com/docs/foundry/global-branching/branching-functions/)
- [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic)
- [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views)

## Target OpenFoundry resource model

The implementation should define stable OpenFoundry-owned resources that can map
to public Foundry concepts without requiring Palantir RID formats. Compatibility
aliases may be accepted at service boundaries, but persisted state should use
OpenFoundry canonical IDs.

| Public Foundry concept | OpenFoundry resource target | Required notes |
| --- | --- | --- |
| Global Branch | `global_branch` | Cross-application branch associated with one ontology, one space, one or more organizations, and a lifecycle state. |
| Main branch | `main` branch alias | Canonical production branch. The UI may display `Main`, but APIs should normalize to an OpenFoundry canonical name. |
| Branch role | `global_branch_role_assignment` | Branch management permission such as owner. Roles do not grant edit access to underlying resources. |
| Branch organizations | `global_branch_organization_gate` | Organization gate controlling who can access the branch itself. Resource-level visibility still applies. |
| Branched resource | `branched_resource` | Resource-specific branch state for pipeline, code repository, ontology entity, Workshop module, function, Logic file, Object View, dataset, or view. |
| Resource adapter | `branch_resource_adapter` | Per-resource plugin that knows how to add, remove, preview, rebase, diff, check, approve, and merge one resource type. |
| Branch taskbar | `branch_taskbar_context` | In-app branch selector, modified resources selector, proposal controls, merge controls, and resource status panel. |
| Branching application | `global_branching_app` | Central hub for My items, Branches, Proposals, approvals, preview status, merge history, comments, and security settings. |
| Preview status | `branch_preview_status` | Aggregate and per-resource status such as pending, in progress, ready for preview, failed, or blocked. |
| Proposal | `global_branch_proposal` | Reviewable unit created from a branch that can be approved, rejected, closed, or merged. |
| Proposal resource | `global_branch_proposal_resource` | One modified resource participating in a proposal with status, reviewers, checks, approvals, and merge attempts. |
| Merge check | `merge_check` | Adapter-produced condition that gates merge and points users to a fix. |
| Reviewer assignment | `branch_reviewer_assignment` | User or group requested to review one proposal or proposal resource. |
| Review decision | `branch_review_decision` | Approve/reject/edit-review decision with reviewer, timestamp, comment, and policy context. |
| Project approval policy | `project_approval_policy` | Project-level rule defining eligible reviewers, approvals required, and contributor self-approval behavior. |
| Resource protection | `resource_branch_protection` | Setting requiring branch/proposal workflow before modifying protected resources on main. |
| Rebase operation | `branch_rebase_operation` | Resource-specific reconciliation of main changes into branch state, with conflicts and resolution records. |
| Side-effect policy | `branch_side_effect_policy` | Controls whether webhooks, external function calls, and notifications run from branched action execution. |
| Branch retention policy | `global_branch_retention_policy` | Space-scoped inactivity and closure thresholds plus cleanup/de-indexing behavior. |
| Merge attempt | `branch_merge_attempt` | Attempt to merge a proposal with build/indexing scope, per-resource outcomes, and errors. |
| Branch audit event | `branch_audit_event` | Immutable event stream for create, update, role changes, resource changes, proposals, reviews, rebases, merges, closes, and retention actions. |

## Milestone A: minimum viable Global Branching parity

### Branch creation, access, and lifecycle

- [ ] `GB.1` Global branch CRUD and lifecycle state machine (`P0`, `todo`)
  - Create, get, list, update metadata, close, and inspect global branches.
  - Track `Active`, `Inactive`, `Closed`, and `Merged` states, plus timestamps for creation, last activity, closure, and merge.
  - Require each branch to be associated with a single ontology and a space-like ownership scope.
  - Prevent reopening closed branches until a product decision explicitly enables it.
  - Docs: [Global Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview/), [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention).

- [ ] `GB.2` Branch creation entry points (`P0`, `todo`)
  - Support branch creation from Pipeline Builder, transforms code repositories, TypeScript function repositories, Ontology Manager, Workshop, and the Global Branching application.
  - Ensure all entry points collect branch name, description, ontology, space when required, organization access, and initial source branch.
  - Branches should be creatable only from `main` unless a future documented workflow permits branch-from-branch creation.
  - Docs: [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/), [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

- [ ] `GB.3` Branch selector and context propagation (`P0`, `todo`)
  - Add a reusable branch selector that appears in branch-aware applications.
  - Propagate branch context through API calls, preview requests, build requests, ontology reads, action execution, function selection, and Workshop runtime loads.
  - Use main when no branch is selected and clearly label branch context when a non-main branch is active.
  - Docs: [Branch taskbar](https://www.palantir.com/docs/foundry/foundry-branching/branch-taskbar/), [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/).

- [ ] `GB.4` Global Branching application shell (`P0`, `todo`)
  - Build a centralized route with My items, Branches, Proposals, and Approvals sections.
  - My items should show the current user's open proposals and open branches, plus shortcuts to merged proposals, closed proposals, and closed branches.
  - Branches should support list, search, filter by status, filter by creator, create new branch, close eligible branch, and open branch detail.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/).

- [ ] `GB.5` Branch detail overview (`P0`, `todo`)
  - Show branch name, status, description, creator, created date, last updated date, ontology, space, organizations, changed resource count, preview status, proposal link, and comments.
  - List modified resources with type, display name, owning application, current status, preview status, merge-check status, and direct link to the branched resource.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/), [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/).

### Branch taskbar and branched resources

- [ ] `GB.6` Branch taskbar component (`P0`, `todo`)
  - Add a reusable taskbar to supported applications when viewing a branch other than main.
  - Include branch selector, modified resource selector, create proposal action, merge changes action after proposal creation, review status, and merge-check hints.
  - Ensure taskbar affordances are consistent in Pipeline Builder, Workshop, Ontology Manager, and function/action editing surfaces.
  - Docs: [Branch taskbar](https://www.palantir.com/docs/foundry/foundry-branching/branch-taskbar/), [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/).

- [ ] `GB.7` Branched resource registry (`P0`, `todo`)
  - Register resource adapters for pipeline, code repository, ontology object type, ontology link type, ontology action type, ontology interface type, shared property type, Workshop module, function package, AIP Logic file, Object View, dataset, and view.
  - Store resource identity, main version pointer, branch version pointer, owning application, project, protection state, preview status, merge state, and last activity.
  - Docs: [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/), [Integrations](https://www.palantir.com/docs/foundry/foundry-branching/integrations/).

- [ ] `GB.8` Add and remove resources from a branch (`P0`, `todo`)
  - Add a resource to a branch on first branch-scoped edit or explicit add action.
  - Remove a resource from a branch from the taskbar or branching app, reverting that resource to main state for that branch.
  - Warn when removing a resource could break other branched resources that depend on it.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

- [ ] `GB.9` Branch-aware resource links and navigation (`P0`, `todo`)
  - Generate deep links that preserve branch context for each modified resource.
  - Ensure links from Branches, Proposals, Preview status, taskbar resource selector, and review screens open the correct branched version.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/), [Branch taskbar](https://www.palantir.com/docs/foundry/foundry-branching/branch-taskbar/).

### Preview and isolated runtime behavior

- [ ] `GB.10` Branch preview status engine (`P0`, `todo`)
  - Track aggregate and per-resource preview statuses: pending, in progress, ready for preview, failed, and blocked.
  - Update preview status when branch builds, indexing jobs, ontology indexing, function publishes, or Workshop previews complete.
  - Surface preview status in branch detail, proposal detail, and taskbar resource selectors.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/), [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/).

- [ ] `GB.11` Workshop branch preview fallback semantics (`P0`, `todo`)
  - Workshop runtime must load branched data for resources modified on the branch.
  - Workshop runtime must load main data for unmodified resources in the module.
  - Clearly expose when a widget is reading branch data versus fallback main data.
  - Docs: [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/), [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/).

- [ ] `GB.12` Branch action execution sandbox (`P0`, `todo`)
  - Allow actions to run in Workshop on a branch without writing edits back to main.
  - Require all object types modified by the action to be indexed on the branch before execution.
  - Return branch-scoped action results, validation errors, and object edits to the calling Workshop module.
  - Docs: [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/), [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/).

- [ ] `GB.13` Safe side-effect defaults for branch actions (`P0`, `todo`)
  - Disable webhook execution by default for actions applied on branches.
  - Fail or block function-backed actions with external calls by default when executed on branches.
  - Suppress notifications by default for actions executed on branches.
  - Show explicit user-facing explanations when side effects are skipped or blocked.
  - Docs: [Managing side effects via actions on branches](https://www.palantir.com/docs/foundry/global-branching/side-effects-on-branches/).

### Proposal and merge basics

- [ ] `GB.14` Proposal creation (`P0`, `todo`)
  - Create a proposal from the taskbar or Global Branching application once a branch has modified resources.
  - Capture proposal name, description, creator, branch, changed resources, initial preview statuses, and comments.
  - Block proposal creation for closed or merged branches.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Branch taskbar](https://www.palantir.com/docs/foundry/foundry-branching/branch-taskbar/).

- [ ] `GB.15` Proposal detail and changed resources (`P0`, `todo`)
  - Show proposal overview, associated branch, resource list, resource statuses, reviewers, review links, comments, merge checks, branch preview status, resources changed tab, and merge history tab.
  - Support search/filter by resource type, status, reviewer state, and merge-check state.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/).

- [ ] `GB.16` Merge checks framework (`P0`, `todo`)
  - Run adapter-provided merge checks for each proposal resource.
  - Record status, severity, error message, remediation link, stale/retry state, and last checked timestamp.
  - Block merge until required checks pass or are explicitly marked non-blocking by product policy.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/).

- [ ] `GB.17` Merge execution with build scope choice (`P0`, `todo`)
  - Merge approved proposals into main from the taskbar or Global Branching application.
  - Offer build scope choices for affected datasets/resources: build all affected resources or build modified resources only.
  - Persist merge attempts with per-resource results and errors.
  - Mark proposal and branch merged only when all required resource merges succeed.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/).

## Milestone B: credible Foundry-style multi-application branching parity

### Security and access controls

- [ ] `GB.18` Branch security roles (`P1`, `todo`)
  - Implement owner role assignment for users and groups.
  - Automatically assign the branch creator as owner.
  - Allow owners and space administrators to edit metadata, assign roles, create proposals, merge proposals, manage organizations, and remove inactive labels.
  - Ensure branch roles control branch management only and do not grant resource edit access.
  - Docs: [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

- [ ] `GB.19` Branch organization gates (`P1`, `todo`)
  - Require users to belong to at least one organization attached to a branch to access the branch.
  - Restrict branch organizations to a subset of the branch space's organizations.
  - Warn users not to include sensitive information in branch names/descriptions because metadata may appear around branched resources.
  - Docs: [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

- [ ] `GB.20` Resource-level permission enforcement (`P1`, `todo`)
  - Require resource-level view permission to see a branched resource or proposal resource.
  - Require underlying resource edit permission to modify a resource on a branch.
  - Require merge/deploy permission from the resource adapter before merging that resource.
  - Docs: [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security), [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources).

### Resource protection and approvals

- [ ] `GB.21` Resource branch protection (`P1`, `todo`)
  - Protect resources so main cannot be changed directly and changes must go through a branch and proposal.
  - Support protection on Workshop modules, ontology object types, ontology action types, ontology link types, ontology interface types, shared property types, AIP Logic functions, and any resource adapters that opt in.
  - Show branch-lock state in file/resource listings and resource details.
  - Docs: [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources), [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

- [ ] `GB.22` Project approval policies (`P1`, `todo`)
  - Implement project-level approval policies with eligible reviewers, approvals required, contributor self-approval setting, default policy, and custom policy.
  - Refresh open proposals when a policy changes or a resource moves projects.
  - Display applicable policies from proposal and taskbar reviewer controls.
  - Docs: [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources).

- [ ] `GB.23` Reviewer assignment and review decisions (`P1`, `todo`)
  - Add/remove reviewers on proposal resources from the taskbar or Global Branching application.
  - Notify reviewers and expose review entry points.
  - Record approvals, rejections, edited reviews, reviewer comments, and policy satisfaction.
  - Treat rejection as a merge blocker until resolved according to local policy.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources), [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

- [ ] `GB.24` Application-specific approval adapters (`P1`, `todo`)
  - Code Repositories and Pipeline Builder should respect local branch protection and approval policies.
  - Ontology resources should be grouped into an ontology proposal while still tracking per-resource policy status.
  - Workshop modules should support project-policy approvals where implemented and automatic approval fallback where public docs describe that behavior.
  - Object Views should be automatically approved until a public object-view approval workflow exists.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources), [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

### Rebasing and conflicts

- [ ] `GB.25` Rebase requirement tracking (`P1`, `todo`)
  - Detect when main changed after a branch was created or last rebased for each resource.
  - Surface rebase-required status in the taskbar, proposal merge checks, resource editor, and Global Branching application.
  - Block merge for resources that require a rebase when the adapter declares rebase mandatory.
  - Docs: [Rebasing and conflict resolution](https://www.palantir.com/docs/foundry/global-branching/rebasing-and-conflict-resolution), [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views), [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

- [ ] `GB.26` Generic rebase operation model (`P1`, `todo`)
  - Store main base version, current main version, current branch version, proposed rebase result, conflict list, and resolution decisions.
  - Support adapter-provided automatic acceptance for non-conflicting changes.
  - Preserve audit trail for manual conflict resolutions.
  - Docs: [Rebasing and conflict resolution](https://www.palantir.com/docs/foundry/global-branching/rebasing-and-conflict-resolution), [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

- [ ] `GB.27` Workshop rebase and changelog UX (`P1`, `todo`)
  - Show out-of-date messages in Workshop changelog when main has changes absent from the branch.
  - Provide conflict review UI where the user chooses branch or main changes to keep.
  - Update merge checks after successful rebase.
  - Docs: [Rebasing and conflict resolution](https://www.palantir.com/docs/foundry/global-branching/rebasing-and-conflict-resolution).

- [ ] `GB.28` Object View and AIP Logic rebase UX (`P1`, `todo`)
  - Object Views should show main state, branch state, proposed rebase result, automatic non-conflicting changes, conflicts, and manual tab-level conflict choices.
  - AIP Logic should expose split-screen comparison and require manual incorporation of main changes when conflicts exist.
  - Docs: [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views), [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

### Cross-application adapters

- [ ] `GB.29` Pipeline Builder adapter (`P1`, `todo`)
  - Integrate global branches with Pipeline Builder branch lifecycle, proposals, local merge checks, build previews, build-on-merge, and fallback branches for inputs not built on the current branch.
  - Keep Pipeline Builder's local branch semantics and map them into global branch resources.
  - Docs: [Pipeline Builder branches overview](https://www.palantir.com/docs/foundry/pipeline-builder/branches-overview/), [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/).

- [ ] `GB.30` Code Repositories adapter (`P1`, `todo`)
  - Link global branches to repository branches and local code-review policies.
  - Expose changed repository resources, publish/build check status, protected branch requirements, and merge outcomes.
  - Docs: [Code Repositories branch settings](https://www.palantir.com/docs/foundry/code-repositories/branch-settings), [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/).

- [ ] `GB.31` Ontology Manager adapter (`P1`, `todo`)
  - Support branch-scoped object type, link type, action type, interface type, and shared property type changes.
  - Group ontology resources under a local ontology proposal while preserving per-resource approvals and statuses.
  - Include indexing changes as modifications and allow users to remove unwanted indexing changes before merge.
  - Docs: [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology), [Resource protection and project approval policies](https://www.palantir.com/docs/foundry/foundry-branching/protecting-resources).

- [ ] `GB.32` Workshop adapter (`P1`, `todo`)
  - Add Global Branching as Workshop's branch mechanism.
  - Support branched module edits, changelog, preview, rebase, proposal integration, and runtime fallback semantics.
  - Ensure non-Workshop embedded elements that are not branch-aware remain read-only/main-backed or clearly marked unsupported.
  - Docs: [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/), [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/).

- [ ] `GB.33` Functions adapter (`P1`, `todo`)
  - Support TypeScript function development, branch publishing, branched pre-release versions, and version targets.
  - Make branched function versions usable in Workshop and function-backed actions on the same branch.
  - Ensure branched function versions are unavailable from other branches and main.
  - On merge, publish only the selected version target to main and update branch consumers to the stable version.
  - Docs: [Branching functions](https://www.palantir.com/docs/foundry/global-branching/branching-functions/).

- [x] `GB.34` AIP Logic adapter (`P1`, `done`)
  - Add, remove, modify, publish, review, rebase, and merge Logic functions on branches.
  - Support branched Logic functions in branch-aware applications such as Workshop and branch ontology object interactions.
  - Enforce merge requirements: up to date with main, published on branch, publishable state, and no pending approvals.
  - Added the Logic branch adapter domain layer with branch resource identity, isolated branch versions, branched pre-release publication metadata for branch-aware Workshop/action/object contexts, proposal review state, manual rebase conflicts, merge checks, and merge-to-main version promotion.
  - Added Logic authoring UI controls for add/edit/publish/review/rebase/merge/remove flows, branch-only availability, merge requirement detail, conflict visibility, and adapter operation history.
  - Tests cover branch isolation, pre-release scope, approval checks, rebase conflict resolution, removal restrictions, publishability checks, and successful merge publication.
  - Docs: [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

- [ ] `GB.35` Object Views adapter (`P1`, `todo`)
  - Track OV-managed modules and full object view tab resources on branches.
  - Support branch editing against latest ontology state on the same branch.
  - Rebase object view module resources and tab configuration resources separately where required.
  - Enforce deployability checks: publish permission, rebased with main, and no unsupported legacy-field modifications.
  - Docs: [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

### Side effects and action policy overrides

- [ ] `GB.36` Branch side-effect policy UI (`P1`, `todo`)
  - Add action type settings for enabling webhooks on branches, enabling external function calls on branches, and enabling notifications on branches.
  - For notifications, allow branch owner or default configured recipients when branch notifications are enabled.
  - Warn strongly when branch execution may hit production external systems.
  - Docs: [Managing side effects via actions on branches](https://www.palantir.com/docs/foundry/global-branching/side-effects-on-branches/).

- [ ] `GB.37` Side-effect audit and simulation (`P1`, `todo`)
  - Record whether webhooks, external function calls, and notifications were skipped, blocked, executed, or redirected for every branch action run.
  - Provide a simulation/dry-run report before enabling side effects on a branch action type.
  - Docs: [Managing side effects via actions on branches](https://www.palantir.com/docs/foundry/global-branching/side-effects-on-branches/).

## Milestone C: advanced parity, retention, and scale

### Branch retention and cost control

- [ ] `GB.38` Space-scoped branch retention policies (`P2`, `todo`)
  - Configure inactivity-to-inactive and inactive-to-closed thresholds per space.
  - Run retention tasks on a scheduled cadence and record every state transition.
  - Default to documented-style short-lived branches while allowing local OpenFoundry defaults to be configured explicitly.
  - Docs: [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention), [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

- [ ] `GB.39` Inactive branch notifications and recovery (`P2`, `todo`)
  - Notify branch owners by in-platform notification and email when a branch becomes inactive.
  - Allow owners to remove the inactive label or make a branch change to return to active.
  - Prevent inactive label removal by non-owners unless the user is a space administrator.
  - Docs: [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention), [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

- [ ] `GB.40` Closed and merged branch cleanup (`P2`, `todo`)
  - Delete or de-index leftover branch data after a configurable grace period for closed or merged branches.
  - Preserve audit metadata and proposal history after resource cleanup.
  - Prevent branch cleanup from deleting main state or merged stable resources.
  - Docs: [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention).

- [ ] `GB.41` Branch cost insights (`P2`, `blocked`)
  - Track approximate compute, storage, indexing, and build costs by branch and resource.
  - Surface branch cost insights in Resource Management or an OpenFoundry-native resource management view.
  - Mark as blocked until OpenFoundry has a shared cost-metering model.
  - Docs: [Global Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview/), [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention).

### Advanced resource coverage

- [ ] `GB.42` Dataset and view branch integration (`P2`, `todo`)
  - Integrate global branch context with dataset branches, dataset builds, table reads, schemas, lineage, and views.
  - Allow views to be built on branches even when the view itself is not tracked as a changed global-branch resource.
  - Clearly document view limitations and require force-build behavior where applicable.
  - Docs: [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/), [Pipeline Builder branches overview](https://www.palantir.com/docs/foundry/pipeline-builder/branches-overview/).

- [ ] `GB.43` Restricted views support (`P2`, `blocked`)
  - Support branch-scoped restricted views when OpenFoundry security/governance and restricted-view semantics exist.
  - Label the feature experimental or blocked until the required security model is implemented.
  - Docs: [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/), [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/).

- [ ] `GB.44` Cross-application dependency graph for branches (`P2`, `todo`)
  - Build a branch dependency graph linking modified pipelines, datasets, views, ontology entities, actions, functions, Logic files, Object Views, and Workshop modules.
  - Use the graph to warn on unsafe resource removal, compute preview readiness, suggest build scope, and explain merge effects.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/), [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/).

- [ ] `GB.45` Branch-aware generated SDK and API headers (`P2`, `todo`)
  - Add documented OpenFoundry API patterns for reading, previewing, building, and executing resources on a branch.
  - Generate SDK helpers for passing branch context safely without leaking branch-only versions to main.
  - Docs: [Global Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview/), [Branching functions](https://www.palantir.com/docs/foundry/global-branching/branching-functions/).

### Observability, audit, and administration

- [ ] `GB.46` Branch audit event stream (`P2`, `todo`)
  - Emit immutable events for branch create/update/close, role changes, organization changes, resource add/remove, preview status changes, proposal creation, reviewer changes, reviews, rebases, merge checks, merge attempts, retention actions, and side-effect decisions.
  - Provide filters by branch, proposal, resource, user, event type, and time window.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/), [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

- [ ] `GB.47` Branch health and operational dashboards (`P2`, `todo`)
  - Show open branches by age, inactive branches, branches near closure, proposals awaiting review, failed merge checks, failed preview resources, side-effect policy overrides, and merge failures.
  - Support drill-down from operational dashboards into branch/proposal detail pages.
  - Docs: [Global Branching application](https://www.palantir.com/docs/foundry/global-branching/branching-app/), [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention).

- [ ] `GB.48` Administration settings (`P2`, `todo`)
  - Add global enable/disable for Global Branching, per-application enablement, branch retention policy configuration, default organization behavior, and branch naming rules.
  - Ensure admin settings do not silently break existing open branches; require migration or warning flows.
  - Docs: [Global Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview/), [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention), [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

## Milestone D: cross-application atomic propagation

> **Added 2026-05-17.** The existing milestones model branches as a
> registry of branched resources from many applications, but do not
> guarantee that creating, switching, merging, or closing a branch
> propagates **atomically** across all participating applications. This
> milestone closes that gap.

### Atomic branch lifecycle propagation

- [ ] `GB.28` Atomic create across applications (`P1`, `todo`)
  - Branch creation is a two-phase commit across the participating application adapters (Pipeline Builder, Ontology Manager, Workshop, Functions, AIP Logic, Object Views, Dataset Versioning, Schedule, Data Health, Object Storage V2 overlays).
  - Failure in any adapter aborts the branch creation; partial state is rolled back and the creator sees a single typed error.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/).

- [ ] `GB.29` Atomic merge across applications (`P1`, `todo`)
  - Merge orchestrator records a multi-resource merge plan and executes it as a saga (use existing `libs/saga`) with per-adapter compensation steps.
  - Build-scope choice from `GB.17` applies after the saga has materialized resource changes on main.
  - Docs: [Branching lifecycle](https://www.palantir.com/docs/foundry/foundry-branching/branching-lifecycle-usage/).

- [ ] `GB.30` Atomic close and retention sweep (`P1`, `todo`)
  - Branch close releases adapter overlays (Object Storage V2 branch overlay, dataset branch transactions, Workshop branch versions, etc.) in a single audited operation.
  - Retention policy can close branches inactive for N days; the same atomic close path runs.
  - Docs: [Branch retention](https://www.palantir.com/docs/foundry/global-branching/branch-retention).

### Cross-application preview consistency

- [ ] `GB.31` Cross-resource preview snapshot (`P1`, `todo`)
  - A "preview" command on a proposal builds a consistent snapshot id used by every adapter to render the proposal at that moment; later edits do not affect the preview.
  - Workshop, Object Views, Vertex, Map, Quiver, dashboards all honor the snapshot id when rendering proposal previews.
  - Docs: [Supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/).

- [ ] `GB.32` Consistent preview for action chains (`P1`, `todo`)
  - When a preview includes a chain of actions (e.g. action A's writes feed action B), the preview applies them in declared order against the branch overlay so the user sees the same end state every time.
  - Docs: [Best practices and technical details](https://www.palantir.com/docs/foundry/foundry-branching/best-practices-and-technical-details/).

### Cross-application audit

- [ ] `GB.33` Single audit envelope per branch operation (`P2`, `todo`)
  - Create, merge, close, and rebase emit a single audit envelope with per-adapter sub-events keyed by a shared operation id, so an auditor can reconstruct the whole change without joining across services.
  - Docs: [Branch security](https://www.palantir.com/docs/foundry/foundry-branching/branch-security).

### Cross-application cost insights

- [ ] `GB.34` Branch cost rollup (`P2`, `todo`)
  - Branch detail shows aggregate cost across all branched resource kinds (build runs, function executions, agent runs, notebook sessions, indexer reprocessing) sourced from the Resource Management accounting (see [Resource Management checklist](./foundry-resource-management-1to1-checklist.md)).
  - Docs: [Branch cost insights](https://www.palantir.com/docs/foundry/foundry-branching/cost-insights).

### Restricted-view interaction

- [ ] `GB.35` Branch + restricted view safety (`P2`, `todo`)
  - Branches may not create restricted views that depend on branched datasets (transform-input rule from Security/Governance `SG.30` applies).
  - Adapter rejects with a clear error and links to the policy doc.
  - Docs: [Restricted views constraints](https://www.palantir.com/docs/foundry/security/restricted-views#non-input-rule).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify existing OpenFoundry global branch API routes, frontend pages, branch models, and generated SDK methods.
- [ ] `INV.2` Identify Pipeline Builder branch, draft, proposal, publish, history, and merge-check primitives that can become a resource adapter.
- [ ] `INV.3` Identify dataset branch, transaction, view, schema, and build primitives needed for branch preview and merge build scope.
- [ ] `INV.4` Identify Ontology Manager resources and whether object types, link types, action types, interface types, shared property types, and indexing changes can be versioned independently.
- [ ] `INV.5` Identify Workshop app/module/page/widget versioning, changelog, preview, publish, and runtime branch-context primitives.
- [ ] `INV.6` Identify Functions, TypeScript runtime, Python runtime, function package publishing, version target, and function-backed action primitives.
- [ ] `INV.7` Identify AIP Logic or generic function-builder surfaces that need branch-aware save/publish/rebase/review behavior.
- [ ] `INV.8` Identify Object View or object detail surfaces that can be modeled as OV-managed modules and full object view tab resources.
- [ ] `INV.9` Identify existing approval, workflow, notification, email, audit, and policy engines that can support reviewers and project approval policies.
- [ ] `INV.10` Identify existing tenant/space/organization/security models that can support branch roles and organization gates.
- [ ] `INV.11` Identify existing webhook, external function call, notification, and action execution paths that need branch side-effect controls.
- [ ] `INV.12` Identify existing retention, cleanup, resource-management, and cost-metering services that can support inactive/closed/merged branch cleanup.
- [ ] `INV.13` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

## Suggested service boundaries

> **Reader note (2026-05-14)** — The services in the table below are
> *target* decomposition proposals, not a current inventory of
> binaries. Some have been built under consolidated names after S8
> (`marketplace-service` → `federation-product-exchange-service`;
> `approvals-service` → `workflow-automation-service/internal/approvals`;
> `ontology-security-service` → `authorization-policy-service`;
> `ai-service` → `agent-runtime-service` + `llm-catalog-service`).
> Others are not yet implemented. For the canonical list of binaries
> on disk today, see
> [`docs/architecture/services-and-ports.md`](../architecture/services-and-ports.md).

| Surface | Responsibilities |
| --- | --- |
| `global-branch-service` | Branch CRUD, lifecycle, security roles, organizations, branched resource registry, taskbar context, preview status aggregation, retention state transitions. |
| `branch-proposal-service` | Proposal CRUD, proposal resources, comments, reviewer assignments, review decisions, merge checks, merge attempts, merge history. |
| `branch-adapter-sdk` | Shared adapter contract for add/remove/diff/preview/rebase/check/approve/merge links across resource-owning services. |
| `pipeline-build-service` | Pipeline Builder branch adapter, build previews, build-scope planning, dataset output branch commits, merge-time builds. |
| `dataset-versioning-service` | Dataset branch/view/schema/table-read adapter, branch fallback reads, view builds, branch cleanup. |
| `ontology-definition-service` | Ontology resource adapter, ontology proposal grouping, branch-scoped definitions, indexing state, protection policies. |
| `ontology-actions-service` | Branch action sandbox, function-backed action branch execution, webhook/external-call/notification side-effect policy enforcement. |
| `functions service` | Function branch publishing, branched pre-release versions, version targets, merge-to-main stable publish behavior. |
| `logic service` | AIP Logic-style branch save/publish/rebase/review/merge adapter if OpenFoundry implements Logic separately from Functions. |
| `workshop service` | Workshop branch adapter, changelog, rebase, preview fallback semantics, taskbar integration, runtime branch context. |
| `object-view service` | Object View branch adapter, OV-managed module tracking, tab resource tracking, rebase/deployability checks. |
| `workflow-automation-service` | Approval policies, reviewer workflows, review notifications, rejection/approval state machine, branch event subscriptions. |
| `retention/resource-management service` | Inactive/closed/merged cleanup, de-indexing jobs, cost approximations, branch health dashboards. |
| `apps/web` | Global Branching app, branch taskbar, branch selectors, proposal review UI, security tab, retention settings, adapter-specific branch panels. |

## Acceptance criteria for first complete Global Branching milestone

- [ ] A user can create a global branch from Pipeline Builder, Ontology Manager, Workshop, and the Global Branching app.
- [ ] A user can switch branch context in supported applications and see a taskbar on non-main branches.
- [ ] Editing a pipeline, ontology object type, action type, function, or Workshop module on a branch registers a modified resource.
- [ ] The Global Branching app lists open branches, branch details, modified resources, preview status, proposals, reviewers, and merge history.
- [ ] Workshop preview reads branched resources where modified and falls back to main for unmodified resources.
- [ ] Actions can execute on a branch without writing edits to main, and default side-effect policy prevents webhooks, external function calls, and notifications from firing unexpectedly.
- [ ] A branch proposal can be created, assigned reviewers, approved/rejected, checked, and merged into main.
- [ ] Merge checks block merge when resources are not ready, stale, missing approvals, or failing adapter checks.
- [ ] Merge execution records per-resource results and supports build-all-affected versus build-modified-only scope choices.
- [ ] Rebase-required state is surfaced for at least Workshop and one other resource adapter, with conflict resolution records.
- [ ] Branch owners can manage branch roles and organizations, and resource permissions are enforced independently from branch visibility.
- [ ] Inactive branches can be marked by retention policy and closed branches cannot be reopened unless explicitly enabled by product policy.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for branch lifecycle state transitions, branch organization gates, owner role permissions, resource registration, resource removal dependency warnings, preview status aggregation, proposal state transitions, merge-check aggregation, reviewer policy satisfaction, side-effect policy defaults, and retention transitions.
- Adapter contract tests for Pipeline Builder, Ontology, Workshop, Functions, AIP Logic, Object Views, datasets, and views covering add/remove/diff/preview/rebase/check/merge behavior.
- API tests for branch CRUD, branch security, resource listing, taskbar context, proposal CRUD, reviewer assignment, review decisions, merge checks, merge attempts, preview status, side-effect policy, and retention settings.
- Integration tests for branch-scoped Pipeline Builder output into dataset branch preview, ontology indexing on branch, function-backed action on branch, Workshop fallback-to-main runtime, proposal approval, and merge to main.
- E2E tests for the Global Branching app, branch taskbar, create proposal flow, reviewer flow, failed merge-check remediation, successful merge, inactive branch notification, and branch side-effect warning flow.
- Regression tests proving branch-only versions and branch action edits cannot leak into main before merge.
