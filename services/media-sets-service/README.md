# media-sets-service

## LLM context

Owns media sets, media items, branches, transactions, access patterns, retention, virtual items, markings, presigned access, and gRPC support.

Agent note: delegates transformations to media-transform-runtime-service and connector resolution to connector-management when configured.

## Entrypoints

- `cmd/media-sets-service/main.go` builds the `media-sets-service` binary.

## Current HTTP / runtime surface

- `/api/v1/media-sets/{rid}/items|branches|transactions|retention|access-patterns`
- `/api/v1/items/{rid}[/download|/markings]`
- `/api/v1/transactions/{rid}/commit|abort`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `8` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `accesspatterns`, `branches`, `cedarauthz`, `config`, `connectorclient`, `connectorresolver`, `grpcserver`, `handlers`, `mediaitems`, `mediapath`, `metrics`, `models`, `presignclaim`, `repo`, `retention`, `server`, `storage`, `transactions``, ... .
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `CONNECTOR_MANAGEMENT_SERVICE_URL`, `DATABASE_URL`, `HOST`, `JWT_SECRET`, `MEDIA_PRESIGN_TTL_SECONDS`, `MEDIA_RETENTION_REAPER_INTERVAL_SECONDS`, `MEDIA_SETS_GRPC_PORT`, `MEDIA_STORAGE_BUCKET`
- `MEDIA_STORAGE_ENDPOINT`, `MEDIA_TRANSFORM_RUNTIME_URL`, `METRICS_ADDR`, `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/media-sets-service ./services/media-sets-service/cmd/media-sets-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
