# Foundry Code Repositories and Code Workspaces 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's developer
surfaces equivalent to Foundry Code Repositories and Code Workspaces.
This covers in-browser Git-backed code repositories for Python, Java,
SQL, R and TypeScript transforms (including CI/CD checks, branch and
tag lifecycle, build executors, secrets injection, profiling, per-
language templates, transform compile and publish, dataset I/O
contracts, and lineage emission), managed Jupyter, RStudio and VS
Code-as-a-service workspaces (with persisted home, env management via
conda/poetry/renv, dataset mounting, GPU attach, share-as-snapshot,
and idle hibernation), the in-IDE rendering of Linter and Peer
Manager (the resources themselves are owned by the Compass checklist),
and the Palantir-style VS Code extension that authenticates against
OpenFoundry and edits remote Code Repos.

> **Scope distinction.** The **Linter** and **Peer Manager** resources
> (rule packs, review queues, approval rules as resource types) live
> in [foundry-compass-1to1-checklist.md](./foundry-compass-1to1-checklist.md).
> This file owns their **in-IDE rendering** (diagnostics, quick-fix,
> review pane) and the full **Code Repositories / Code Workspaces**
> stack. Containerized user code with its own image and runtime lives
> in [foundry-compute-modules-1to1-checklist.md](./foundry-compute-modules-1to1-checklist.md).
> The TS / Python **Functions runtime** is owned by
> [foundry-functions-runtime-1to1-checklist.md](./foundry-functions-runtime-1to1-checklist.md);
> this checklist only covers authoring those functions inside a Code
> Repository, not their execution layer.

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
| `P0` | Required for credible Code Repositories: repo resource, file editor, Git push/pull, branches, a build executor, transform publish to a dataset, lineage emission. |
| `P1` | Required for Foundry-style parity: PR review wired to Peer Manager, protected branches and checks, in-IDE Linter rendering, Jupyter / RStudio / VS Code-server workspaces, env management, dataset mounting, GPU attach. |
| `P2` | Advanced parity: share-as-snapshot, idle hibernation, multi-user collab editing, the Palantir-style VS Code extension, secrets vault integration, custom build executors, Marketplace publication of repo templates. |

## Official Palantir documentation library

### Product overview

- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)
- [Code Repositories overview](https://www.palantir.com/docs/foundry/code-repositories/overview)
- [Code Workspaces overview](https://www.palantir.com/docs/foundry/code-workspaces/overview)

### Code Repositories

- [Python transform overview](https://www.palantir.com/docs/foundry/code-repositories/python-overview)
- [Build and publish](https://www.palantir.com/docs/foundry/code-repositories/build-and-publish)
- [Checks and protected branches](https://www.palantir.com/docs/foundry/code-repositories/checks-and-protected-branches)
- [Repository templates](https://www.palantir.com/docs/foundry/code-repositories/templates)
- [Secrets and credentials](https://www.palantir.com/docs/foundry/code-repositories/secrets)
- [Profiling and optimization](https://www.palantir.com/docs/foundry/code-repositories/profiling)
- [Tags and releases](https://www.palantir.com/docs/foundry/code-repositories/tags)

### Code Workspaces

- [Jupyter workspaces](https://www.palantir.com/docs/foundry/code-workspaces/jupyter)
- [RStudio workspaces](https://www.palantir.com/docs/foundry/code-workspaces/rstudio)
- [VS Code workspaces](https://www.palantir.com/docs/foundry/code-workspaces/vscode-workspaces)
- [Configure environment](https://www.palantir.com/docs/foundry/code-workspaces/configure-environment)
- [Dataset mounting](https://www.palantir.com/docs/foundry/code-workspaces/datasets)
- [GPU workspaces](https://www.palantir.com/docs/foundry/code-workspaces/gpu)
- [Share as snapshot](https://www.palantir.com/docs/foundry/code-workspaces/share)
- [Idle hibernation](https://www.palantir.com/docs/foundry/code-workspaces/hibernation)

### VS Code extension

- [Palantir extension for Visual Studio Code](https://www.palantir.com/docs/foundry/developer-workspaces/vscode-extension)
- [FoundryTS in VS Code](https://www.palantir.com/docs/foundry/developer-workspaces/foundryts-vscode)

### Integrations

- [Peer Manager review workflow](https://www.palantir.com/docs/foundry/code-repositories/peer-review)
- [Linter rule packs](https://www.palantir.com/docs/foundry/code-repositories/linter)
- [Lineage emission from transforms](https://www.palantir.com/docs/foundry/code-repositories/lineage)
- [Functions authored in Code Repos](https://www.palantir.com/docs/foundry/code-repositories/functions)

## Milestone A: minimum viable Code Repositories parity

### Repository resource and storage

- [x] `CRW.1` Code repository resource (`P0`, `done`)
  - CRUD a `code_repository` resource in Compass with name, owner, organizations, markings, default branch, language template, and storage backend reference.
  - Repositories are first-class resources: trash, move, rename, and ACLs all follow Compass conventions.
  - Implemented in `code-repository-review-service` via `/v1/code-repos/repositories` CRUD, soft-trash/restore, move, rename, and persisted Compass metadata.
  - Docs: [Code Repositories overview](https://www.palantir.com/docs/foundry/code-repositories/overview).

- [x] `CRW.2` Git storage backend (`P0`, `done`)
  - Each repo is backed by a bare Git repository hosted by `code-repositories-service` (gitea/gitaly-style or a managed go-git server).
  - HTTPS push and pull are accepted with OIDC-backed credentials; SSH is optional and gated behind a feature flag.
  - Implemented in `code-repository-review-service` with managed bare repositories, Smart HTTP `/v1/code-repos/git/{id}.git`, OIDC bearer/basic-password auth, persisted clone URLs, and optional SSH URL emission behind `CODE_REPOSITORY_GIT_SSH_ENABLED`.
  - Docs: [Code Repositories overview](https://www.palantir.com/docs/foundry/code-repositories/overview).

- [x] `CRW.3` Per-language repository templates (`P0`, `done`)
  - Built-in templates for Python transform, Java transform, SQL transform, R transform, and TypeScript function repos.
  - Template seeds a working `transforms-python` / `transforms-java` / `transforms-sql` / `foundry-functions-typescript` skeleton with sample inputs and a passing build.
  - Implemented built-ins for Python, Java, SQL, R, and TypeScript function templates; creation seeds an initial Git commit into the managed bare repo and `/v1/code-repos/templates` lists template metadata.
  - Docs: [Repository templates](https://www.palantir.com/docs/foundry/code-repositories/templates).

### In-browser editor

- [x] `CRW.4` File tree and code editor (`P0`, `done`)
  - `apps/web` exposes a per-repo file tree, a Monaco-based editor with per-language syntax, multi-tab editing, save-on-blur, and unsaved-change warnings.
  - Right-click actions: new file, rename, delete, move; preserve Git semantics by routing through the service.
  - Implemented Monaco-backed multi-tab editing in `apps/web`, unsaved-change warnings, save-on-blur, file-tree context actions, and service-backed Git commits for file save/new/rename/move/delete.
  - Docs: [Code Repositories overview](https://www.palantir.com/docs/foundry/code-repositories/overview).

- [x] `CRW.5` Branch and tag lifecycle (`P0`, `done`)
  - Create, switch, delete, and merge branches via the UI and API. Tag creation with annotated tags for releases; protected tags require permission.
  - Default branch enforcement: deleting the default branch is blocked.
  - Implemented Git-backed branch list/create/switch/delete/merge and annotated tag list/create APIs plus `apps/web` branch/tag controls; default branch deletion is rejected and protected tag creation requires `code_repository:manage_protected_tags`.
  - Docs: [Tags and releases](https://www.palantir.com/docs/foundry/code-repositories/tags).

- [x] `CRW.6` Commit, push, and pull (`P0`, `done`)
  - Editor commits land as real Git commits with the actor's identity preserved (no shared bot user). Remote `git push` / `git pull` use ephemeral OIDC tokens.
  - Web UI commit dialog supports message body, sign-off, and atomic multi-file commits.
  - Implemented actor-stamped Git commits from OIDC claims, branch-aware commit history, atomic multi-file editor commits with message body/sign-off, and Smart HTTP push/pull remains token-gated through bearer/basic-password OIDC credentials.
  - Docs: [Code Repositories overview](https://www.palantir.com/docs/foundry/code-repositories/overview).

### Build executor and transform publish

- [ ] `CRW.7` Build executor service (`P0`, `todo`)
  - `build-executor-service` runs containerized builds triggered by pushes; each repo declares its language and the executor picks the matching base image.
  - Builds emit a structured log stream to the UI and a final pass/fail with diagnostics.
  - Docs: [Build and publish](https://www.palantir.com/docs/foundry/code-repositories/build-and-publish).

- [ ] `CRW.8` Transform compile and publish (`P0`, `todo`)
  - For transform repos, the executor compiles the transform graph, validates declared dataset inputs and outputs against Compass, and registers the transform on the target branch.
  - Publish writes a new transform spec resource consumable by the build orchestrator.
  - Docs: [Build and publish](https://www.palantir.com/docs/foundry/code-repositories/build-and-publish).

- [ ] `CRW.9` Dataset I/O contract validation (`P0`, `todo`)
  - The executor reads `@transform_df` / equivalent decorator metadata and checks that every input/output RID exists, is readable/writable, and matches the declared schema.
  - Mismatches fail the build with a typed `DatasetContractMismatch` diagnostic.
  - Docs: [Python transform overview](https://www.palantir.com/docs/foundry/code-repositories/python-overview).

- [ ] `CRW.10` Lineage emission on publish (`P0`, `todo`)
  - Successful publish emits lineage edges from input dataset RIDs to output dataset RIDs through the transform RID. Edges land in the lineage graph service in real time.
  - Docs: [Lineage emission from transforms](https://www.palantir.com/docs/foundry/code-repositories/lineage).

- [ ] `CRW.11` Per-repo build history and re-run (`P0`, `todo`)
  - Builds are recorded with commit SHA, branch, actor, duration, status, and log artifact reference.
  - Re-run from a previous commit is supported without requiring a new push.
  - Docs: [Build and publish](https://www.palantir.com/docs/foundry/code-repositories/build-and-publish).

### Audit

- [ ] `CRW.12` Repo audit events (`P0`, `todo`)
  - Every push, branch creation, branch deletion, merge, publish, and ACL change emits an audit event with actor, repo RID, branch, commit SHA, and request id.
  - Audit is consumable via the central audit query surface.
  - Docs: [Code Repositories overview](https://www.palantir.com/docs/foundry/code-repositories/overview).

## Milestone B: credible Foundry-style Code Repos and Code Workspaces parity

### Peer review and protected branches

- [ ] `CRW.13` Pull request lifecycle (`P1`, `todo`)
  - Create, comment, request-changes, approve, and merge PRs from the in-IDE PR pane. The PR resource itself is owned by Peer Manager (see Compass checklist), this item covers the embedded UI and the wiring.
  - Merge strategies: merge commit, squash, rebase.
  - Docs: [Peer Manager review workflow](https://www.palantir.com/docs/foundry/code-repositories/peer-review).

- [ ] `CRW.14` Inline diff and review comments (`P1`, `todo`)
  - Side-by-side and unified diff views with file-, hunk-, and line-level comments. Threaded replies and resolve/unresolve.
  - Comments are persisted alongside the PR resource and survive force-push (best-effort line tracking).
  - Docs: [Peer Manager review workflow](https://www.palantir.com/docs/foundry/code-repositories/peer-review).

- [ ] `CRW.15` Protected branches and required checks (`P1`, `todo`)
  - Per-branch policy: required reviewers (count and role), required passing checks, dismiss-stale-reviews, restrict pushers, restrict force-push.
  - Policy is stored in the repo resource and evaluated by Cedar at push-time.
  - Docs: [Checks and protected branches](https://www.palantir.com/docs/foundry/code-repositories/checks-and-protected-branches).

- [ ] `CRW.16` CI/CD checks pipeline (`P1`, `todo`)
  - Repos can declare a `checks.yaml` pipeline of named checks (build, lint, test, custom). Each check reports pass/fail with annotations bound to file+line.
  - Required checks block merge until green.
  - Docs: [Checks and protected branches](https://www.palantir.com/docs/foundry/code-repositories/checks-and-protected-branches).

### In-IDE Linter rendering

- [ ] `CRW.17` Linter diagnostics in editor (`P1`, `todo`)
  - The Linter resource (Compass) exposes rule packs; the editor renders matched diagnostics inline (squiggle + gutter icon + problems pane).
  - Diagnostics include severity, message, rule id, and a link to the rule documentation.
  - Docs: [Linter rule packs](https://www.palantir.com/docs/foundry/code-repositories/linter).

- [ ] `CRW.18` Autofix and quick-fix actions (`P1`, `todo`)
  - When a rule declares an autofix, the editor exposes a quick-fix code action that applies it as a single edit; "fix all" applies all safe fixes.
  - Docs: [Linter rule packs](https://www.palantir.com/docs/foundry/code-repositories/linter).

- [ ] `CRW.19` Linter as a CI gate (`P1`, `todo`)
  - The lint check participates in the checks pipeline; failing rules of severity `error` block merge into a protected branch.
  - Docs: [Checks and protected branches](https://www.palantir.com/docs/foundry/code-repositories/checks-and-protected-branches).

### Secrets and credentials

- [ ] `CRW.20` Per-repo secrets injection (`P1`, `todo`)
  - Repository settings can declare named secrets; the build executor mounts them as env vars or files only for the duration of the build.
  - Secrets are scoped per repo and per branch (production vs. preview).
  - Docs: [Secrets and credentials](https://www.palantir.com/docs/foundry/code-repositories/secrets).

- [ ] `CRW.21` Profiling artifacts (`P1`, `todo`)
  - Transform jobs can opt into producing profiling artifacts (py-spy, async-profiler, sparkmeasure) that surface in the build history pane as flame graphs.
  - Docs: [Profiling and optimization](https://www.palantir.com/docs/foundry/code-repositories/profiling).

### Code Workspaces: Jupyter, RStudio, VS Code-server

- [ ] `CRW.22` Code Workspace resource (`P1`, `todo`)
  - CRUD a `code_workspace` resource: name, owner, kind (`jupyter` / `rstudio` / `vscode`), base image, env spec reference, attached datasets, GPU class, lifecycle.
  - Docs: [Code Workspaces overview](https://www.palantir.com/docs/foundry/code-workspaces/overview).

- [ ] `CRW.23` Jupyter workspace runtime (`P1`, `todo`)
  - `code-workspaces-service` provisions a per-user JupyterLab pod with a persisted home volume, attached datasets read-only by default, and a kernel matching the declared env spec.
  - Idle eviction and resume are supported (see CRW.36).
  - Docs: [Jupyter workspaces](https://www.palantir.com/docs/foundry/code-workspaces/jupyter).

- [ ] `CRW.24` RStudio workspace runtime (`P1`, `todo`)
  - Provision an RStudio Server pod with a persisted home volume, an `renv`-managed library, and dataset mounts. R kernel version is part of the env spec.
  - Docs: [RStudio workspaces](https://www.palantir.com/docs/foundry/code-workspaces/rstudio).

- [ ] `CRW.25` VS Code-server workspace runtime (`P1`, `todo`)
  - Provision a `code-server` pod (open-source VS Code in the browser) with persisted home, extension marketplace mirror, and the same dataset-mount surface.
  - Docs: [VS Code workspaces](https://www.palantir.com/docs/foundry/code-workspaces/vscode-workspaces).

- [ ] `CRW.26` Per-user persisted home (`P1`, `todo`)
  - Each workspace has a per-user persisted volume mounted at `~/`. Quotas are enforced; over-quota triggers a soft-block on writes with an in-UI banner.
  - Docs: [Code Workspaces overview](https://www.palantir.com/docs/foundry/code-workspaces/overview).

- [ ] `CRW.27` Environment management (`P1`, `todo`)
  - Env specs support `conda` (Python), `poetry` (Python), `renv` (R), and `npm`/`pnpm` (TypeScript). Specs are versioned; rebuild is incremental when the lockfile didn't change.
  - Workspace can re-lock and commit the lockfile back to a connected repo.
  - Docs: [Configure environment](https://www.palantir.com/docs/foundry/code-workspaces/configure-environment).

- [ ] `CRW.28` Dataset mounting (`P1`, `todo`)
  - Attach a dataset to a workspace at a chosen mount path; reads stream through a FUSE-style mount that enforces permissions and markings.
  - Writes require explicit write-mount and emit lineage on commit.
  - Docs: [Dataset mounting](https://www.palantir.com/docs/foundry/code-workspaces/datasets).

- [ ] `CRW.29` GPU attach (`P1`, `todo`)
  - Workspace spec can request a GPU class (e.g., `gpu-small`, `gpu-large`); scheduler binds to GPU-capable nodes and exposes the device to the runtime.
  - Per-tenant GPU quotas are enforced.
  - Docs: [GPU workspaces](https://www.palantir.com/docs/foundry/code-workspaces/gpu).

- [ ] `CRW.30` Workspace audit events (`P1`, `todo`)
  - Start, stop, hibernate, resume, dataset attach/detach, env rebuild, share, and GPU attach all emit audit events with actor, workspace RID, and resource refs.
  - Docs: [Code Workspaces overview](https://www.palantir.com/docs/foundry/code-workspaces/overview).

## Milestone C: advanced parity

### Workspace sharing and lifecycle

- [ ] `CRW.31` Share-as-snapshot (`P2`, `todo`)
  - Owner can snapshot a workspace (home volume + env spec + attached datasets) into a read-only artifact that other users can fork into their own workspace.
  - Snapshots are versioned, permissioned, and can be marked.
  - Docs: [Share as snapshot](https://www.palantir.com/docs/foundry/code-workspaces/share).

- [ ] `CRW.32` Idle hibernation (`P2`, `todo`)
  - Workspaces idle past a per-tenant threshold (default 60 minutes) hibernate: pod is terminated, home volume retained. Resume restores the editor state.
  - Per-workspace override and an explicit "keep alive" toggle.
  - Docs: [Idle hibernation](https://www.palantir.com/docs/foundry/code-workspaces/hibernation).

### Collaboration

- [ ] `CRW.33` Multi-user collaborative editing (`P2`, `todo`)
  - VS Code-server and Jupyter workspaces support live-share-style sessions: a second authenticated user joins, sees cursors, and edits concurrently. Backed by a CRDT layer.
  - Sessions are audited and time-boxed.
  - Docs: [VS Code workspaces](https://www.palantir.com/docs/foundry/code-workspaces/vscode-workspaces).

### Palantir-style VS Code extension

- [ ] `CRW.34` VS Code extension authentication (`P2`, `todo`)
  - `palantir-vscode-extension` (renamed for OpenFoundry as the public-facing extension) authenticates the local VS Code instance against OpenFoundry via OIDC device-code flow; refresh tokens stored in the OS secret store.
  - Docs: [Palantir extension for Visual Studio Code](https://www.palantir.com/docs/foundry/developer-workspaces/vscode-extension).

- [ ] `CRW.35` Compass and Code Repos browsing from VS Code (`P2`, `todo`)
  - Extension exposes a Compass tree view, a Code Repos browser, and remote editing of files in a repo branch (commits go through the same audit path as web edits).
  - Docs: [Palantir extension for Visual Studio Code](https://www.palantir.com/docs/foundry/developer-workspaces/vscode-extension).

- [ ] `CRW.36` Run transforms from VS Code (`P2`, `todo`)
  - Extension can trigger a build and tail logs without leaving the editor; failed builds open a problems pane with the same diagnostics as the web UI.
  - Docs: [Palantir extension for Visual Studio Code](https://www.palantir.com/docs/foundry/developer-workspaces/vscode-extension).

- [ ] `CRW.37` FoundryTS integration in VS Code (`P2`, `todo`)
  - For TypeScript function repos, the extension wires up the FoundryTS-equivalent typings, IntelliSense for Ontology types, and a "run function locally with mock ontology" debug profile.
  - Docs: [FoundryTS in VS Code](https://www.palantir.com/docs/foundry/developer-workspaces/foundryts-vscode).

### Secrets and executors

- [ ] `CRW.38` Secrets vault integration (`P2`, `todo`)
  - Repo secrets can be backed by an external Vault Transit or KMS-backed store rather than the platform's own KMS; resolution happens at build-start with audit.
  - Docs: [Secrets and credentials](https://www.palantir.com/docs/foundry/code-repositories/secrets).

- [ ] `CRW.39` Custom build executors (`P2`, `todo`)
  - Tenants can register a custom executor image for a language stack (e.g., a hardened Python image with patched native libs) and bind it per-repo or per-branch.
  - Custom executors must pass an admission policy (signed image, allow-listed registry).
  - Docs: [Build and publish](https://www.palantir.com/docs/foundry/code-repositories/build-and-publish).

- [ ] `CRW.40` Marketplace publication of repo templates (`P2`, `todo`)
  - Repos can be published as templates into the platform Marketplace; consumers fork them into their own org with parameter substitution.
  - Docs: [Repository templates](https://www.palantir.com/docs/foundry/code-repositories/templates).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the existing Compass resource registry and its handling of nested resources, since `code_repository` and `code_workspace` must register there (see `libs/core-models/resource/registry.go`).
- [ ] `INV.2` Identify the Git server library to embed (go-git in-process, or an external Gitea-compatible service) and where its data volume lives in the dev k3s deployment.
- [ ] `INV.3` Identify the build orchestrator overlap with `services/build-orchestrator-service` (if present) and decide whether the build executor is a new service or an executor adapter.
- [ ] `INV.4` Identify how transform specs are persisted today (or if a new `transforms-registry-service` is needed) so publish (`CRW.8`) has a destination.
- [ ] `INV.5` Identify the lineage emission API (overlap with the lineage graph checklist) so publish writes are not a one-off path.
- [ ] `INV.6` Identify the secrets backend strategy: extend the existing Vault Transit integration in `identity-federation-service/internal/jwksrotation/vault_signer.go` or stand up a separate `secrets-service`.
- [ ] `INV.7` Identify the runtime for Code Workspaces pods: reuse the cluster used by Compute Modules, or a separate per-user workspace cluster with stricter isolation.
- [ ] `INV.8` Identify the dataset-mount FUSE layer and how it enforces markings live (overlap with the Object Storage V2 checklist).
- [ ] `INV.9` Identify the Peer Manager resource API endpoints so the in-IDE PR pane is a pure consumer (no duplicated PR storage).
- [ ] `INV.10` Identify the auth flow for the VS Code extension (OIDC device-code with the identity federation service) and where refresh tokens land in the OS keychain on each platform.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `code-repositories-service` | `code_repository` resource CRUD, Git protocol endpoints (HTTPS push/pull), branch/tag lifecycle, PR adapter to Peer Manager, protected-branch evaluation, secrets binding, audit emission, repo templates. |
| `code-workspaces-service` | `code_workspace` resource CRUD, per-user pod provisioning for Jupyter/RStudio/VS Code-server, persisted home volumes, env spec resolution, dataset mount wiring, GPU class scheduling, idle hibernation, share-as-snapshot. |
| `build-executor-service` | Containerized build runners, log streaming, transform compile + publish, dataset I/O contract validation, lineage emission, profiling artifact capture, custom executor admission. |
| `repo-build-runner` (lib) | Shared in-process helper used by `build-executor-service` (and optionally by the VS Code extension's "run locally" path) for transform graph parsing and diagnostics formatting. |
| `palantir-vscode-extension` | OpenFoundry-branded VS Code extension: OIDC auth, Compass browser, remote Code Repos editing, build trigger + log tail, FoundryTS-equivalent ontology typings. |
| `apps/web` | Repository editor (Monaco), file tree, branch/tag UI, PR review pane, in-IDE Linter diagnostics, build history, workspace launcher and lifecycle controls. |

## Acceptance criteria for first complete milestone

- A user can create a Python transform repository from a template, edit a file in the browser, push a commit, and watch the build executor run a transform that publishes a new dataset version with lineage edges visible in the lineage graph.
- A user can create a JupyterLab workspace, attach a dataset, run a notebook against it, and have the workspace hibernate after the configured idle window with no data loss on resume.
- A protected branch refuses a merge until a Peer Manager-approved review and the required checks (build + lint + tests) all pass.
- Linter diagnostics are visible inline in the web editor with severity, rule id, and a working autofix for at least one rule.
- Every push, build, publish, workspace start, dataset attach, and PR merge emits an audit event consumable from the central audit surface.

## Test plan expectations

- Integration tests (`go test -tags=integration`) exercise the Git push/pull path against an in-process or testcontainered Git backend.
- Integration tests run a sample Python transform end-to-end: commit, build, publish, assert lineage edges and audit events.
- Unit tests cover protected-branch policy evaluation (reviewer count, required checks, force-push restrictions) under Cedar.
- Unit tests cover env spec resolution and lockfile drift detection for `conda`, `poetry`, and `renv`.
- Frontend tests (`vitest`) cover the editor (file open, save, branch switch), the PR review pane, and the workspace launcher state machine.
- A smoke test in `smoke/` provisions a workspace, attaches a dataset, runs a notebook, hibernates, and resumes to validate the full lifecycle.
