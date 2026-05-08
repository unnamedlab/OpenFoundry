# Repository Map

OpenFoundry is organized as a Go-first platform monorepo with clear directory-level ownership boundaries. The repository still carries migration and parity documentation from the Rust-era codebase, but the runnable source tree is now the single Go module rooted at `go.mod`.

## Top-Level Layout

| Path | Role |
| --- | --- |
| `apps/web` | React 19 + Vite frontend and product UI routes. Root `pnpm` scripts delegate here through the `@open-foundry/web` package. |
| `services/*` | Go microservices. Each service has one or more `cmd/<binary>/main.go` entrypoints plus service-local `internal/` packages. |
| `libs/*` | Shared Go packages for kernels, middleware, eventing, storage, observability, schedulers, SDK support, and test helpers. |
| `proto/*` | Canonical protobuf contracts grouped by domain, with Buf configuration under `proto/` and root generation wiring in `buf.gen.yaml`. |
| `tools/of-cli` | Go CLI for smoke execution, benchmarks, OpenAPI validation, SDK generation, and mock-provider support. |
| `infra/*` | Docker Compose assets, Helm charts, Terraform modules/provider schema, observability assets, test tools, scripts, and operational runbooks. |
| `sdks/*` | Generated SDK packages for TypeScript, Python, Java, and Python transform authoring. |
| `smoke/*` | Critical-path end-to-end scenarios used to validate real platform flows. |
| `benchmarks/*` | Reproducible benchmark scenarios, runbooks, and result outputs. |
| `python/*` | Python runtime package used by Python sidecar and authoring/runtime flows. |
| `docs/*` | VitePress technical documentation site. |
| `PoC/*` | Spanish proof-of-concept walkthrough and demo preparation material. |
| `Good-practices-architecture/*` | Architecture review checklists and supporting governance notes. |
| `.github/workflows/*` | CI, release, packaging, security, infra, integration, and docs automation. |

## Workspace Control Files

| File | Purpose |
| --- | --- |
| `go.mod` / `go.sum` | Single Go module for the whole backend/tooling monorepo: `github.com/openfoundry/openfoundry-go`. |
| `Makefile` | Primary Go command surface for tools, generation, build, test, lint, format, tidy, vet, and the composite CI gate. |
| `justfile` | Legacy helper recipes. Many backend recipes still reference Cargo/Rust, so prefer `make`, root `pnpm` scripts, direct `docker compose -f infra/compose/...`, and scripts under `infra/scripts`. |
| `package.json` / `pnpm-lock.yaml` / `pnpm-workspace.yaml` | Root Node workspace and scripts that delegate frontend commands to `apps/web`. |
| `buf.gen.yaml` | Root Buf generation pipeline that emits Go protobuf code into `libs/proto-gen`. |
| `proto/buf.yaml` / `proto/buf.lock` / `proto/buf.gen.yaml` | Protobuf lint, dependency, and domain-local generation inputs. |
| `sqlc.yaml` | SQLC generation configuration for type-safe database access. |
| `compose.yaml` | Legacy root Compose entrypoint that still points at `infra/docker-compose.yml`; use explicit `infra/compose/docker-compose.yml` and `infra/compose/docker-compose.dev.yml` flags until corrected. |
| `.env.example` | Example environment values for local development. |

## Current Code Shape

- The repo currently has 41 service directories under `services/` and 42 service command entrypoints because `workflow-automation-service` also ships the `approvals-timeout-sweep` command.
- The repo currently has 32 shared library directories under `libs/`.
- `go list ./...` discovers hundreds of packages across services, shared libraries, tooling, and a vendored-looking Go package inside `apps/web/node_modules`; when running Go-wide checks, use the Makefile targets so the command shape stays consistent with CI intent.
- There are no `Cargo.toml` files in the active source tree; docs that mention Cargo/Rust service crates are migration-era or parity references unless they explicitly describe legacy decisions.

## Delivery Surfaces

The repository produces more than one artifact:

- Go binaries from `services/*/cmd/*` and `tools/of-cli`.
- Frontend bundles from `apps/web`.
- Docker images from service-specific Dockerfiles and shared infra workflows.
- Generated protobuf Go packages under `libs/proto-gen`.
- Generated OpenAPI, SDK, and Terraform schema artifacts for UI, SDK, and provider consumers.
- Helm templates, Terraform modules, Compose profiles, and operational runbooks under `infra/`.
- GitHub Pages output from the VitePress docs site under `docs/`.

## Where To Look First

- If the change is product UI, routing, or frontend state, start in `apps/web/src`.
- If it is API or service behavior, start in the matching folder under `services/` and then inspect shared packages under `libs/` before duplicating logic.
- If it affects ontology, objects, actions, search, or function runtime behavior, inspect `libs/ontology-kernel` and the ontology service handlers together.
- If it affects identity or policy behavior, inspect `services/identity-federation-service`, `services/authorization-policy-service`, `libs/auth-middleware`, and `libs/authz-cedar-go`.
- If it changes public contract shape, inspect `proto/`, `libs/proto-gen`, generated OpenAPI, and SDK flows together.
- If it changes deployability, inspect `infra/`, explicit Compose files under `infra/compose`, service Dockerfiles, and the relevant workflow under `.github/workflows/`.
