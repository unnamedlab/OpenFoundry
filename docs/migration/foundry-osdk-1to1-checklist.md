# Foundry OSDK (Ontology SDK) 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Ontology SDK: typed
client generation per ontology version for TypeScript, Python, and Java;
object/link/action/function/query/agent surfaces; authentication and token
flows; runtime resolution against the ontology query service; pagination
and result caps; permission and marking enforcement on the server side;
Marketplace publication and consumption from external apps; CLI tooling
for local generation and CI integration.

> **Scope distinction.** This checklist covers the **Ontology SDK** —
> the typed-per-version client that external apps and OpenFoundry's own
> Workshop runtime consume. The broader server-side SDK generation
> service for protobuf clients is owned by
> [foundry-devops-marketplace-1to1-checklist.md](./foundry-devops-marketplace-1to1-checklist.md)
> and the existing `services/sdk-generation-service/`. The ontology
> model itself is owned by
> [foundry-ontology-manager-object-views-1to1-checklist.md](./foundry-ontology-manager-object-views-1to1-checklist.md).

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets,
screenshots, or any non-public behavior. The target is **functional parity
based on public Palantir Foundry documentation**.

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
| `P0` | Required for a credible OSDK: TypeScript generation, object/link/action surfaces, auth, ontology-version pinning. |
| `P1` | Required for Foundry-style parity: Python and Java SDKs, function/query/agent surfaces, pagination, error model, Marketplace publication. |
| `P2` | Advanced parity: incremental codegen, streaming subscriptions, BYO bundlers, generation diff guardrails, multi-region. |

## Official Palantir documentation library

### Product overview

- [OSDK overview](https://palantir.com/docs/foundry/ontology-sdk/overview)
- [OSDK getting started](https://palantir.com/docs/foundry/ontology-sdk/getting-started)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Generation and runtime

- [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation)
- [OSDK runtime](https://palantir.com/docs/foundry/ontology-sdk/runtime)
- [OSDK authentication](https://palantir.com/docs/foundry/ontology-sdk/authentication)
- [OSDK error model](https://palantir.com/docs/foundry/ontology-sdk/errors)

### Surfaces

- [Object surface](https://palantir.com/docs/foundry/ontology-sdk/objects)
- [Link traversal](https://palantir.com/docs/foundry/ontology-sdk/links)
- [Action invocation](https://palantir.com/docs/foundry/ontology-sdk/actions)
- [Function calls](https://palantir.com/docs/foundry/ontology-sdk/functions)
- [Query results](https://palantir.com/docs/foundry/ontology-sdk/queries)
- [Agent calls](https://palantir.com/docs/foundry/ontology-sdk/agents)

### Distribution

- [Marketplace OSDK deployment](https://palantir.com/docs/foundry/ontology-sdk/marketplace-osdk-deployment)

## Milestone A: credible TypeScript OSDK with object/link/action

### Generation pipeline

- [ ] `OSDK.1` Ontology version pinning (`P0`, `todo`)
  - Every OSDK generation is pinned to an explicit ontology version (semver-like or content hash).
  - The generated package metadata carries the ontology version, ontology RID, and the build timestamp.
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation).

- [ ] `OSDK.2` TypeScript codegen (`P0`, `todo`)
  - Generate an NPM-installable package per ontology version with: object type classes (typed properties), link traversal helpers, action invocation methods, function call methods, and a typed client factory.
  - Produce ESM + CJS dual output and TypeScript `.d.ts` declarations.
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation), [Object surface](https://palantir.com/docs/foundry/ontology-sdk/objects).

- [ ] `OSDK.3` Generation CLI (`P0`, `todo`)
  - CLI (`openfoundry osdk generate`) that takes an ontology RID + version and writes the package to disk.
  - Supports CI integration with a `--check` mode that fails on drift.
  - Docs: [OSDK getting started](https://palantir.com/docs/foundry/ontology-sdk/getting-started).

### Runtime client

- [ ] `OSDK.4` Client factory (`P0`, `todo`)
  - `createClient({ baseUrl, auth, ontologyRid })` returns a typed client.
  - Default to fetch-based transport with pluggable interceptors for logging and retry.
  - Docs: [OSDK runtime](https://palantir.com/docs/foundry/ontology-sdk/runtime).

- [ ] `OSDK.5` Object surface (`P0`, `todo`)
  - For each object type: `client.objects.<TypeName>.get(pk)`, `.search(predicate)`, `.create(input)`.
  - Typed predicates with property comparison and link traversal.
  - Docs: [Object surface](https://palantir.com/docs/foundry/ontology-sdk/objects).

- [ ] `OSDK.6` Link traversal (`P0`, `todo`)
  - `obj.<linkName>()` returns the linked object(s) with typed result; supports `.search(predicate)` on the far side.
  - Pagination on multi-target links.
  - Docs: [Link traversal](https://palantir.com/docs/foundry/ontology-sdk/links).

- [ ] `OSDK.7` Action invocation (`P0`, `todo`)
  - `client.actions.<ActionName>.apply(input)` with typed parameters and result.
  - Validation errors come back as typed exceptions, not generic `Error`.
  - Docs: [Action invocation](https://palantir.com/docs/foundry/ontology-sdk/actions).

### Authentication

- [ ] `OSDK.8` Token flows (`P0`, `todo`)
  - Support service-token (PAT) and OIDC bearer-token flows.
  - Token refresh handled transparently in the client.
  - Docs: [OSDK authentication](https://palantir.com/docs/foundry/ontology-sdk/authentication).

- [ ] `OSDK.9` Public-client (PKCE) flow for browser apps (`P0`, `todo`)
  - Browser OSDK supports PKCE login + token storage with secure defaults.
  - Token storage backend pluggable (sessionStorage default).
  - Docs: [OSDK authentication](https://palantir.com/docs/foundry/ontology-sdk/authentication).

### Error model

- [ ] `OSDK.10` Typed error model (`P0`, `todo`)
  - Standard error classes: `NotFoundError`, `PermissionError`, `ValidationError`, `RateLimitError`, `OntologyVersionMismatchError`, `MarkingDeniedError`, `NetworkError`.
  - Error responses from the server include a stable `error_code` enum the SDK maps to a class.
  - Docs: [OSDK error model](https://palantir.com/docs/foundry/ontology-sdk/errors).

## Milestone B: Python, Java, functions, queries, agents, Marketplace

### Python and Java SDKs

- [ ] `OSDK.11` Python codegen (`P1`, `todo`)
  - Generate a Python package (PEP 517/518) mirroring the TS surface; dataclasses for object types, typed methods.
  - Async (asyncio) and sync flavors share the same generated types.
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation).

- [ ] `OSDK.12` Java codegen (`P1`, `todo`)
  - Maven/Gradle artifact mirroring the TS surface; typed POJOs, fluent client.
  - Configurable HTTP layer (OkHttp default).
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation).

### Function, query, agent surfaces

- [ ] `OSDK.13` Function call methods (`P1`, `todo`)
  - For each public function type version: `client.functions.<FunctionName>.run(input)` with typed input/output.
  - Batched and async variants where the function declares support.
  - Docs: [Function calls](https://palantir.com/docs/foundry/ontology-sdk/functions).

- [ ] `OSDK.14` Query result surface (`P1`, `todo`)
  - For each query function: `client.queries.<QueryName>(args).asObjectSet()` returns a typed object-set handle that can be further filtered with predicates.
  - Lazy execution: nothing is fetched until iteration.
  - Docs: [Query results](https://palantir.com/docs/foundry/ontology-sdk/queries).

- [ ] `OSDK.15` Agent call methods (`P1`, `todo`)
  - For each published Agent (see [Agents checklist](./foundry-aip-agents-threads-assist-1to1-checklist.md)): `client.agents.<AgentName>.startThread()`, `.send(message)`, `.stream(message)`.
  - Tool invocations inside the agent honor the caller's permissions.
  - Docs: [Agent calls](https://palantir.com/docs/foundry/ontology-sdk/agents).

### Pagination, caps, and observability

- [ ] `OSDK.16` Pagination helpers (`P1`, `todo`)
  - Cursor-based pagination on every list/search method with an `.all()` iterator that fetches lazily.
  - Caps enforced server-side, mirrored client-side with a clear `MaxResultsReached` signal.
  - Docs: [OSDK runtime](https://palantir.com/docs/foundry/ontology-sdk/runtime).

- [ ] `OSDK.17` Request tracing (`P1`, `todo`)
  - Propagate W3C trace headers; expose a hook for the host app to attach its own observability.
  - Docs: [OSDK runtime](https://palantir.com/docs/foundry/ontology-sdk/runtime).

### Marketplace distribution

- [ ] `OSDK.18` OSDK packaging in Marketplace bundles (`P1`, `todo`)
  - A Marketplace product can include an OSDK as a deployable artifact pinned to the included ontology version.
  - On install, the OSDK is published to the enrollment's internal NPM/PyPI/Maven proxy.
  - Docs: [Marketplace OSDK deployment](https://palantir.com/docs/foundry/ontology-sdk/marketplace-osdk-deployment).

- [ ] `OSDK.19` Versioned OSDKs side-by-side (`P1`, `todo`)
  - Allow multiple OSDK versions for the same ontology to coexist in an enrollment so older apps keep working through an ontology migration.
  - Deprecation timeline metadata visible in Marketplace.
  - Docs: [Marketplace OSDK deployment](https://palantir.com/docs/foundry/ontology-sdk/marketplace-osdk-deployment).

## Milestone C: incremental gen, subscriptions, governance

### Incremental generation and guardrails

- [ ] `OSDK.20` Incremental codegen (`P2`, `todo`)
  - On ontology updates, regenerate only changed types/links/actions/functions/queries; produce a diff summary.
  - Cache hits keyed by ontology-version content hash.
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation).

- [ ] `OSDK.21` Breaking-change guardrails (`P2`, `todo`)
  - On regeneration, detect breaking changes (removed properties, narrowed types, removed methods); block publication without an explicit major version bump.
  - Generate a CHANGELOG with semver classification per change.
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation).

### Streaming subscriptions

- [ ] `OSDK.22` Object change subscriptions (`P2`, `todo`)
  - `client.objects.<TypeName>.subscribe(predicate, handler)` opens a server-sent-events stream of changes; the SDK delivers typed updates.
  - Subscription respects markings and permission changes; revoked permissions terminate the stream.
  - Docs: [OSDK runtime](https://palantir.com/docs/foundry/ontology-sdk/runtime).

- [ ] `OSDK.23` Agent stream messages (`P2`, `todo`)
  - Agent thread methods support `.stream()` returning incremental tool-call events and final message tokens.
  - Docs: [Agent calls](https://palantir.com/docs/foundry/ontology-sdk/agents).

### Governance and multi-region

- [ ] `OSDK.24` Marking-aware client (`P2`, `todo`)
  - The client surfaces a typed `MarkingDeniedError` when a method tries to read/write data the caller cannot access.
  - On bulk fetches, the client exposes the count of omitted records as part of the result envelope.
  - Docs: [OSDK error model](https://palantir.com/docs/foundry/ontology-sdk/errors).

- [ ] `OSDK.25` Region-pinned clients (`P2`, `todo`)
  - The client supports pinning to an enrollment region; requests never cross the region boundary unless the host explicitly opts in.
  - Required for the data-residency story in the Security/Governance checklist.
  - Docs: [OSDK runtime](https://palantir.com/docs/foundry/ontology-sdk/runtime).

- [ ] `OSDK.26` BYO bundler / target environments (`P2`, `todo`)
  - Generated TS package supports Vite, webpack, Next.js, Deno, Bun, Cloudflare Workers, Node.
  - Document feature flags per target (e.g. subscriptions require WebSocket support).
  - Docs: [OSDK generation](https://palantir.com/docs/foundry/ontology-sdk/generation).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current `services/sdk-generation-service/` capabilities and whether it can be the OSDK generation backend.
- [ ] `INV.2` Identify the ontology version + content-hash contract from the ontology-definition service.
- [ ] `INV.3` Identify the auth provider for service tokens and PKCE flows.
- [ ] `INV.4` Identify the internal NPM/PyPI/Maven proxies (overlap with Functions runtime).
- [ ] `INV.5` Identify the Marketplace product manifest format (overlap with DevOps/Marketplace).
- [ ] `INV.6` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `osdk-generation-service` | Per-ontology-version codegen for TS/Python/Java, incremental generation, breaking-change detection, package publication to internal proxies. |
| `osdk-runtime` | Generated package runtime (shared layer): transport, auth, pagination, error mapping, tracing, subscriptions. |
| `sdks/typescript`, `sdks/python`, `sdks/java` | Generated packages per ontology version, distributed via internal proxies (and optionally Marketplace). |
| `marketplace-service` | Bundle OSDKs into products and install them into target enrollments. |
| `apps/web` | OSDK explorer view per ontology version, generation history, install instructions. |
