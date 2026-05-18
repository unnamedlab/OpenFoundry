# media-transform-runtime-service

## LLM context

Runs media transform catalog and transform execution runtime.

Agent note: exposes /catalog and /transform in addition to health/metrics; it is a worker/runtime, not the media-set metadata store.

## Entrypoints

- `cmd/media-transform-runtime-service/main.go` builds the `media-transform-runtime-service` binary.

## Current HTTP / runtime surface

- `GET /catalog`
- `GET /catalog/{kind}`
- `POST /transform`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- No SQL migration files live under this service directory.
- Main internal packages: `catalog`, `config`, `handlers`, `runtime`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `HOST`, `MEDIA_TRANSFORM_PORT`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/media-transform-runtime-service ./services/media-transform-runtime-service/cmd/media-transform-runtime-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
