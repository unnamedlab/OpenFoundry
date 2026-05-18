# Foundry Developer Console, Custom Endpoints, and MCP servers 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's developer-platform
surfaces that expose the platform to third-party apps and AI tools. The
checklist covers (1) the Developer Console for third-party application
registration, OAuth2 clients (confidential and public), scope selection,
redirect URIs, per-app credentials and key rotation, per-app token
quotas, per-app audit, organization owner, public-app review workflow,
and an app analytics dashboard; (2) Custom Endpoints that route a public
HTTP URL to a Function or Compute Module with declared request/response
schemas, auth modes, rate limits, CORS, payload limits, an OpenAPI
export, custom domain mapping, and per-route observability; (3) the
FoundryTS framework as the Developer-Console-facing SDK surface for
typed function decorators, ontology types codegen, the FoundryTS CLI,
project scaffolding, local debug, and Developer-Console-driven
publication; (4) an Ontology MCP server that exposes the Ontology to MCP
clients with per-tool exposure (object queries, actions, functions,
agents), OAuth2 auth, capability negotiation, observability, and
per-client quotas; (5) a Palantir-style MCP server with a broader
surface (datasets, pipelines, Slate apps, Workshop pages, dashboards);
and (6) cross-application interactivity contracts (deep links, URL
parameter conventions, event-bus refresh, save-as-X handoff).

> **Scope distinction.** Developer Console here owns **app and OAuth
> client registration**; the user-side OAuth and token surface
> (authorization-code flow, token introspection, session management)
> remains in
> [foundry-security-governance-1to1-checklist.md](./foundry-security-governance-1to1-checklist.md).
> Custom Endpoints expose Functions and Compute Modules to the public
> internet but the **runtime** that executes them stays in
> [foundry-functions-runtime-1to1-checklist.md](./foundry-functions-runtime-1to1-checklist.md)
> and
> [foundry-compute-modules-1to1-checklist.md](./foundry-compute-modules-1to1-checklist.md).
> OSDK codegen remains in
> [foundry-osdk-1to1-checklist.md](./foundry-osdk-1to1-checklist.md);
> this checklist only covers the Developer-Console-facing publication
> workflow of FoundryTS, not the runtime SDK shape itself.

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
| `P0` | Required for a credible developer platform: app CRUD, OAuth2 client (confidential + public), scope selection, redirect URIs, custom endpoint route to a Function, OpenAPI export. |
| `P1` | Required for Foundry-style parity: per-app token quotas, public-app review workflow, FoundryTS Developer-Console publication, Ontology MCP basic tools, per-route rate limit and CORS. |
| `P2` | Advanced parity: broader Palantir MCP surface, MCP capability negotiation, cross-app deep-link contracts, custom domain mapping, app analytics dashboards. |

## Official Palantir documentation library

### Developer Console

- [Developer Console overview](https://www.palantir.com/docs/foundry/developer-console/overview)
- [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications)
- [Scopes catalog](https://www.palantir.com/docs/foundry/developer-console/scopes)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Custom Endpoints

- [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints)
- [Expose Functions as endpoints](https://www.palantir.com/docs/foundry/functions/expose-functions-as-endpoints)

### FoundryTS

- [FoundryTS overview](https://www.palantir.com/docs/foundry/foundry-ts/overview)
- [FoundryTS CLI](https://www.palantir.com/docs/foundry/foundry-ts/cli)

### Ontology MCP

- [Ontology MCP overview](https://www.palantir.com/docs/foundry/ontology-mcp/overview)
- [Configure Ontology MCP](https://www.palantir.com/docs/foundry/ontology-mcp/configure)

### Palantir MCP

- [Palantir MCP overview](https://www.palantir.com/docs/foundry/palantir-mcp/overview)

### Cross-application interactivity

- [Cross-application interactivity](https://www.palantir.com/docs/foundry/developer-toolchain/cross-application-interactivity)

## Milestone A: minimum viable Developer Console and Custom Endpoints parity

### Application registry

- [ ] `DCE.1` Application CRUD (`P0`, `todo`)
  - Developer Console exposes a create/read/update/archive lifecycle for third-party applications scoped to an organization.
  - Each application carries: display name, description, logo, homepage URL, support contact, organization owner, visibility (private to org / public after review), and a stable `app_rid`.
  - Application archive is reversible for a configurable retention window before hard delete.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.2` Organization owner and co-maintainers (`P0`, `todo`)
  - Every application has exactly one organization owner and a set of co-maintainers with declared roles (`admin`, `developer`, `viewer`).
  - Ownership transfer requires confirmation from the receiving organization owner; audit emits a transfer event.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

### OAuth2 client model

- [ ] `DCE.3` Confidential OAuth2 client (`P0`, `todo`)
  - Each application can register one or more confidential OAuth2 clients with a generated `client_id` and a `client_secret` shown once at creation.
  - Supported grant types: `client_credentials`, `authorization_code` with PKCE optional, `refresh_token`.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.4` Public OAuth2 client with PKCE (`P0`, `todo`)
  - Public client variant (no secret) supporting `authorization_code` with PKCE mandatory.
  - Disallows `client_credentials`; rejects grants attempting to use a secret.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.5` Redirect URI registry (`P0`, `todo`)
  - Each client lists allowed redirect URIs with exact-match validation (no wildcard substitution); HTTPS required except for `http://localhost` and `http://127.0.0.1` in development.
  - Authorization endpoint rejects unregistered URIs with a stable `invalid_redirect_uri` error.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.6` Scope selection from catalog (`P0`, `todo`)
  - Developer Console surfaces a scope catalog grouped by surface (ontology read/write, datasets read, pipelines run, functions invoke, MCP tools, custom endpoints).
  - Applications request a subset; consent screen during user authorization shows the human-readable scope description.
  - Docs: [Scopes catalog](https://www.palantir.com/docs/foundry/developer-console/scopes).

- [ ] `DCE.7` Credential rotation (`P0`, `todo`)
  - Confidential clients can rotate `client_secret` with a configurable overlap window where both the old and new secret are valid.
  - Rotation emits an audit event and the old secret is removed at the end of the overlap window.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

### Custom endpoints minimum viable surface

- [ ] `DCE.8` Custom endpoint route (`P0`, `todo`)
  - A developer can register a public HTTP route (`/api/custom/<org-slug>/<endpoint-slug>`) that maps to a Function version or a Compute Module deployment.
  - Route metadata: HTTP method, request/response content types, owner application.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.9` Request and response schemas (`P0`, `todo`)
  - Each endpoint declares a request schema (JSON Schema derived from the Function signature) and a response schema; the gateway validates inbound payloads and rejects with `400` on mismatch.
  - Schemas are versioned together with the endpoint binding to the Function version.
  - Docs: [Expose Functions as endpoints](https://www.palantir.com/docs/foundry/functions/expose-functions-as-endpoints).

- [ ] `DCE.10` Endpoint auth modes (`P0`, `todo`)
  - Endpoint declares one of: `oauth2` (bearer token from a Developer-Console-registered client), `api_key` (per-application long-lived key), `service` (internal-only, mTLS), `public` (anonymous, with stricter rate-limit defaults).
  - Public mode requires explicit org-admin approval; emits an audit event on toggle.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.11` OpenAPI export per app (`P0`, `todo`)
  - Developer Console exposes `/api/developer-console/v1/applications/{app_rid}/openapi` returning an OpenAPI 3.1 document covering all custom endpoints owned by the application.
  - The document includes auth schemes, scope requirements, and request/response schemas.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.12` Endpoint invocation via Function (`P0`, `todo`)
  - The custom-endpoints gateway resolves the bound Function version, attaches the caller's claims and the application identity, and invokes the Functions runtime.
  - Response is streamed back if the Function declares streaming output.
  - Docs: [Expose Functions as endpoints](https://www.palantir.com/docs/foundry/functions/expose-functions-as-endpoints).

## Milestone B: credible Foundry-style developer-platform parity

### Quotas and review

- [ ] `DCE.13` Per-application token quotas (`P1`, `todo`)
  - Configurable quotas per application: tokens issued per minute, concurrent active sessions, total monthly active users; quota exceeded returns `429` with a `Retry-After` header.
  - Org admins can raise quotas; raises are audit-logged.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.14` Public-app review workflow (`P1`, `todo`)
  - To list an application as public (consentable by users outside the owner organization) it must pass a review: scope justification, redirect URI verification, privacy policy URL, support email reachable, abuse contact.
  - Reviews have states `draft`, `submitted`, `in_review`, `approved`, `rejected`, `revoked`; reviewer notes are stored.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.15` Per-application audit feed (`P1`, `todo`)
  - Developer Console exposes a feed of audit events for an application: secret rotated, scope added, redirect URI changed, owner transferred, review state changed, quota exceeded.
  - Feed is queryable and exportable (CSV/JSON) by org admins.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

### Custom endpoints production controls

- [ ] `DCE.16` Per-client rate limit (`P1`, `todo`)
  - Each endpoint declares a default rate limit; per-client overrides applied at gateway.
  - Limiter is token-bucket with per-client buckets keyed by `client_id` (OAuth2) or API key hash.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.17` CORS policy per endpoint (`P1`, `todo`)
  - Endpoint declares allowed origins, methods, headers, credentials flag, and max-age; gateway emits CORS preflight responses without invoking the Function.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.18` Payload size limits (`P1`, `todo`)
  - Per-endpoint request and response size limits; oversize requests rejected with `413` before the Function is invoked.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.19` Per-route observability (`P1`, `todo`)
  - Gateway emits per-route metrics: requests, p50/p95/p99 latency, error rate by code, payload sizes; metrics surfaced in the Developer Console UI per app.
  - W3C trace headers propagated into the Functions runtime.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

### FoundryTS publication path

- [ ] `DCE.20` FoundryTS project scaffolding (`P1`, `todo`)
  - `foundry-ts init` scaffolds a TypeScript project with typed function decorators, ontology types codegen wiring, a sample function, and a `foundry-ts.config.ts` declaring the target enrollment.
  - Docs: [FoundryTS overview](https://www.palantir.com/docs/foundry/foundry-ts/overview).

- [ ] `DCE.21` Typed function decorators (`P1`, `todo`)
  - Decorators (or higher-order helpers) annotate exported functions with: input/output schemas (inferred from TS types), scope requirements, idempotency key, and timeout.
  - Build step emits a manifest consumable by the Functions runtime.
  - Docs: [FoundryTS overview](https://www.palantir.com/docs/foundry/foundry-ts/overview).

- [ ] `DCE.22` Ontology types codegen in FoundryTS (`P1`, `todo`)
  - `foundry-ts codegen` pulls the ontology version and emits typed object/link/action/query types that the project's functions can import.
  - Generation is incremental and cache-keyed by ontology content hash.
  - Docs: [FoundryTS overview](https://www.palantir.com/docs/foundry/foundry-ts/overview).

- [ ] `DCE.23` FoundryTS CLI (`P1`, `todo`)
  - CLI surface: `init`, `codegen`, `build`, `dev` (local debug), `publish`, `logs`, `status`; `--enrollment` flag selects a target.
  - Configuration file `foundry-ts.config.ts` is the single source of truth and `--check` mode fails on drift.
  - Docs: [FoundryTS CLI](https://www.palantir.com/docs/foundry/foundry-ts/cli).

- [ ] `DCE.24` Local debug runtime (`P1`, `todo`)
  - `foundry-ts dev` starts a local HTTP server that mounts the project's functions, proxies ontology calls to the target enrollment, and supports breakpoints via the Node inspector protocol.
  - Hot-reload on source change; trace IDs match the production format.
  - Docs: [FoundryTS overview](https://www.palantir.com/docs/foundry/foundry-ts/overview).

- [ ] `DCE.25` Developer-Console-driven publication (`P1`, `todo`)
  - `foundry-ts publish` packages the project, uploads it to the target application's Developer Console entry, and produces a versioned deployment that can be promoted from `staging` to `live`.
  - Promotion requires the application's org-admin role.
  - Docs: [FoundryTS CLI](https://www.palantir.com/docs/foundry/foundry-ts/cli).

### Ontology MCP basic tools

- [ ] `DCE.26` MCP server transport (`P1`, `todo`)
  - Ontology MCP server speaks MCP over HTTP+SSE and over stdio; both transports share the same tool catalog.
  - Server announces its protocol version and supported features on handshake.
  - Docs: [Ontology MCP overview](https://www.palantir.com/docs/foundry/ontology-mcp/overview).

- [ ] `DCE.27` Per-tool exposure: object queries (`P1`, `todo`)
  - MCP tool `ontology.object.search`, `ontology.object.get`, `ontology.link.traverse` exposed to MCP clients; tool schemas derived from the ontology types.
  - Results respect the caller's markings and permissions.
  - Docs: [Ontology MCP overview](https://www.palantir.com/docs/foundry/ontology-mcp/overview).

- [ ] `DCE.28` Per-tool exposure: actions (`P1`, `todo`)
  - MCP tool `ontology.action.apply` enumerates action types the caller is permitted to apply; invocations route through the same validation as the Workshop UI.
  - Docs: [Ontology MCP overview](https://www.palantir.com/docs/foundry/ontology-mcp/overview).

- [ ] `DCE.29` Per-tool exposure: functions and agents (`P1`, `todo`)
  - MCP tools `ontology.function.run` and `ontology.agent.message` for published functions and agents respectively.
  - Streaming responses (function streaming output, agent token stream) propagated as MCP partial results.
  - Docs: [Ontology MCP overview](https://www.palantir.com/docs/foundry/ontology-mcp/overview).

- [ ] `DCE.30` MCP OAuth2 auth (`P1`, `todo`)
  - MCP server uses Developer-Console-registered OAuth2 clients; supports the OAuth2 flow described in the MCP specification (authorization code + PKCE for interactive clients, client credentials for service clients).
  - Tokens carry the same scopes as the REST surface; tool calls are filtered by granted scopes.
  - Docs: [Configure Ontology MCP](https://www.palantir.com/docs/foundry/ontology-mcp/configure).

- [ ] `DCE.31` MCP configuration UI (`P1`, `todo`)
  - Developer Console UI to configure which tools the Ontology MCP server exposes per application (allow/deny list per surface, per object type, per action type).
  - Configuration changes are audited.
  - Docs: [Configure Ontology MCP](https://www.palantir.com/docs/foundry/ontology-mcp/configure).

## Milestone C: advanced parity

### Palantir MCP broader surface

- [ ] `DCE.32` Palantir MCP server (`P2`, `todo`)
  - Separate MCP server exposing a broader platform surface: datasets (`datasets.search`, `datasets.read_sample`), pipelines (`pipelines.run`, `pipelines.status`), Slate apps (`slate.app.embed_url`, `slate.app.execute_block`), Workshop pages (`workshop.page.deep_link`, `workshop.module.execute`), dashboards (`dashboards.snapshot`).
  - Docs: [Palantir MCP overview](https://www.palantir.com/docs/foundry/palantir-mcp/overview).

- [ ] `DCE.33` Capability negotiation (`P2`, `todo`)
  - On handshake the MCP client and server negotiate optional capabilities: streaming partial results, large-payload chunking, structured tool errors, resource subscriptions.
  - Server gracefully degrades when the client lacks a capability rather than failing the session.
  - Docs: [Palantir MCP overview](https://www.palantir.com/docs/foundry/palantir-mcp/overview).

- [ ] `DCE.34` Per-client MCP quotas (`P2`, `todo`)
  - Tool-call quotas per MCP client (calls/minute, concurrent sessions, payload bytes/day); excess returns a structured MCP error.
  - Quotas configurable per application via the Developer Console.
  - Docs: [Configure Ontology MCP](https://www.palantir.com/docs/foundry/ontology-mcp/configure).

- [ ] `DCE.35` MCP observability (`P2`, `todo`)
  - Per-tool metrics (call count, latency, error breakdown), per-session trace, audit feed of all tool invocations surfaced in the Developer Console.
  - Docs: [Ontology MCP overview](https://www.palantir.com/docs/foundry/ontology-mcp/overview).

### Cross-application interactivity

- [ ] `DCE.36` Deep-link contract (`P2`, `todo`)
  - Stable URL schema for cross-app navigation: `/<app>/<resource-kind>/<resource-rid>?<param>=...`; every first-party app (Workshop, Object View, Quiver, Slate, Dashboards) registers its accepted parameters.
  - Outbound deep-link helpers in shared libs return canonical URLs.
  - Docs: [Cross-application interactivity](https://www.palantir.com/docs/foundry/developer-toolchain/cross-application-interactivity).

- [ ] `DCE.37` URL parameter conventions (`P2`, `todo`)
  - Reserved parameter names: `objectRid`, `objectSetRid`, `branchId`, `markingsId`, `filter`, `selection`; all first-party apps interpret these identically.
  - Unknown parameters are preserved across navigations but not interpreted.
  - Docs: [Cross-application interactivity](https://www.palantir.com/docs/foundry/developer-toolchain/cross-application-interactivity).

- [ ] `DCE.38` Event bus for cross-app refresh (`P2`, `todo`)
  - In-browser event bus (BroadcastChannel + server SSE fallback) emitting events like `ontology.object.updated`, `dataset.committed`, `action.applied`; embedded iframes subscribe and refresh.
  - Bus events are scoped to the user session and not persisted.
  - Docs: [Cross-application interactivity](https://www.palantir.com/docs/foundry/developer-toolchain/cross-application-interactivity).

- [ ] `DCE.39` Save-as-X handoff (`P2`, `todo`)
  - Standard handoff protocol so e.g. a Quiver analysis can be saved as a Workshop page, a Slate visualization can be exported to a Dashboard, etc.
  - Handoff carries a typed payload (`source_kind`, `source_rid`, `target_kind`, `payload`) and the receiving app opens in a scaffold-from-payload mode.
  - Docs: [Cross-application interactivity](https://www.palantir.com/docs/foundry/developer-toolchain/cross-application-interactivity).

### Production polish

- [ ] `DCE.40` Custom domain mapping (`P2`, `todo`)
  - An application can map a custom domain (e.g. `api.acme.example.com`) to its custom endpoints via DNS validation and managed TLS (ACME).
  - Domain status tracks `pending_dns`, `pending_tls`, `active`, `revoked`.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.41` App analytics dashboard (`P2`, `todo`)
  - Per-application dashboard with: active users, token issuance volume, top scopes requested, top custom endpoints by invocation, top MCP tools by call count, error rate, p95 latency.
  - Time-range selector (last 24h / 7d / 30d / custom); exportable.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

- [ ] `DCE.42` Webhook delivery from custom endpoints (`P2`, `todo`)
  - Outbound webhook variant of custom endpoints: an event source (action applied, dataset committed, automation fired) posts a signed payload to the application's configured webhook URL.
  - Delivery retries with exponential backoff and a dead-letter visible to org admins.
  - Docs: [Custom endpoints overview](https://www.palantir.com/docs/foundry/developer-console/custom-endpoints).

- [ ] `DCE.43` Scope grant explorer (`P2`, `todo`)
  - User-facing view (under the Security/Governance account page) listing every application that has been granted scopes by the user, with one-click revoke.
  - Revocation invalidates issued tokens and emits an audit event for both the user and the application owner.
  - Docs: [Scopes catalog](https://www.palantir.com/docs/foundry/developer-console/scopes).

- [ ] `DCE.44` MCP resource subscriptions (`P2`, `todo`)
  - Ontology MCP server supports MCP `resources/subscribe` so a client (e.g. an editor agent) is notified when an object set changes.
  - Subscription respects markings and is auto-terminated on permission revocation.
  - Docs: [Configure Ontology MCP](https://www.palantir.com/docs/foundry/ontology-mcp/configure).

- [ ] `DCE.45` Cross-region application replication (`P2`, `todo`)
  - Applications and their OAuth2 clients can be replicated across enrollment regions for failover; secrets are re-wrapped per-region via the platform KMS.
  - Replication state visible in the Developer Console.
  - Docs: [Manage applications](https://www.palantir.com/docs/foundry/developer-console/manage-applications).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current OAuth2 implementation in `services/identity-federation-service/` and whether `client_credentials`/`authorization_code`/`refresh_token` flows already exist; map the missing pieces (PKCE, public clients, per-app secret store with rotation overlap).
- [ ] `INV.2` Identify the existing edge gateway (`services/edge-gateway-service/`) router-table model and whether it can host the custom-endpoints route plane or whether a dedicated `custom-endpoints-gateway` is warranted.
- [ ] `INV.3` Identify how the Functions runtime (`services/functions-runtime-service/`) is invoked today (sync vs queue) and what binding metadata is needed to route a custom endpoint to a function version.
- [ ] `INV.4` Identify the existing scope catalog (if any) used by `libs/auth-middleware` and decide whether scopes should live in `identity-federation-service` or a new `developer-console-service`.
- [ ] `INV.5` Identify the audit emission path shared with Security/Governance and confirm event schemas for `app.created`, `app.secret_rotated`, `app.scope_changed`, `app.review_state_changed`, `endpoint.invoked`, `mcp.tool_called`.
- [ ] `INV.6` Identify the parts of the Ontology surface (object search, action apply, function run, agent message) that already expose stable HTTP APIs the MCP server can wrap without re-implementing logic.
- [ ] `INV.7` Identify the FoundryTS overlap with the OSDK generation service (`services/sdk-generation-service/`): is FoundryTS codegen a thin wrapper over OSDK codegen, or a separate code path?
- [ ] `INV.8` Identify the deep-link conventions already in use across `apps/web` routes and document the gap to a stable cross-app contract.
- [ ] `INV.9` Identify the rate-limiting building blocks (token-bucket library, Redis instance) available to the gateway.
- [ ] `INV.10` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

> **Reader note (2026-05-18)** — The services in the table below are
> target decomposition proposals, not a current inventory of binaries.
> `mcp-ontology-service` and `mcp-platform-service` do not exist as
> current `services/` directories; the table documents proposed future
> boundaries. For the canonical service list, see
> [`docs/reference/repository-layout.md`](../reference/repository-layout.md).

| Surface | Responsibilities |
| --- | --- |
| `developer-console-service` | Application CRUD, organization owner, co-maintainers, OAuth2 client registration (confidential + public), scope catalog, redirect URI registry, credential rotation, review workflow, per-application audit feed, per-app token quotas, app analytics aggregation. |
| `custom-endpoints-gateway` | Public ingress for `/api/custom/...`, request/response schema validation, per-client rate limiting, CORS, payload-size enforcement, OpenAPI export, custom domain + TLS, webhook delivery, per-route observability; resolves the bound Function version and invokes the Functions runtime. |
| `mcp-ontology-service` | Ontology MCP server (HTTP+SSE and stdio transports), per-tool exposure for objects/actions/functions/agents, OAuth2 auth, per-client quotas, resource subscriptions, observability. |
| `mcp-platform-service` | Palantir-style broader MCP server exposing datasets, pipelines, Slate apps, Workshop pages, dashboards; capability negotiation; shares auth and quota substrate with `mcp-ontology-service`. |
| `foundry-ts-cli` (under `tools/`) | FoundryTS CLI: project scaffolding, ontology codegen, build, local debug server, publish to Developer Console, status/logs commands. |
| `apps/web` Developer Console UI | Application management, OAuth2 client configuration, scope selection, review submission, custom endpoint editor with OpenAPI preview, MCP tool exposure editor, app analytics dashboard, per-application audit feed. |
| `identity-federation-service` | Token issuance for Developer-Console-registered clients (continues to own user-side OAuth flows and JWKS). |
| `audit-compliance-service` | Sink for Developer Console, custom-endpoints, and MCP audit events. |

## Acceptance criteria

A milestone is considered accepted when:

1. Every `P0` item in the milestone is `done` with a backing test (unit, integration, or end-to-end as appropriate) and a runbook entry in `infra/runbooks/`.
2. The Developer Console UI in `apps/web` can drive the surface end-to-end without manual API calls for `P0` items.
3. The custom-endpoints gateway exposes `/healthz`, `/metrics`, and per-route trace IDs that match the Functions runtime trace IDs.
4. The Ontology MCP server passes an MCP conformance harness exercising at least the `initialize`, `tools/list`, `tools/call`, and `resources/list` requests against the OpenFoundry tool catalog.
5. The OpenAPI export for any non-trivial application validates against a public OpenAPI 3.1 linter without warnings other than well-known stylistic ones.
6. A parity matrix entry for `developer-console`, `custom-endpoints`, `ontology-mcp`, `palantir-mcp`, `foundry-ts`, and `cross-app-interactivity` is added to `foundry-feature-parity-matrix.json`.

## Test plan expectations

- **Unit tests** alongside each Go package: OAuth2 client validation (redirect URI exact-match, PKCE enforcement, scope subset), schema validation in the custom-endpoints gateway, rate-limiter behavior, OpenAPI document generation, MCP tool catalog filtering by granted scopes.
- **Integration tests** (`-tags=integration`, `libs/testing` helpers): full authorization-code-with-PKCE flow against `developer-console-service` + `identity-federation-service`, custom-endpoint invocation that exercises the gateway -> Functions runtime path, Ontology MCP server `initialize` + `tools/call` against a seeded ontology, credential rotation overlap window.
- **End-to-end tests** in `apps/web`: create application, register confidential client, select scopes, create custom endpoint bound to a sample Function, invoke endpoint with an issued token, view the per-route metrics; configure the Ontology MCP server allow-list and connect with a sample MCP client.
- **Conformance harness** for MCP: assert that the Ontology and Palantir MCP servers respond correctly to the MCP specification's required messages and degrade gracefully when capabilities are absent.
- **Security tests**: redirect URI exact-match rejection, public client rejecting `client_credentials`, secret rotation overlap window expiry, scope downgrade after revocation, public-endpoint anonymous rate-limit enforcement, MCP tool call denied when scope missing.
- **Load tests** for the custom-endpoints gateway: sustained `P0` baseline RPS per route with p95 latency under a documented budget and correct `429` behavior past quota.
