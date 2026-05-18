# lineage-service

## LLM context

Owns lineage graph ingestion/query APIs, OpenLineage events, upstream/downstream traversal, and optional Kafka-to-Iceberg lineages.

Agent note: can run HTTP-health-only depending on LINEAGE_RUNTIME_MODE.

## Entrypoints

- `cmd/lineage-service/main.go` builds the `lineage-service` binary.

## Current HTTP / runtime surface

- `GET /api/v1/lineage/upstream/{rid}`
- `GET /api/v1/lineage/downstream/{rid}`
- `GET /api/v1/lineage/job/{namespace}/{name}/runs`
- `POST /api/v1/lineage/events`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `3` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `icebergschema`, `kafkatoiceberg`, `lineage`, `lineageconsumer`, `lineagegraph`, `lineagestore`, `models`, `openlineage`, `queryrouter`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `AI_SERVICE_URL`, `DATABASE_URL`, `DATASET_SERVICE_URL`, `DATA_DIR`, `DISTRIBUTED_COMPUTE_POLL_INTERVAL_MS`, `DISTRIBUTED_COMPUTE_TIMEOUT_SECS`, `DISTRIBUTED_PIPELINE_WORKERS`, `HOST`
- `ICEBERG_CATALOG_URL`, `JWT_SECRET`, `KAFKA_BOOTSTRAP_SERVERS`, `LINEAGE_RUNTIME_MODE`, `LINEAGE_TRINO_ENABLED`, `LOCAL_STORAGE_ROOT`, `PORT`, `S3_ACCESS_KEY`
- `S3_ENDPOINT`, `S3_REGION`, `S3_SECRET_KEY`, `SERVICE_VERSION`, `STORAGE_BACKEND`, `STORAGE_BUCKET`, `WORKFLOW_SERVICE_URL`

## Build

```sh
go build -o bin/lineage-service ./services/lineage-service/cmd/lineage-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
