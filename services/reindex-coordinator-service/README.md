# reindex-coordinator-service

## LLM context

Coordinates reindex scans and emits work/events for rebuilding search/index projections.

Agent note: background coordinator with Postgres/Cassandra/Kafka configuration and no product REST API.

## Entrypoints

- `cmd/reindex-coordinator-service/main.go` builds the `reindex-coordinator-service` binary.

## Current HTTP / runtime surface

- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `event`, `repo`, `runtime`, `scan`, `server`, `state`, `topics`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `CASSANDRA_CONTACT_POINTS`, `CASSANDRA_KEYSPACE`, `DATABASE_MAX_CONNECTIONS`, `DATABASE_URL`, `HOST`, `KAFKA_BOOTSTRAP_SERVERS`, `KAFKA_CLIENT_ID`, `KAFKA_SASL_MECHANISM`
- `KAFKA_SASL_PASSWORD`, `KAFKA_SASL_USERNAME`, `KAFKA_SECURITY_PROTOCOL`, `METRICS_ADDR`, `OF_OPENLINEAGE_NAMESPACE`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/reindex-coordinator-service ./services/reindex-coordinator-service/cmd/reindex-coordinator-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
