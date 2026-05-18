# retrieval-context-service

## LLM context

Owns retrieval context, knowledge-base retrieval/search-adjacent state, conversations, and auth probe routes.

Agent note: business routes are deliberately small and wire-compatible with ai-kernel expectations.

## Entrypoints

- `cmd/retrieval-context-service/main.go` builds the `retrieval-context-service` binary.

## Current HTTP / runtime surface

- `/api/v1/knowledge-bases*`
- `/api/v1/conversations*`
- `/api/v1/_authz_probe`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/retrieval-context-service ./services/retrieval-context-service/cmd/retrieval-context-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
