# ontology-definition-service

## LLM context

Owns declarative ontology schema definitions such as object types and related schema metadata.

Agent note: mounted on both legacy /api/v1/ontology-definition and canonical /api/v1/ontology prefixes.

## Entrypoints

- `cmd/ontology-definition-service/main.go` builds the `ontology-definition-service` binary.

## Current HTTP / runtime surface

- `/api/v1/ontology-definition*`
- `/api/v1/ontology* schema routes`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `3` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `METRICS_ADDR`, `NATS_URL`, `OPENFOUNDRY_JWT_SECRET`, `PG_SCHEMA`, `PORT`
- `SERVICE_VERSION`

## Build

```sh
go build -o bin/ontology-definition-service ./services/ontology-definition-service/cmd/ontology-definition-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
