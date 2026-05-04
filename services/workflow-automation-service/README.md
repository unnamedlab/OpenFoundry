# `workflow-automation-service`

Foundry-pattern runtime for business automations. After
FASE 5 / Tarea 5.3 of the
[orchestration migration plan](../../docs/architecture/migration-plan-foundry-pattern-orchestration.md),
this binary owns the entire lifecycle of an `AutomationRun`: HTTP
catalog API, condition consumer, effect dispatcher and outcome
publisher all live in the same Rust process. The legacy Go Temporal
worker at `workers-go/workflow-automation/` was retired by Tarea 5.4.

## Topology

```text
   user / webhook / svc-to-svc ──POST /workflows/{id}/runs──┐
                                                              │
   pipeline-schedule-service (NATS, legacy) ─────────────────►│
                                                              ▼
                                  ┌────────────────────────────────┐
                                  │  HTTP API + outbox publisher   │
                                  │  - inserts automation_runs row │
                                  │    (state=Queued)              │
                                  │  - INSERT outbox.events        │
                                  │  - returns 202                 │
                                  └────────────────────────────────┘
                                                              │
                                                  Debezium    │
                                                              ▼
                                                ┌──────────────────────┐
                                                │ automate.condition.v1│
                                                └──────────────────────┘
                                                              │
                                                              ▼
                                  ┌────────────────────────────────┐
                                  │  Condition consumer            │
                                  │  - libs/idempotency dedup      │
                                  │  - state-machine apply (Claim) │
                                  │  - HTTP POST                   │
                                  │    ontology-actions-service    │
                                  │    (5 attempts, 30s→10m exp)   │
                                  │  - state-machine apply         │
                                  │    (EffectCompleted/Failed) +  │
                                  │    INSERT outbox.events        │
                                  │    (one TX, atomic)            │
                                  └────────────────────────────────┘
                                                              │
                                                  Debezium    │
                                                              ▼
                                                ┌──────────────────────┐
                                                │ automate.outcome.v1  │
                                                └──────────────────────┘
```

State lives in `pg-policy.workflow_automation` (same per-service CNPG
cluster as today; see [`k8s/README.md`](k8s/README.md)). Tables
created by `migrations/`:

- `workflow_automation.automation_runs` — state machine row, six
  standard `libs/state-machine` columns plus `tenant_id`,
  `definition_id`, `correlation_id` projections.
- `workflow_automation.processed_events` — per-condition dedup
  table (`libs/idempotency`).
- `outbox.events` — Debezium-captured outbox (`libs/outbox`).

## Configuration

| Env var | Default | Purpose |
|---|---|---|
| `DATABASE_URL` | _(required)_ | `workflow-automation-pg` DSN |
| `JWT_SECRET` | _(required)_ | HS256 secret for API auth |
| `KAFKA_BOOTSTRAP_SERVERS` | _(required for consumer)_ | Comma-separated brokers |
| `KAFKA_SASL_USERNAME` / `KAFKA_SASL_PASSWORD` / `KAFKA_SASL_MECHANISM` / `KAFKA_SECURITY_PROTOCOL` | _(unset)_ / _(unset)_ / `SCRAM-SHA-512` / `SASL_SSL` | Optional SASL/SCRAM |
| `OF_ONTOLOGY_ACTIONS_URL` (or `ONTOLOGY_ACTIONS_SERVICE_URL`, `ONTOLOGY_SERVICE_URL`, `OF_ONTOLOGY_ACTIONS_GRPC_ADDR`) | _(required for consumer)_ | Effect dispatch base URL |
| `OF_ONTOLOGY_ACTIONS_BEARER_TOKEN` (or `ONTOLOGY_ACTIONS_BEARER_TOKEN`) | _(required for consumer)_ | Service bearer token |
| `OF_OPENLINEAGE_NAMESPACE` | `openfoundry` | OpenLineage namespace stamped on outbound events |
| `NATS_URL` | `nats://localhost:4222` | Legacy NATS bus for `of.workflows.run.requested` (kept until producer migrates) |
| `WORKFLOW_AUTOMATION_SERVICE__HOST` / `WORKFLOW_AUTOMATION_SERVICE__PORT` | `0.0.0.0` / `50137` | HTTP listener |

If `KAFKA_BOOTSTRAP_SERVERS`, `OF_ONTOLOGY_ACTIONS_URL` or the
bearer token are missing the consumer task is skipped and the HTTP
API stays up — useful for dev environments where Kafka is not
running. The startup log records `skipping automate.condition.v1
consumer; HTTP API still online` when this happens.

## Verification

```bash
# Trigger a manual automation run.
curl -X POST -H 'Authorization: Bearer <jwt>' \
  -H 'Content-Type: application/json' \
  http://localhost:50137/api/v1/workflows/<id>/runs \
  -d '{"context": {"action_id": "promote-customer", "parameters": {"priority": "high"}}}'

# Watch the outcome.
kafkacat -C -t automate.outcome.v1 -f '%k → %s\n'

# Inspect the row.
psql "$DATABASE_URL" -c \
  "SELECT id, state, attempts, last_error FROM workflow_automation.automation_runs ORDER BY created_at DESC LIMIT 5"
```

## Migration cross-references

- Per-worker inventory:
  [`docs/architecture/refactor/workflow-automation-worker-inventory.md`](../../docs/architecture/refactor/workflow-automation-worker-inventory.md)
- Tarea 5.2 — state machine schema:
  [`migrations/20260504100000_automation_runs.sql`](migrations/20260504100000_automation_runs.sql)
- Tarea 5.3 — outbox + idempotency schema:
  [`migrations/20260504200000_outbox_and_idempotency.sql`](migrations/20260504200000_outbox_and_idempotency.sql)
- Tarea 5.3 — Rust state machine impl:
  [`src/domain/automation_run.rs`](src/domain/automation_run.rs)
- Tarea 5.3 — effect dispatcher:
  [`src/domain/effect_dispatcher.rs`](src/domain/effect_dispatcher.rs)
- Tarea 5.3 — condition consumer:
  [`src/domain/condition_consumer.rs`](src/domain/condition_consumer.rs)
- Tarea 5.4 — Go worker deletion: removed from
  `workers-go/workflow-automation/`, helm `of-platform`
  workers list, CI matrix and justfile.
