# iceberg-catalog-service

## LLM context

Implements Iceberg REST/catalog management, namespace/table admin surfaces, tokens, append, config, diagnose, and transaction commit flows.

Agent note: ties Lakekeeper/catalog state to OpenFoundry auth/audit and pipeline-build clients.

## Entrypoints

- `cmd/iceberg-catalog-service/main.go` builds the `iceberg-catalog-service` binary.

## Current HTTP / runtime surface

- `/iceberg/v1/config`
- `/iceberg/v1/transactions/commit`
- `/iceberg/v1/diagnose`
- `/api/v1/namespaces*`
- `/api/v1/iceberg-tables*`
- `/api-tokens*`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `2` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `audit`, `authz`, `config`, `domain`, `handlers`, `metrics`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `BUILD_GIT_SHA`, `DATABASE_URL`, `HOST`, `ICEBERG_CATALOG_WAREHOUSE_URI`, `ICEBERG_DEFAULT_TENANT`, `ICEBERG_DEFAULT_TOKEN_TTL_SECS`, `ICEBERG_JWT_AUDIENCE`, `ICEBERG_JWT_ISSUER`
- `ICEBERG_LONG_LIVED_TOKEN_TTL_SECS`, `JWT_SECRET`, `LAKEKEEPER_URL`, `METRICS_ADDR`, `OAUTH_INTEGRATION_URL`, `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/iceberg-catalog-service ./services/iceberg-catalog-service/cmd/iceberg-catalog-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
