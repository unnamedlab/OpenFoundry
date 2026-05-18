# Foundry Functions runtime 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Foundry Functions
runtime: function types (typed contracts) separated from functions
(implementations), TypeScript v1/v2 and Python runtimes, NPM/PyPI
dependency resolution, ontology reads and writes, staged writes with
human review, batched and async execution, function-as-tool exposure to
LLMs, function-backed Actions, query-as-function in OQL, deployment
lifecycle, branching, observability, and Marketplace packaging.

> **Scope distinction.** This checklist is dedicated to the **Functions
> runtime** (TS v1, TS v2, Python). It does **not** redefine Compute
> Modules (containerized custom code, owned by
> [foundry-compute-modules-1to1-checklist.md](./foundry-compute-modules-1to1-checklist.md)),
> nor AIP Logic (no-code orchestration blocks, owned by
> [foundry-aip-logic-evals-1to1-checklist.md](./foundry-aip-logic-evals-1to1-checklist.md)),
> nor Model Adapters
> ([foundry-model-integration-model-studio-1to1-checklist.md](./foundry-model-integration-model-studio-1to1-checklist.md)).
> Function-as-tool exposure to LLMs is owned here; the Agents app
> consumes it from
> [foundry-aip-agents-threads-assist-1to1-checklist.md](./foundry-aip-agents-threads-assist-1to1-checklist.md).

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets, or any
non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**.

> **Current OpenFoundry implementation note (2026-05-18).**
> `services/function-runtime-service` exists and is no longer a missing
> binary. Its v0 implementation includes a function registry, immutable
> versions, sync/async invocation, run lookup/listing, Postgres or in-memory
> persistence, TypeScript/Python subprocess executors, and service-local tests.
> The remaining P0 integration gaps are edge-gateway registration for
> `/api/v1/functions/*`, Helm/ArgoCD wiring, production-grade isolation, code
> repository blob fetch, and adapters from Workshop/Actions/AIP Logic into this
> runtime.

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
| `P0` | Required for credible Functions: function types vs. functions, TS v1 and Python runtimes, sync execution, ontology reads, deployment lifecycle. |
| `P1` | Required for Foundry-style parity: TS v2, staged writes, batched execution, function-backed Actions, function-as-tool for LLMs, NPM deps. |
| `P2` | Advanced parity: query-as-function in OQL, async/long-running, branched functions, marking-aware execution, Marketplace packaging. |

## Official Palantir documentation library

### Product overview

- [Functions overview](https://palantir.com/docs/foundry/functions/overview)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Function types and functions

- [Function types](https://palantir.com/docs/foundry/functions/function-types)
- [TypeScript v1](https://palantir.com/docs/foundry/functions/typescript-v1)
- [TypeScript v2](https://palantir.com/docs/foundry/functions/typescript-v2)
- [Python functions](https://palantir.com/docs/foundry/functions/python)

### Runtime

- [Function execution](https://palantir.com/docs/foundry/functions/execution)
- [Dependencies](https://palantir.com/docs/foundry/functions/dependencies)
- [Staged writes](https://palantir.com/docs/foundry/functions/staged-writes)
- [Batched execution](https://palantir.com/docs/foundry/functions/batched-execution)

### Integrations

- [Function-backed Actions](https://palantir.com/docs/foundry/functions/function-backed-actions)
- [Function-as-tool for LLMs](https://palantir.com/docs/foundry/functions/llm-tools)
- [Query-as-function in OQL](https://palantir.com/docs/foundry/functions/oql-queries)
- [Marketplace packaging](https://palantir.com/docs/foundry/functions/marketplace)

## Milestone A: credible Functions

### Function type (contract) model

- [ ] `FN.1` Function type resource (`P0`, `todo`)
  - CRUD a `function_type` resource: name, version, parameter schema (name, type, description, required, default), return type schema, throws schema, side-effects declaration (none, ontology-read, ontology-write, http, model-call), and visibility (project, organization, public).
  - Function types are independently versionable and reusable across multiple function implementations.
  - Stable RID, Compass-discoverable.
  - Docs: [Function types](https://palantir.com/docs/foundry/functions/function-types).

- [ ] `FN.2` Function implementation resource (`P0`, `todo`)
  - `function_impl` resource that binds a function type version to a source artifact (TS/Python), with build state, dependencies, and a deployment pointer.
  - Multiple implementations may bind to the same function type; one is the "active" implementation per environment.
  - Docs: [Function types](https://palantir.com/docs/foundry/functions/function-types).

- [ ] `FN.3` Version compatibility checks (`P0`, `todo`)
  - When publishing a new function type version, run compatibility checks (parameter additions only as optional, no type narrowing on return).
  - Block incompatible publishes unless an explicit major-version bump is requested.
  - Docs: [Function types](https://palantir.com/docs/foundry/functions/function-types).

### TypeScript v1 and Python runtimes

- [ ] `FN.4` TypeScript v1 runtime (`P0`, `todo`)
  - Node-based isolate per invocation, configurable memory and time budgets.
  - Standard library available; `fetch`/`http` disabled unless the side-effect declaration includes `http`.
  - Type definitions for ontology read clients generated from the active ontology version.
  - Docs: [TypeScript v1](https://palantir.com/docs/foundry/functions/typescript-v1).

- [ ] `FN.5` Python runtime (`P0`, `todo`)
  - Per-invocation Python interpreter (cpython) with allowlisted stdlib modules.
  - Same side-effect gating as TS.
  - Ontology client stub generated from ontology version.
  - Docs: [Python functions](https://palantir.com/docs/foundry/functions/python).

### Execution and ontology reads

- [ ] `FN.6` Synchronous execution API (`P0`, `todo`)
  - `POST /functions/{type_rid}/execute` with parameters; returns result, execution id, duration, and audit info.
  - Caller permissions are enforced for every ontology read inside the function (the function does not gain elevated permissions).
  - Docs: [Function execution](https://palantir.com/docs/foundry/functions/execution).

- [ ] `FN.7` Ontology read client (`P0`, `todo`)
  - Typed client exposing `Objects.get`, `Objects.search`, `Links.traverse`, `Queries.run` with the caller's permissions and markings.
  - Pagination and result caps enforced by the runtime, not by user code.
  - Docs: [Functions overview](https://palantir.com/docs/foundry/functions/overview).

### Deployment lifecycle

- [ ] `FN.8` Build pipeline (`P0`, `todo`)
  - On commit, build the function artifact (TS bundle or Python wheel), resolve dependencies, run lint and unit tests, and produce a versioned build with a build id.
  - Surface build state and logs in the Functions UI.
  - Docs: [Functions overview](https://palantir.com/docs/foundry/functions/overview).

- [ ] `FN.9` Deploy and roll back (`P0`, `todo`)
  - Promote a build to the active implementation for a function type version with one click.
  - Roll back to a previous build with audit.
  - Per-environment active implementation pointer (dev/staging/prod with Apollo handoff later).
  - Docs: [Function execution](https://palantir.com/docs/foundry/functions/execution).

## Milestone B: TS v2, staged writes, batched exec, integrations

### TypeScript v2 and ontology writes

- [ ] `FN.10` TypeScript v2 runtime (`P1`, `todo`)
  - Improved DX, ESM, top-level async, typed Action invocations, typed Query results.
  - Backward-compatible read API with v1.
  - Docs: [TypeScript v2](https://palantir.com/docs/foundry/functions/typescript-v2).

- [ ] `FN.11` Staged ontology writes (`P1`, `todo`)
  - In TS v2 and Python, allow `staged.update(object, patch)` calls that accumulate into a staged-edits set.
  - The runtime returns the staged edits to the caller; nothing is committed unless the caller invokes `commit()` or the wrapping Action's flow does.
  - Docs: [Staged writes](https://palantir.com/docs/foundry/functions/staged-writes).

- [ ] `FN.12` Human review for staged writes (`P1`, `todo`)
  - Function types may declare `requires_review: true`. Staged edits returned by these functions cannot be committed without an approver decision recorded in the audit trail.
  - Reviewer UI shows the staged diff before approval.
  - Docs: [Staged writes](https://palantir.com/docs/foundry/functions/staged-writes).

### Batched and async execution

- [ ] `FN.13` Batched execution API (`P1`, `todo`)
  - `POST /functions/{type_rid}/execute-batch` with an array of inputs; the runtime executes them concurrently up to a configured budget and returns per-input results and errors.
  - Stable ordering of results to inputs.
  - Docs: [Batched execution](https://palantir.com/docs/foundry/functions/batched-execution).

- [ ] `FN.14` Long-running async execution (`P1`, `todo`)
  - `POST /functions/{type_rid}/execute-async` returns an execution id; poll `GET /executions/{id}` for status, logs, and result.
  - Default timeout 5 min, extendable per function type.
  - Docs: [Function execution](https://palantir.com/docs/foundry/functions/execution).

### Dependencies

- [ ] `FN.15` NPM dependency resolution (`P1`, `todo`)
  - Per-function `package.json` resolved via an internal NPM proxy with an allowlist policy.
  - Vulnerability scan on every build; block known critical CVEs without override.
  - Docs: [Dependencies](https://palantir.com/docs/foundry/functions/dependencies).

- [ ] `FN.16` PyPI dependency resolution (`P1`, `todo`)
  - Same as NPM but for Python wheels; internal PyPI proxy with allowlist.
  - Pin transitive deps and lock per build.
  - Docs: [Dependencies](https://palantir.com/docs/foundry/functions/dependencies).

### Integrations: Actions and LLM tools

- [ ] `FN.17` Function-backed Actions (`P1`, `todo`)
  - Bind an Action type's behavior to a function type version; the action invokes the function and applies any staged writes through the Action flow (with validation, side effects, audit).
  - Docs: [Function-backed Actions](https://palantir.com/docs/foundry/functions/function-backed-actions).

- [ ] `FN.18` Function-as-tool for LLMs (`P1`, `todo`)
  - Generate an LLM-compatible tool spec (JSON schema for parameters and return) from a function type.
  - Tool invocations from an Agent pass the caller's permissions to the function runtime; the LLM never bypasses permission checks.
  - Docs: [Function-as-tool for LLMs](https://palantir.com/docs/foundry/functions/llm-tools).

- [ ] `FN.19` Unit tests in-runtime (`P1`, `todo`)
  - Co-locate unit tests next to the function source; run on every build with a per-test result surfaced in the UI.
  - Snapshot tests for ontology read responses (using a fixed test dataset).
  - Docs: [Functions overview](https://palantir.com/docs/foundry/functions/overview).

## Milestone C: query-as-function, branches, governance, packaging

### Query-as-function and OQL integration

- [ ] `FN.20` Query function type kind (`P2`, `todo`)
  - A function type can declare itself as a "query function" that returns an object set or a tabular result. The OQL planner can call it as a derived source.
  - Docs: [Query-as-function in OQL](https://palantir.com/docs/foundry/functions/oql-queries).

- [ ] `FN.21` OQL pushdown hooks (`P2`, `todo`)
  - For query functions written in a constrained DSL, allow pushdown so the OQL planner combines the function with surrounding predicates instead of materializing.
  - Functions in TS/Python without DSL declaration fall back to materialize-and-join.
  - Docs: [Query-as-function in OQL](https://palantir.com/docs/foundry/functions/oql-queries).

### Branched and marking-aware execution

- [ ] `FN.22` Branch-aware execution (`P2`, `todo`)
  - When invoked under a branch context, reads target branched ontology versions and writes (staged) go to the branch.
  - Function impl deployments may themselves be branched; the active impl on a branch is the branch-scoped impl when present, else main.
  - Docs: [Function execution](https://palantir.com/docs/foundry/functions/execution).

- [ ] `FN.23` Marking-aware reads and writes (`P2`, `todo`)
  - The function runtime resolves caller clearances on every read; reads of objects/properties the caller cannot access return errors or omit values per policy.
  - Staged writes inherit markings from the affected objects; no escalation possible.
  - Docs: [Functions overview](https://palantir.com/docs/foundry/functions/overview).

### Observability

- [ ] `FN.24` Per-execution observability (`P2`, `todo`)
  - Emit execution traces (OTel) with parent context from the caller (Workshop, Action, Agent, etc.).
  - Logs and per-call metrics (latency p50/p95/p99, error rate) consumable from the Functions detail UI.
  - Docs: [Function execution](https://palantir.com/docs/foundry/functions/execution).

- [ ] `FN.25` Cost accounting (`P2`, `todo`)
  - Per-function CPU·s and memory·s accounting; attribute to the calling user/project.
  - Surface a "top consumers" view in the Functions admin.
  - Docs: [Function execution](https://palantir.com/docs/foundry/functions/execution).

### Marketplace packaging

- [ ] `FN.26` Functions in Marketplace bundles (`P2`, `todo`)
  - Pack a function type + implementation + dependencies + tests into a Marketplace product entry.
  - Install resolves dependencies in the target enrollment's package proxies.
  - Docs: [Marketplace packaging](https://palantir.com/docs/foundry/functions/marketplace).

- [ ] `FN.27` OSDK exposure (`P2`, `todo`)
  - Generate OSDK client methods for each function type version (see [OSDK checklist](./foundry-osdk-1to1-checklist.md)).
  - Typed parameters and return; permission checks remain server-side.
  - Docs: [Function types](https://palantir.com/docs/foundry/functions/function-types).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current Functions UI surface and what backend it points to.
- [ ] `INV.2` Identify the isolate/sandbox runtime to use for TS and Python.
- [ ] `INV.3` Identify the NPM/PyPI proxy strategy and allowlist policy owner.
- [ ] `INV.4` Identify the ontology read/write client that the function runtime will use, with marking-aware enforcement.
- [ ] `INV.5` Identify the audit/observability paths the runtime will emit into.
- [ ] `INV.6` Identify the staged-writes storage and reviewer UI owner (likely overlaps with the Ontology checklist).
- [ ] `INV.7` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `functions-control-service` | Function type and implementation CRUD, version compatibility checks, deployment pointer per environment. |
| `functions-build-service` | Compile TS/Python, resolve deps from the internal proxies, run lint and tests, produce build artifacts. |
| `functions-runtime-service` | Per-invocation sandboxed execution (TS isolate, Python interpreter), permission-aware ontology client, staged writes accumulator, OTel tracing. |
| `functions-async-service` | Long-running execution queue, status polling, batched execution scheduler. |
| `apps/web` | Functions IDE UX, function type/impl detail, build state, deployments, tests, executions log, staged-writes review. |
