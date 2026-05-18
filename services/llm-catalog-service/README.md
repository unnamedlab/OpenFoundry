# llm-catalog-service

## LLM context

Owns LLM model registry/admin CRUD, model invocation proxying, provider config, kernel defaults, and purpose-checkpoint integration.

Agent note: admin CRUD and invocation have different auth expectations.

## Entrypoints

- `cmd/llm-catalog-service/main.go` builds the `llm-catalog-service` binary.

## Current HTTP / runtime surface

- `/api/v1/llm/models* (admin CRUD)`
- `POST /api/v1/llm/invoke`
- `/api/v1/kernel-defaults`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `ANTHROPIC_API_KEY`, `ANTHROPIC_BASE_URL`, `CHECKPOINTS_PURPOSE_SERVICE_URL`, `DATABASE_URL`, `HOST`, `JWT_SECRET`, `LLM_RATE_LIMIT_CAPACITY`, `LLM_RATE_LIMIT_REFILL_PER_SECOND`
- `OLLAMA_BASE_URL`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/llm-catalog-service ./services/llm-catalog-service/cmd/llm-catalog-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
