# Monorepo Structure

The Go module is the primary organizational unit of OpenFoundry. It groups shared libraries, developer tooling, frontend code, generated contracts, infrastructure, and runtime services under one repository so platform contracts can evolve together.

## Top-Level Layout

| Path | Role |
| --- | --- |
| `apps/web` | React + Vite frontend for the platform UI. |
| `libs/` | Shared Go packages used across services. |
| `services/` | Runtime Go microservices and command entrypoints. |
| `tools/of-cli` | Internal Go CLI for generation, smoke, mock providers, and benchmarks. |
| `proto/` | Protobuf and code-generation inputs. |
| `sdks/` | Generated TypeScript, Python, Java, and transform SDK packages. |
| `python/` | Python runtime package used by sidecar/runtime flows. |
| `infra/` | Docker Compose assets, Helm charts, Terraform modules/provider schema, observability assets, scripts, and deployment overlays. |
| `smoke/` | Scenario definitions for end-to-end smoke validation. |
| `benchmarks/` | Benchmark scenarios, runbooks, and result outputs. |
| `docs/` | VitePress technical documentation site. |

## Workspace Composition

The active backend codebase is a single Go module named `github.com/openfoundry/openfoundry-go`. It currently contains:

- 41 service directories under `services/`.
- 42 service command entrypoints under `services/*/cmd/*/main.go`.
- 32 shared library directories under `libs/`.
- Generated protobuf packages under `libs/proto-gen`.
- Tooling under `tools/of-cli`.

This structure makes it possible to share:

- authentication, claims, and Cedar authorization helpers;
- storage, Cassandra, outbox, idempotency, and saga primitives;
- event bus, audit, scheduling, observability, and testing helpers;
- ontology, query, pipeline-expression, AI, ML, media, geospatial, search, and vector kernels;
- generated protobuf types and public contract code.

## Single-Module Decision

The repository intentionally uses one root `go.mod` rather than per-service modules or a `go.work` composition. That keeps services, shared libraries, and tooling on one dependency graph, avoids drift between `libs/` and `services/`, and lets root commands validate the whole source tree consistently.

## Frontend and Contract Placement

The frontend sits outside the Go package tree but inside the same monorepo so that it can directly consume:

- generated OpenAPI JSON from `apps/web/public/generated/openapi`;
- generated Terraform schema from `apps/web/public/generated/terraform`;
- generated TypeScript SDK outputs under `sdks/typescript`;
- protobuf and API contract changes from the same commit as backend changes.

That keeps UI, SDK, and contract surfaces versioned alongside the backend services that produce them.

## Service Ownership Model

Each Go service typically contains:

- `cmd/<service-name>/main.go` for process bootstrapping;
- `internal/config` for service-local configuration;
- `internal/server` or equivalent HTTP/gRPC startup wiring;
- `internal/handlers`, `internal/domain`, `internal/models`, `internal/repo`, `internal/runtime`, or other service-specific packages as needed;
- optional service-local `Dockerfile`, `README.md`, `config.yaml`, migrations, fixtures, and test files.

The codebase follows service ownership boundaries in code and deployment packaging. Shared behavior should move into `libs/` only when multiple services need the same primitive or contract.
