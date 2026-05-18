# code-repository-review-service

## LLM context

Backs code-security scan creation, review-oriented global-branch metadata endpoints, and managed bare-Git Code Repository hosting.

Agent note: routes are mounted under `/v1`, not `/api/v1`; the edge gateway may add or rewrite prefixes externally.

## Entrypoints

- `cmd/code-repository-review-service/main.go` builds the `code-repository-review-service` binary.

## Current HTTP / runtime surface

- `POST /v1/code-security/scans`
- `GET /v1/code-repos/templates`
- `/v1/code-repos/repositories*` (including Git-backed `/files` editor actions)
- `/v1/code-repos/git/{repository_id}.git*` (Git Smart HTTP; Bearer token or Basic password OIDC JWT)
- `/v1/global-branches*`
- `GET /healthz`
- `GET /healthz/json`
- `GET /metrics`

## State and dependencies

- Contains Code Repository, code-security and global-branch SQL migrations; check service migrations before changing persisted models.
- Main internal packages: `codesecurity`, `config`, `handlers`, `models`, `repo`, `server`, `subscriber`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `BRANCH_EVENTS_CONSUMER_GROUP`, `CODE_REPOSITORY_GIT_HTTP_BASE_URL`, `CODE_REPOSITORY_GIT_ROOT`, `CODE_REPOSITORY_GIT_SSH_BASE_URL`, `CODE_REPOSITORY_GIT_SSH_ENABLED`, `DATABASE_URL`, `HOST`, `JWT_SECRET`, `KAFKA_BOOTSTRAP_SERVERS`, `KAFKA_BROKERS`, `PORT`, `SERVICE_ACTOR`
- `SERVICE_VERSION`

## Build

```sh
go build -o bin/code-repository-review-service ./services/code-repository-review-service/cmd/code-repository-review-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
