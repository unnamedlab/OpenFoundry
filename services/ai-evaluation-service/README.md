# ai-evaluation-service

## LLM context

Runs AI benchmark and guardrail evaluation endpoints.

Agent note: stores evaluation/guardrail records in Postgres and protects productive routes with JWT.

## Entrypoints

- `cmd/ai-evaluation-service/main.go` builds the `ai-evaluation-service` binary.

## Current HTTP / runtime surface

- `POST /api/v1/evaluations/benchmark`
- `POST /api/v1/guardrails/evaluate`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `2` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/ai-evaluation-service ./services/ai-evaluation-service/cmd/ai-evaluation-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
