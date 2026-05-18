# audit-compliance-service

## LLM context

Owns audit/compliance APIs: audit-event reads, retention policies, SDS scans, GDPR/data-subject helpers, and lineage-delete coordination.

Agent note: delegates impact checks to lineage-service when configured.

## Entrypoints

- `cmd/audit-compliance-service/main.go` builds the `audit-compliance-service` binary.

## Current HTTP / runtime surface

- `/api/v1/audit/events*`
- `/api/v1/retention/policies*`
- `POST /api/v1/sds/scan`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `10` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `handlers`, `lineagedeletion`, `models`, `openapi`, `repo`, `retentionpolicy`, `sds`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `JWT_SECRET`, `LINEAGE_SERVICE_URL`, `METRICS_ADDR`, `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/audit-compliance-service ./services/audit-compliance-service/cmd/audit-compliance-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
