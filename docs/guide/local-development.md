# Local Development

This page describes the fastest reliable paths for working in the current OpenFoundry monorepo. The active backend/tooling stack is Go; older Rust/Cargo references in migration material describe historical parity work rather than the runnable source tree.

## Required Tooling

- Go `1.25` or newer, matching the root `go.mod` directive.
- Node.js `20+`.
- pnpm `9+`.
- Docker and Docker Compose for integration tests and local infrastructure.
- `make` for the primary backend command surface.
- `just` for broad repo workflows such as frontend, docs, infra, smoke, and benchmark recipes.

Optional but useful for specialized flows:

- Buf for protobuf linting and generation.
- SQLC for type-safe database code generation.
- golangci-lint and gofumpt for local lint/format parity with Makefile targets.
- Helm for chart validation.
- Terraform for module validation.

You can install the pinned Go dev tools into `./bin` with:

```bash
make tools
```

## Backend Iteration

Build every Go package:

```bash
make build
```

Build every service command into `./bin`:

```bash
make build-services
```

Run unit tests with race detection and coverage:

```bash
make test
```

Run integration-tagged Go tests. This path expects Docker because many tests use testcontainers:

```bash
make test-integration
```

Run lint, format, tidy, vet, or the composite local CI gate:

```bash
make lint
make fmt
make tidy
make vet
make ci
```

## Code Generation

Regenerate protobuf and SQLC outputs together:

```bash
make gen
```

Or run the generators independently:

```bash
make gen-proto
make gen-sqlc
```

## Full Local Stack

Use this when you need infra, backend services, and the web app together:

```bash
just dev-stack
```

There is also a faster path when dependencies are already running and binaries/assets are already prepared:

```bash
just dev-stack-fast
```

## Infrastructure Only

Use this when you only need backing services such as Postgres, Redis, NATS, MinIO, or search infrastructure:

```bash
just infra-up
just infra-down
```

The root `compose.yaml` is the local entrypoint, while supporting Compose assets live under `infra/compose`.

## Frontend Iteration

The root Node scripts proxy into `apps/web`:

```bash
pnpm dev
pnpm lint
pnpm test:unit
pnpm build
pnpm check
```

If you prefer to work directly in the app package:

```bash
pnpm --dir apps/web dev
pnpm --dir apps/web check
pnpm --dir apps/web test:unit
```

The frontend is React + Vite. Its source is under `apps/web/src`, with generated public contract artifacts under `apps/web/public/generated`.

## Docs Iteration

The docs site is isolated under `docs/` and powered by VitePress:

```bash
just docs-install
just docs-dev
just docs-build
```

If you prefer to work directly inside the docs package:

```bash
cd docs
npm ci
npm run docs:dev
npm run docs:build
```

## Operational Assumptions

The repo is designed around service isolation rather than a single shared application package. Local and integration flows combine service-owned code with shared libraries, generated contracts, and infrastructure dependencies.

Common supporting infrastructure includes:

- Postgres-compatible databases for service persistence.
- Redis/Valkey for caching and stateful coordination.
- NATS and Kafka-compatible paths for control-plane and data-plane messaging.
- MinIO or another object-store-compatible backend.
- Search/vector infrastructure through the shared search and vector abstractions.
- Cassandra where Cassandra-backed kernel tests or service flows are under test.

## Helpful Commands

| Goal | Command |
| --- | --- |
| Install pinned Go dev tools | `make tools` |
| Generate protobuf + SQLC code | `make gen` |
| Build all Go packages | `make build` |
| Build service binaries | `make build-services` |
| Run Go tests | `make test` |
| Run integration-tagged Go tests | `make test-integration` |
| Run Go lint | `make lint` |
| Format Go code | `make fmt` |
| Run Go local CI gate | `make ci` |
| Validate proto contracts | `just proto-lint` |
| Validate OpenAPI drift | `just openapi-check` |
| Validate TypeScript SDK drift | `just sdk-typescript-check` |
| Export Terraform schema | `just terraform-schema` |
| Run smoke suite | `just smoke` |
| Run benchmark suite | `just bench-critical-paths` |
| Run frontend CI locally | `just ci-frontend` |
| Build docs | `just docs-build` |
