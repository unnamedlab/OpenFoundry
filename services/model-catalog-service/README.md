# model-catalog-service

## LLM context

Owns model-catalog metadata and CRUD-style registry APIs.

Agent note: Postgres-backed /api/v1/model-catalog service.

## Entrypoints

- `cmd/model-catalog-service/main.go` builds the `model-catalog-service` binary.

## Current HTTP / runtime surface

- `/api/v1/model-catalog*`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `4` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/model-catalog-service ./services/model-catalog-service/cmd/model-catalog-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
