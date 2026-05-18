# ingestion-replication-service

## LLM context

Owns ingest jobs, streaming streams, stream branches, schema validation/history, hot buffers, topology run/replay, and replication/reconcile paths.

Agent note: integrates with Kafka/Flink runtime URLs and dataset-service when configured.

## Entrypoints

- `cmd/ingestion-replication-service/main.go` builds the `ingestion-replication-service` binary.

## Current HTTP / runtime surface

- `/api/v1/ingest-jobs*`
- `/api/v1/streaming/streams*`
- `/api/v1/streaming/topologies/{id}:run|replay`
- `/api/v1/streaming/streams/{id}/schema:validate`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `11` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `engine`, `handlers`, `hotbuffer`, `models`, `reconcile`, `repo`, `runtime`, `server`, `storage`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `DATASET_SERVICE_URL`, `FLINK_RUNTIME_URL`, `HOST`, `JWT_SECRET`, `KAFKA_BOOTSTRAP_SERVERS`, `KAFKA_RUNTIME_URL`, `METRICS_ADDR`
- `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/ingestion-replication-service ./services/ingestion-replication-service/cmd/ingestion-replication-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
