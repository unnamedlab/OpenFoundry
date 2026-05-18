# ontology-exploratory-analysis-service

## LLM context

Owns exploratory ontology views, maps, geospatial layers/tiles/query, and writeback proposal APIs.

Agent note: uses ontology-actions/query downstream URLs for integrated analysis flows.

## Entrypoints

- `cmd/ontology-exploratory-analysis-service/main.go` builds the `ontology-exploratory-analysis-service` binary.

## Current HTTP / runtime surface

- `/api/v1/views*`
- `/api/v1/maps*`
- `/api/v1/geospatial/*`
- `/api/v1/writeback*`
- `/api/v1/writeback-proposals*`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `4` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `ONTOLOGY_ACTIONS_SERVICE_URL`, `ONTOLOGY_QUERY_SERVICE_URL`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/ontology-exploratory-analysis-service ./services/ontology-exploratory-analysis-service/cmd/ontology-exploratory-analysis-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
