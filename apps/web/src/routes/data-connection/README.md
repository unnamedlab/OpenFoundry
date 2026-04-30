# Data Connection — frontend MVP

Frontend slice for the Data Connection application, modelled after the Foundry
"Data Connection" product. This is the **v0 MVP**: a vertical slice that
validates the architecture (sources → egress policies → batch syncs) without
trying to cover every connector or capability.

## Routes

| Route                                    | Purpose                                                                                          |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `/data-connection`                       | Landing page with the user's sources (table + `+ New source` CTA).                                |
| `/data-connection/new`                   | Connector gallery. S3, REST API and PostgreSQL are real; the rest are tagged "Coming soon".       |
| `/data-connection/sources/[id]`          | Source detail with five tabs: Overview / Networking / Credentials / Capabilities / Runs.          |
| `/data-connection/egress-policies`       | List of network egress policies + 2-step creation wizard (definition + permissions).              |
| `/data-connection/agents`                | Placeholder for the legacy agent worker. Download is intentionally disabled (out of MVP scope).   |

The **Networking** tab on the source detail reproduces the empty state from
the design screenshot (`bf55df40`): a centered "Select a policy" prompt with
"Use existing policy" / "Create new policy" CTAs when no policy is attached,
and a list view once policies are bound.

## API surface

All requests go through `$lib/api/data-connection.ts`, which exposes typed
helpers for every endpoint the MVP needs. The contract is:

```
GET    /api/v1/data-connection/catalog
GET    /api/v1/data-connection/sources
POST   /api/v1/data-connection/sources
GET    /api/v1/data-connection/sources/:id
PATCH  /api/v1/data-connection/sources/:id
DELETE /api/v1/data-connection/sources/:id
POST   /api/v1/data-connection/sources/:id/test-connection
GET    /api/v1/data-connection/sources/:id/credentials
POST   /api/v1/data-connection/sources/:id/credentials
GET    /api/v1/data-connection/sources/:id/egress-policies
POST   /api/v1/data-connection/sources/:id/egress-policies
DELETE /api/v1/data-connection/sources/:id/egress-policies/:policyId
GET    /api/v1/data-connection/egress-policies
POST   /api/v1/data-connection/egress-policies
DELETE /api/v1/data-connection/egress-policies/:id
GET    /api/v1/data-connection/sources/:id/syncs
POST   /api/v1/data-connection/syncs
POST   /api/v1/data-connection/syncs/:id/run
GET    /api/v1/data-connection/syncs/:id/runs
```

The catalog falls back to `FALLBACK_CONNECTOR_CATALOG` (defined in the same
module) when the backend is unreachable, so the gallery still renders during
backend bring-up. Pages render their empty states gracefully when other
endpoints fail.

## Backend wiring TODOs (out of this slice)

The frontend talks to handlers that already exist in
`services/connector-management-service`, `services/network-boundary-service`
and `services/ingestion-replication-service`. The remaining wiring is:

1. Replace the placeholder `fn main() {}` in those services with the real
   Axum router (the handler functions themselves are already implemented for
   `connections`/`boundary`).
2. Mount the routes under `/api/v1/data-connection/...` (likely behind the
   existing API gateway).
3. Add migrations for the MVP tables that don't yet exist:
   `network_egress_policies`, `source_policy_bindings`, `source_credentials`,
   `batch_sync_defs`, `sync_runs`. The `connections` table already covers the
   `Source` model.
4. Wire `POST /sources/:id/test-connection` to the existing
   `connectors::*::test_connection` functions.

## Out of MVP

* Agent worker download / registration (legacy per Foundry docs).
* HyperAuto, virtual tables, virtual media, CDC, streaming exports, webhooks,
  compute modules, external transforms.
* Connectors beyond S3 / REST API / PostgreSQL.
* Health monitoring & alerting integration.
* Fine-grained policy permissions (the wizard accepts opaque marking/group
  identifiers; resolution stays in `authorization-policy-service`).
* Playwright happy-path: requires a live backend; the existing unit tests
  cover the catalog helpers (`src/lib/api/data-connection.test.ts`).
