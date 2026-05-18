# workflow-automation-service

## LLM context

Owns workflows, automations, runs, approvals, schedule/scheduler state, approval timeout sweep, and automation operations.

Agent note: contains both the HTTP service and approvals-timeout-sweep command; Temporal migration notes live under docs/architecture/legacy-migrations.

## Entrypoints

- `cmd/approvals-timeout-sweep/main.go` builds the `approvals-timeout-sweep` binary.
- `cmd/workflow-automation-service/main.go` builds the `workflow-automation-service` binary.

## Current HTTP / runtime surface

- `/api/v1/workflows*`
- `/api/v1/automations*`
- `/api/v1/approvals*`
- `/api/v1/workflows/{id}/webhook`
- `/api/v1/audit/retention/sweep`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `10` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `approvals`, `automationoperations`, `config`, `domain`, `event`, `handlers`, `models`, `repo`, `scheduler`, `server`, `state`, `topics`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `APPROVAL_TTL_HOURS`, `AUDIT_COMPLIANCE_BEARER_TOKEN`, `AUDIT_COMPLIANCE_SERVICE_URL`, `DATABASE_URL`, `HOST`, `JWT_SECRET`, `KAFKA_BOOTSTRAP_SERVERS`, `NATS_URL`
- `NOTIFICATION_SERVICE_URL`, `OF_OPENLINEAGE_NAMESPACE`, `OF_WORKFLOW_SCHEDULER_ENABLED`, `ONTOLOGY_SERVICE_URL`, `PIPELINE_SERVICE_URL`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/approvals-timeout-sweep ./services/workflow-automation-service/cmd/approvals-timeout-sweep
```
```sh
go build -o bin/workflow-automation-service ./services/workflow-automation-service/cmd/workflow-automation-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
