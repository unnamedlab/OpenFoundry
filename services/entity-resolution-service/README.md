# entity-resolution-service

## LLM context

Owns fusion/entity-resolution APIs for matching, merging, and survivorship-style workflows.

Agent note: JWT-protected /api/v1/fusion service backed by Postgres.

## Entrypoints

- `cmd/entity-resolution-service/main.go` builds the `entity-resolution-service` binary.

## Current HTTP / runtime surface

- `/api/v1/fusion*`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `2` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/entity-resolution-service ./services/entity-resolution-service/cmd/entity-resolution-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
