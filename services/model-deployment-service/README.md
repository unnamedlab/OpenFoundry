# model-deployment-service

## LLM context

Owns model deployment lifecycle records and status transitions.

Agent note: supports both /api/v1/deployments and legacy /api/v1/model-deployment/deployments aliases.

## Entrypoints

- `cmd/model-deployment-service/main.go` builds the `model-deployment-service` binary.

## Current HTTP / runtime surface

- `/api/v1/deployments*`
- `PATCH /api/v1/deployments/{id}/status`
- `/api/v1/model-deployment/deployments* alias`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `MODEL_SERVING_BACKEND_URL`, `OF_MODEL_DEPLOYMENT_RUNTIME`, `OF_MODEL_SERVING_BACKEND_URL`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/model-deployment-service ./services/model-deployment-service/cmd/model-deployment-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
