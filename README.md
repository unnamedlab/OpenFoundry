# OpenFoundry

OpenFoundry is an open-source data operating system implemented as a Go-first
monorepo. The active backend source tree is a single Go module
(`github.com/openfoundry/openfoundry-go`) with bounded-context services under
`services/`, shared packages under `libs/`, protobuf contracts under `proto/`, a
React/Vite frontend under `apps/web`, and documentation under `docs/`.

> **Documentation status (2026-05-08):** this README is the first refreshed
> entry point in the documentation cleanup. Historical migration and parity
> documents still exist in the repository for traceability, but the active code
> shape is the Go monorepo described below.

## Repository layout

```text
OpenFoundry/
├── apps/
│   └── web/                    # React 19 + Vite product frontend
├── services/                   # 41 Go microservice directories
│   ├── edge-gateway-service/
│   ├── identity-federation-service/
│   ├── authorization-policy-service/
│   ├── ontology-actions-service/
│   ├── pipeline-build-service/
│   ├── ai-sink/
│   ├── audit-sink/
│   └── template/               # reference scaffold for new services
├── libs/                       # 32 shared Go packages
│   ├── core-models/
│   ├── auth-middleware/
│   ├── authz-cedar-go/
│   ├── observability/
│   ├── ontology-kernel/
│   ├── query-engine/
│   ├── ai-kernel-go/
│   └── proto-gen/              # generated Go protobuf packages
├── proto/                      # canonical protobuf contracts and Buf config
├── sdks/                       # generated SDK packages and metadata
├── python/                     # Python runtime package for sidecar/runtime flows
├── infra/                      # Compose, Helm, Terraform, observability, ops assets
├── smoke/                      # scenario-driven smoke tests
├── benchmarks/                 # benchmark scenarios and results
├── docs/                       # VitePress technical documentation site
├── tools/                      # repository maintenance and audit utilities
├── go.mod                      # single Go module for backend/tooling
├── Makefile                    # primary Go build/test/generate/lint surface
├── justfile                    # legacy helper recipes; prefer make/pnpm/current scripts
├── package.json                # root Node workspace scripts
└── compose.yaml                # legacy root Compose shim; use infra/compose files directly
```

For a fuller map, see [`docs/guide/repository-map.md`](docs/guide/repository-map.md)
and [`docs/reference/repository-layout.md`](docs/reference/repository-layout.md).

## Main delivery surfaces

| Surface | Current source of truth |
| --- | --- |
| Backend services | `services/*/cmd/*` plus service-local `internal/` packages |
| Shared backend code | `libs/*` |
| Frontend | `apps/web` (`@open-foundry/web`) |
| API contracts | `proto/*`, generated Go under `libs/proto-gen`, generated OpenAPI under `apps/web/public/generated/openapi` |
| SDKs | `sdks/typescript`, `sdks/python`, `sdks/java` |
| Python runtime | `python/openfoundry_pyruntime` |
| Infrastructure | `infra/compose`, `infra/helm`, `infra/terraform`, `infra/observability`; `compose.yaml` is a legacy shim pending path correction |
| Technical docs | `docs/` VitePress site |
| Smoke and benchmark coverage | `smoke/` and `benchmarks/` |

## Single-module backend decision

The backend/tooling workspace intentionally uses one Go module rooted at
`go.mod` instead of per-service modules. This keeps service and library versions
in lockstep, reduces dependency drift, and matches the command surface exposed by
the root `Makefile`.

## Day-to-day commands

### Go backend and tooling

```sh
make tools             # install buf, golangci-lint, sqlc, gofumpt into ./bin
make gen               # run protobuf and sqlc generation
make build             # build all Go packages
make build-services    # build service binaries into ./bin
make test              # run Go tests with race detector and coverage
make test-integration  # run Go integration tests; requires Docker/testcontainers
make lint              # run golangci-lint
make ci                # run tidy, vet, lint, and test
```

### Frontend

```sh
pnpm install
pnpm dev       # delegates to apps/web
pnpm build     # type-checks and builds apps/web
pnpm lint      # runs frontend lint
pnpm test      # runs frontend unit tests
pnpm check     # runs TypeScript check
```

### Documentation site

```sh
pnpm --dir docs install
pnpm --dir docs docs:dev
pnpm --dir docs docs:build
```

## Current code shape

- `services/` currently contains 41 service directories. Most services expose a
  `cmd/<service-name>/main.go` binary; `workflow-automation-service` also ships
  an `approvals-timeout-sweep` command.
- `libs/` currently contains 32 shared library directories covering common
  models, auth, Cedar authorization helpers, observability, event buses,
  reliability primitives, domain kernels, storage/search/vector abstractions,
  AI/ML helpers, generated protobufs, and testing utilities.
- `apps/web` is the product frontend and is managed through the root pnpm
  workspace.
- `proto/` is the canonical protobuf contract tree. Root Buf generation emits Go
  packages into `libs/proto-gen`.
- Migration-era inventory documents remain useful for historical context, but
  they should not override the live source tree when they conflict with the
  directories above.

## Where to start

- Product UI, routing, or frontend state: `apps/web/src`.
- Service behavior: the matching `services/<name>` directory, then shared code
  under `libs/` before duplicating logic.
- Ontology, objects, actions, search, or function-runtime behavior:
  `libs/ontology-kernel` plus the ontology service handlers.
- Identity, tenancy, or policy behavior: `services/identity-federation-service`,
  `services/tenancy-organizations-service`,
  `services/authorization-policy-service`, `libs/auth-middleware`, and
  `libs/authz-cedar-go`.
- Public contract shape: `proto/`, `libs/proto-gen`, generated OpenAPI, and SDK
  generation flows together.
- Deployment or operations: `infra/`, explicit Compose files under `infra/compose`, service Dockerfiles, and
  `.github/workflows/*`.

## Documentation roadmap for this cleanup

Because the documentation set is large, update it in small reviewable slices:

1. Refresh repository entry points (`README.md`, `ARCHITECTURE.md`, repository
   maps, and getting-started pages).
2. Refresh service-family documentation by bounded context: platform/security,
   data engine, ontology, AI/ML, and apps/ops.
3. Refresh generated-contract and SDK documentation after service docs are
   aligned.
4. Mark historical migration-only material explicitly so contributors can tell it
   apart from current operational guidance.
