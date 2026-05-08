# Local Development

This page describes the fastest reliable paths for working in the current OpenFoundry monorepo. The active backend/tooling stack is Go; older Rust/Cargo references in migration material describe historical parity work rather than the runnable source tree.

## Required Tooling

- Go `1.25` or newer, matching the root `go.mod` directive.
- Node.js `20+`.
- pnpm `9+`.
- Docker and Docker Compose for integration tests and local infrastructure.
- `make` for the primary backend command surface.
- Docker Compose v2 for local infrastructure.

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

Use Docker Compose profiles when you need backing services plus a bounded set of containerized application services. The current Compose source of truth is `infra/compose/docker-compose.yml`, with optional local overrides in `infra/compose/docker-compose.dev.yml`:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  --profile foundation \
  up -d
```

Use `--profile edge` when you need the cumulative stack including gateway, web, and the nginx app facade. See the dev-stack page for the full profile map.

For host-run manual verification, `infra/scripts/dev-stack.sh` still exists and handles port selection, `.openfoundry/dev-stack.env`, Compose infrastructure, selected service binaries, and the web app. Treat that script as a convenience wrapper rather than the authoritative command surface.

## Infrastructure Only

Use this when you only need backing services such as Postgres, Valkey, NATS, MinIO, Cassandra, Kafka, OpenSearch, Vespa, Debezium, or Apicurio:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  up -d

docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  down
```

The root `compose.yaml` still points at the legacy `infra/docker-compose.yml` location, so prefer explicit `-f infra/compose/...` flags until that entrypoint is corrected.

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
cd docs
npm ci
npm run docs:dev
npm run docs:build
```

## Operational Assumptions

The repo is designed around service isolation rather than a single shared application package. Local and integration flows combine service-owned code with shared libraries, generated contracts, and infrastructure dependencies.

Common supporting infrastructure includes:

- Postgres-compatible databases for service persistence.
- Valkey for caching and stateful coordination.
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
| Validate proto contracts | `buf lint proto` |
| Run smoke wrapper | `./infra/scripts/smoke.sh` |
| Run frontend CI locally | `pnpm lint && pnpm check && pnpm test:unit && pnpm build` |
| Build docs | `cd docs && npm ci && npm run docs:build` |
