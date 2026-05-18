# ontology-query-service

## LLM context

Owns ontology query/read APIs over Cassandra-backed ontology/object indexes.

Agent note: JWT-protected /api/v1/ontology query service; not the schema-definition owner.

## Entrypoints

- `cmd/ontology-query-service/main.go` builds the `ontology-query-service` binary.

## Current HTTP / runtime surface

- `/api/v1/ontology* query routes`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- No SQL migration files live under this service directory.
- Main internal packages: `config`, `handlers`, `models`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `CASSANDRA_ADDR`, `CASSANDRA_CONTACT_POINTS`, `CASSANDRA_KEYSPACE`, `CASSANDRA_PASSWORD`, `CASSANDRA_USERNAME`, `HOST`, `JWT_SECRET`, `METRICS_ADDR`
- `NATS_URL`, `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/ontology-query-service ./services/ontology-query-service/cmd/ontology-query-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
