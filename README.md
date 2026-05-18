<div align="center">
  <a href="https://github.com/openfoundry/openfoundry-go">
    <img src="images/logo.png" alt="OpenFoundry" width="420" />
  </a>


  **The Open-Source Data Operating System**

  An open, cloud-native operational data platform for building data products with datasets, ontologies, applications, AI/ML, governance, and observability from one monorepo.

  <p align="center">
    <a href="https://github.com/openfoundry/openfoundry-go/actions/workflows/openfoundry-go.yml"><img src="https://img.shields.io/github/actions/workflow/status/openfoundry/openfoundry-go/openfoundry-go.yml?branch=main&style=for-the-badge&label=Go%20CI" alt="Go CI" /></a>
    <a href="https://github.com/openfoundry/openfoundry-go/actions/workflows/ci-frontend.yml"><img src="https://img.shields.io/github/actions/workflow/status/openfoundry/openfoundry-go/ci-frontend.yml?branch=main&style=for-the-badge&label=Frontend%20CI" alt="Frontend CI" /></a>
    <a href="https://github.com/openfoundry/openfoundry-go/actions/workflows/proto-check.yml"><img src="https://img.shields.io/github/actions/workflow/status/openfoundry/openfoundry-go/proto-check.yml?branch=main&style=for-the-badge&label=Proto%20Check" alt="Proto Check" /></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-AGPL--3.0--only-blue.svg?style=for-the-badge" alt="AGPL-3.0-only license" /></a>
  </p>

  [Documentation](docs/) · [Architecture](ARCHITECTURE.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Issues](https://github.com/openfoundry/openfoundry-go/issues)
</div>

---

OpenFoundry is an open-source operational data platform inspired by the capability model of Palantir Foundry, implemented as auditable, extensible software. It combines **50 service directories**, **36 shared libraries**, Protobuf/OpenAPI contracts, generated SDKs, a **React 19 + Vite + TypeScript** web console, and declarative infrastructure for Kubernetes.

The goal is to provide a reproducible foundation for teams that need to connect sources, version datasets, model an ontology, expose APIs, automate workflows, govern access, and operate analytical or AI workloads with end-to-end traceability.

> **Working with this codebase as an AI agent?** Start at [`CLAUDE.md`](CLAUDE.md). It is the canonical onboarding guide for commands, conventions, security-critical zones, and what not to read by default.

## Features & Status

- **Cloud-native architecture:** small Go services, one entrypoint per service, and delivery through Helm, ArgoCD, and Terraform.
- **Ontology at the core:** object types, actions, functions, object views, lineage, and stable contracts for building applications on operational data.
- **Contracts first:** Protobuf as the source of truth, generated OpenAPI, and synchronized TypeScript, Python, and Java SDKs.
- **Integrated governance:** authentication, authorization, Cedar policies, audit, tenancy, SSO/MFA, and egress controls.
- **Observability by default:** `/healthz`, `/metrics`, Prometheus, Grafana, Mimir, structured logs, and OTel traces.
- **Developer platform:** CLI tooling, SDK generation, service templates, VitePress docs, and unit/integration test paths.

| Capability | Status | Capability | Status |
| :-- | :-- | :-- | :-- |
| **Datasets & versioning** | ✅ Available | **Ontology services** | ✅ Available |
| **React web console** | ✅ Available | **Generated SDKs** | ✅ Available |
| **Protobuf/OpenAPI contracts** | ✅ Available | **AuthN/AuthZ foundations** | ✅ Available |
| **Observability stack** | ✅ Available | **Helm/ArgoCD delivery** | ✅ Available |
| **Kafka/NATS integrations** | ✅ Available | **Lakehouse/Iceberg paths** | Under active development |
| **AI/agent runtime services** | Under active development | **Production hardening** | In progress |

## OpenFoundry vs closed data platforms

| Area | OpenFoundry | Closed platforms |
| :-- | :-- | :-- |
| **Control** | Auditable code, contracts, and infrastructure in one monorepo. | Strong provider dependency and less implementation visibility. |
| **Extensibility** | Services, libraries, SDKs, and docs can evolve with your needs. | Extensions are limited by external APIs and vendor roadmaps. |
| **Deployment** | Kubernetes, Helm, ArgoCD, Terraform, and Compose for reproducible environments. | Usually SaaS or managed deployments with less operational control. |
| **Governance** | Policies, audit, and tenancy live beside the platform code. | Governance is coupled to the product and its commercial boundaries. |
| **Developer workflow** | Standard Go, TypeScript, Python, Java, Protobuf, and Makefile workflows. | Proprietary tooling or local workflows that are harder to automate. |

## Quickstart

### 1. Clone the repository

```sh
git clone https://github.com/openfoundry/openfoundry-go.git
cd openfoundry-go
```

### 2. Install development tools

```sh
make tools
```

This installs the Go tools used by the monorepo into `./bin`, including `buf`, `golangci-lint`, `sqlc`, and `gofumpt`.

### 3. Run the main local gate

```sh
make ci
```

`make ci` runs tidy, vet, lint, contract checks, and unit tests. For faster iteration, use:

```sh
make test
make build
make contracts-check
```

### 4. Start the frontend

```sh
pnpm install
pnpm --filter @open-foundry/web dev
```

The web application lives in [`apps/web/`](apps/web/) and uses React 19, Vite, and TypeScript.

### 5. Start local dependencies with Compose

```sh
docker compose -f infra/compose/docker-compose.yml up -d
```

For development, there is also:

```sh
docker compose -f infra/compose/docker-compose.dev.yml up -d
```

### 6. Deploy to Kubernetes

```sh
make gitops-bootstrap
make gitops-status
```

Delivery assets live in [`infra/`](infra/): Helm charts, ArgoCD apps, Terraform, Compose, and operational runbooks.

## Repository layout

```text
openfoundry-go/
├── apps/web/         React 19 + Vite + TypeScript frontend
├── services/         Go microservices; copy docs/templates/service-skeleton/ for new services
├── libs/             Shared Go packages for auth, observability, kernels and more
├── proto/            Protobuf source of truth; Go generated into libs/proto-gen/
├── sdks/             Generated TypeScript, Python and Java SDKs
├── infra/            Helm, ArgoCD, Terraform, Compose and operational runbooks
├── docs/             VitePress capability-oriented documentation site
├── tools/            CLIs and lint/helper tools
├── images/           Project branding assets, including this README logo
├── go.mod            Single Go module for the entire monorepo
└── Makefile          Canonical local task runner
```

Per-service shape:

```text
services/<svc>/
  cmd/<svc>/main.go          entrypoint
  internal/server/           chi router (/healthz, /metrics, /api)
  internal/handlers/         HTTP handlers
  internal/domain/           pure logic
  internal/repo/             data access, sqlc-generated when relevant
  internal/repo/migrations/  goose-style SQL migrations
  internal/models/           wire types
  internal/config/           koanf-backed config
```

## Why a single Go module?

OpenFoundry uses a single Go module (`go.mod` at the root) instead of a multi-module `go.work` setup because it:

- Keeps `libs/` and `services/` synchronized without version drift.
- Simplifies dependency resolution, caching, and builds.
- Makes contract generation and compatibility tests more direct.
- Follows a familiar pattern for large infrastructure monorepos.

Splitting specific services into their own modules remains possible if the project needs it.

## Day-to-day commands

Run these commands from the repository root:

```sh
make help              # list available targets
make tools             # install tools into ./bin
make ci                # tidy + vet + lint + contracts-check + test
make test              # unit tests with race detector and coverage
make test-integration  # tests with the integration build tag; requires Docker
make gen               # regenerate proto Go, sqlc, OpenAPI, and SDKs
make contracts-check   # verify OpenAPI and SDK drift
make build             # compile all packages
make build-services    # compile one binary per service into ./bin/
make lint              # golangci-lint
make fmt               # gofumpt + gci
```

Frontend:

```sh
pnpm --filter @open-foundry/web dev
pnpm --filter @open-foundry/web check
pnpm --filter @open-foundry/web test
```

## Documentation

- [`docs/`](docs/) — capability-oriented technical documentation.
- [`docs/index.md`](docs/index.md) — VitePress site entrypoint.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — high-level architecture overview.
- [`docs/architecture/adr/`](docs/architecture/adr/) — dated architectural decisions.
- [`CLAUDE.md`](CLAUDE.md) — concise onboarding for AI agents.
- [`CONTRIBUTING.md`](CONTRIBUTING.md) — PR process, RFC requirements, and DCO policy.
- [`SECURITY.md`](SECURITY.md) — how to report vulnerabilities.

## Wire-compatibility invariants

Some contracts are pinned by golden tests and must not change without an explicit migration:

- `/healthz` payload shape (`status`, `service`, `version`, `timestamp`).
- JWT claim names and JSON tags.
- Resource RID format and minting: `ri.<service>.<instance>.<type>.<uuid>` for platform-minted resources; `libs/core-models/rid` owns parsing, UUIDv7 minting, and registry-reservation collision handling.
- Resource type registry: `libs/core-models/resource` is the canonical registry for display names, owning services, icons, actions, RID namespace mapping, open-app URLs, and unknown-type placeholders.
- Compass project resource: `tenancy-organizations-service` owns the stable project `rid`, parent `space_rid`, organization/marking RIDs, default queue RID, resource-level grant toggle, and per-role policy payload.
- Compass folder resource: `tenancy-organizations-service` owns stable folder `rid` values, project/parent/space RID projection, folder trash status, and inheritance from project policies with folder-scope grant overrides.
- Compass move/rename: workspace operations may update project/folder parentage, display names, slugs, and derived breadcrumbs, but must not mutate resource RIDs; cross-project folder moves require explicit access-policy and marking confirmations.
- Compass search index: `tenancy-organizations-service` maintains `compass_resource_search_index` entries for project/folder RIDs and long-text catalog sources such as descriptions, READMEs, ontology object/property descriptions, code repository READMEs, and dashboard descriptions. Writes emit `compass.resource.search.updated.v1` outbox events on create/update/move/trash/restore/purge instead of relying on table polling.
- Compass search API: `GET /api/v1/compass/search` is permission-aware, supports `q`, `type`, `project`, `owner`, repeated `marking`, `modified`, `limit`, and `cursor`, returns highlighted snippets and facets for type/project/owner/marking/last-modified buckets, and paginates by opaque cursor over text score, last modified time, and RID.
- Compass search UI shell: `apps/web` route `/search` combines ontology search with `GET /api/v1/compass/search`, keeps the `Cmd/Ctrl+J` global search shell, loads jump-to recents/favorites, displays marking badges and snippet highlights, renders resource facets in the sidebar, and derives resource "Open with" actions from the frontend Compass resource type registry.
- Compass saved searches: `compass_saved_searches` stores per-user named Quicksearch/Data Catalog queries with tab, type, project, owner, marking, and last-modified filters; `/api/v1/workspace/saved-searches` lists/creates/deletes them for the search sidebar.
- Compass recommendations: `GET /api/v1/workspace/recommendations` returns permission-filtered "you might want to open" resources scored from collaborator opens, the caller's recent opens, and explicit project follows stored in `compass_project_follows`; Quicksearch jump-to mode renders them beside favorites and recents.
- Compass open-with menu: `apps/web/src/lib/components/workspace/OpenWithMenu.tsx` is the shared registry-backed launcher for search results, project/folder list views, and resource detail headers; targets resolve from immutable RIDs when present and retain an unknown-resource fallback.
- Compass breadcrumbs: `apps/web` uses the shared `ProjectBreadcrumb` for project/folder paths, click-to-open navigation, and per-crumb copy-RID actions derived from stable project/folder RIDs.
- Compass trash workflow: project/folder/resource-binding deletes are soft deletes with configurable `retention_days` (default 30), `purge_after` metadata, search-index trash/restore events, and folder restore fallback to the project root when the original parent path is gone.
- Compass hard delete audit: permanent deletes are allowed after `purge_after` or by admin override, clean directly affected Compass surface rows, and emit marking-aware `compass.resource.purged` audit events with the dependent rows/resources affected by the purge.
- Compass resource audit: create, move, rename, trash, restore, purge, share grant/update/revoke, and marking-change mutations emit marking-aware `compass.resource.*` events to `audit.events.v1`; `services/audit-sink` exposes them through `GET /api/v1/audit/events` and NDJSON export.
- Compass bulk operations: `POST /api/v1/workspace/resources/batch` applies selected search/folder rows as one preflighted move/trash/share batch, aborts before mutation when any row fails policy/confirmation checks, and emits one `compass.resource.bulk_operation` audit event for the batch instead of per-row audit noise.
- Compass reverse-reference graph: `compass_resource_references` stores directed `source depends on target` edges, the resource registry declares supported reference targets, and workspace APIs/UI expose `depends_on` / `used_by` with move/trash warnings.
- Compass stable resource URLs: web routes and search/open-with URLs identify resources by RID, with optional slug suffixes treated only as visual sugar so rename and move operations do not invalidate links.
- Compass favorites: `user_favorites` is the synced per-user profile store for resource shortcuts; favorites have optional groups, stable display order, and a sidebar/search management UI backed by `/api/v1/workspace/favorites`.
- Compass recents: `resource_access_log` records per-user opens, while `/api/v1/workspace/recents` returns at most 50 visible resources by default, ordered by last-opened and filtered against current project visibility so revoked resources disappear.
- Compass propagate view requirements: `tenancy-organizations-service` keeps Palantir's planned-deprecated setting as a legacy compatibility toggle. Project/folder rows record whether propagation is enabled plus the permanent `disabled_at` marker; new folders and project resource bindings copy the inherited view-requirement marking snapshot on create, and parent policy changes enqueue `compass_view_requirement_propagation_jobs` to update existing descendants with progress reporting and `compass.view_requirements.propagated` audit events.
- Dataset RID format: `ri.foundry.main.dataset.<uuid-v7>`.
- Transaction state/type tokens: `open|committed|aborted` and `snapshot|append|update|delete`.
- Marking source and schema field type discriminators.
- Media reference camelCase keys (`mediaSetRid`, `mediaItemRid`, `branch`, `schema`).

## Getting help

- Open a bug report or feature request in [GitHub Issues](https://github.com/openfoundry/openfoundry-go/issues).
- Review the documentation in [`docs/`](docs/) before changing services or contracts.
- For new capabilities, start from the [`service skeleton`](docs/templates/service-skeleton/) and the existing ADRs.

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the PR process, RFC requirements, and DCO policy. Security reports follow [`SECURITY.md`](SECURITY.md).

## License

OpenFoundry is licensed under **AGPL-3.0-only**. See [`LICENSE`](LICENSE).
