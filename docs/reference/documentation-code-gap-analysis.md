# Documentation vs Code Gap Analysis

This page is a code-first audit of technical documentation drift. The repository rule is: **documentation follows the code; code is never changed just to satisfy stale documentation**.

## Audit Snapshot

Snapshot date: 2026-05-18. The values below come from the working tree, not from roadmap text or archived migration plans.

| Evidence command / source | Current code value | Documentation impact |
| --- | ---: | --- |
| `find services -mindepth 1 -maxdepth 1 -type d` | 50 service directories | Pages must not claim the current monorepo has 42 service binaries. If they mention “42,” they are historical or stale. |
| `find libs -mindepth 1 -maxdepth 1 -type d` | 36 library directories | Pages must not claim the current monorepo has 33 shared libraries. |
| `find proto -mindepth 1 -maxdepth 1 -type d` | 23 protobuf domains | Contract docs should describe these proto domains as the current source of truth. |
| `services/edge-gateway-service/internal/proxy/router_table.go` | Gateway route ownership is prefix-based and alias-driven | Public-route docs must follow the router table, not inferred service names. |
| `services/edge-gateway-service/internal/config/config.go` + `services/edge-gateway-service/config.yaml` + service-local configs | Several gateway aliases intentionally point at consolidated owners instead of same-named placeholder services | Port and route docs must distinguish “binary exists” from “gateway default routes traffic there.” |
| Root `justfile` | `just` is a shim over Makefile targets; no `infra-up`, `dev-stack`, `docs-build`, `smoke`, or `ci-frontend` recipes are defined | Contributor docs must prefer active `make`, `pnpm`, and `docker compose` commands. |

## High-Priority Gaps Found

### 1. Service and library inventory drift

The current filesystem inventory is 50 service directories and 36 shared-library directories. Older repository summaries still claimed 42 Go microservices and 33 shared libraries. The root README has now been corrected to the current counts, and `docs/reference/repository-layout.md` already contains the current detailed inventory.

Services that were historically missing or incorrectly described in older docs include:

- `action-log-sink`
- `cipher-service`
- `function-runtime-service`
- `global-branch-service`
- `iceberg-object-indexer`
- `knowledge-index-service`
- `network-boundary-service`
- `report-service`

Important nuance: several of these are real service directories but still skeletons, placeholders, or route-alias targets in flux. Documentation should state that explicitly instead of omitting them.

Shared libraries that were missing from older “33 libraries” summaries include:

- `pipeline-plan`
- `pipeline-runtime`
- `restrictedview`

### 2. Port tables and service-local READMEs can drift independently

`docs/architecture/services-and-ports.md` now reflects the current service map for the ports that were previously stale. One service-local README was still stale during this audit: `services/authorization-policy-service/README.md` documented `PORT=50115`, while code defaults to `50093`. The README has now been corrected.

Known port/value examples to verify whenever these pages are edited:

| Service | Stale doc value seen in older docs | Current code value / nuance |
| --- | --- | --- |
| `authorization-policy-service` | `50115` | `50093` in `internal/config/config.go` |
| `ingestion-replication-service` | `50120` | `50090` |
| `dataset-versioning-service` | `50117` | `50078` |
| `media-sets-service` | `50121` / `50122` | `50156` / `50157` |
| `ontology-definition-service` | `50122` | `50103` |
| `application-composition-service` | `50118` | `50140` |
| `federation-product-exchange-service` | `50126` | `50120` |
| `iceberg-catalog-service` | `8197` only | Service config defaults to `50118`; gateway `config.yaml` may still point to `8197` |

### 3. Gateway alias docs must not equate a service directory with the live route target

The edge gateway keeps legacy upstream fields for Helm/strangler compatibility, but code defaults often point those fields at surviving consolidated owners. Documentation should always present two separate facts:

1. the filesystem/binary inventory under `services/`; and
2. the gateway's current default route target.

Examples that need this distinction:

- `security_governance_service_url`, `cipher_service_url`, `network_boundary_service_url`, and `checkpoints_purpose_service_url` may be aliases for authorization/compliance surfaces rather than proof that every same-named placeholder is the live route target.
- `data_asset_catalog_service_url` and `dataset_quality_service_url` default to consolidated dataset surfaces.
- `application_curation_service_url` and app-builder routes default to `application-composition-service`.
- `report_service_url` can default to `notebook-runtime-service`, even though `report-service` exists as a placeholder binary.
- `global_branch_service_url` can default to `code-repository-review-service`, even though `global-branch-service` exists as a skeleton binary.

### 4. Route ownership had at least one stale ontology assumption

Older docs said `/api/v1/ontology/types/{id}/objects/query` belonged to `ontology-query-service`. The router table sends broad object-instance paths, including object query paths under `/api/v1/ontology/types/*/objects`, to `object-database-service`; KNN remains on `ontology-query-service`. Any page that lists route ownership should be checked against `services/edge-gateway-service/internal/proxy/router_table.go`.

### 5. Local-development command docs still need code-first checks

`docs/operations/deployment-modes.md` still named `just infra-up` and `just dev-stack` during this audit. The root `justfile` does not define those recipes; it delegates only to active Makefile targets. That page has now been corrected to use Docker Compose commands from the active `infra/compose/` paths.

When auditing command docs, treat `docs/developer-toolchain/local-workflows.md`, the root `justfile`, `Makefile`, and package manifests as higher authority than older runbooks or archive pages.

### 6. Historical service names remain in non-archive docs and should be reviewed semantically

A raw text scan still finds historical service names in some non-archive documentation. These are not automatically wrong: some are compatibility aliases or explanatory notes. They should be reviewed case-by-case to ensure they do not imply a current binary exists when it does not.

Examples to review next:

- `docs/ontology-building/object-edits-and-conflict-resolution.md` references `audit-service`.
- `docs/ontology-building/define-ontologies.md` references `audit-service` alongside current services.
- Several code comments intentionally retain historical terms such as `pipeline-service`, `dataset-service`, `ai-service`, or `streaming-service`; those should not be rewritten unless the surrounding runtime behavior has changed.

## Code-First Documentation Policy

When updating docs, use this precedence order:

1. **Router ownership:** `services/edge-gateway-service/internal/proxy/router_table.go`.
2. **Gateway defaults:** `services/edge-gateway-service/internal/config/config.go` and `services/edge-gateway-service/config.yaml`.
3. **Service default ports:** `services/<service>/internal/config/config.go`; for koanf-based skeleton services, `services/<service>/config.yaml`.
4. **Service role and readiness:** service-local `README.md`, `NOT_IMPLEMENTED_AUDIT.md`, and tests.
5. **Repository inventory:** `find services`, `find libs`, `find proto`, and checked-in package manifests.
6. **Roadmaps/migration checklists/archive pages:** only after the current code has been verified.

## Follow-Up Work Recommended

- Keep `make docs-drift-check` in the local/CI gate; it fails when repository-layout counts disagree with `services/`, `libs/`, or `proto/`, when `services-and-ports.md` route examples disagree with `router_table.go`, or when documented ports disagree with service/gateway config defaults.
- Generate the service inventory table from service-local metadata where possible.
- Review non-archive pages that still mention retired service names and mark each mention as either a compatibility alias or stale text to remove.
- Review migration checklists that state “green” test commands from old commits; those should be dated or converted into current verification instructions.
