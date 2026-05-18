# solution-design-service

## LLM context

Owns solution-design records and APIs.

Agent note: compact JWT-protected CRUD service under /api/v1/solution-design.

## Entrypoints

- `cmd/solution-design-service/main.go` builds the `solution-design-service` binary.

## Current HTTP / runtime surface

- `/api/v1/solution-design*`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/solution-design-service ./services/solution-design-service/cmd/solution-design-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
