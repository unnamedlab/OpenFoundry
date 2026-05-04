SQL migrations for `automation-operations-service`.

After FASE 6 of the Foundry-pattern migration
([ADR-0037](../../../docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md))
the authoritative runtime is the in-process saga substrate
(`libs/saga::SagaRunner` over `saga.state`) plus the per-service
outbox + idempotency tables. The Postgres tables provisioned here
are the live source of truth for every automation run; there is
no longer any external orchestrator that overrides them.

Active migrations:

* `20260504300000_saga_state_and_outbox.sql` — schema for the
  Foundry-pattern runtime (`saga.state`, `outbox.events`,
  `automation_operations.processed_events`). FASE 6 / Tarea 6.2.

The legacy Postgres queue tables that used to live here
(`automation_queues`, `automation_queue_runs`) were archived under
[`docs/architecture/legacy-migrations/automation-operations-service/`](../../../docs/architecture/legacy-migrations/automation-operations-service/)
during the Cassandra-parity migration (S2.7) and dropped before
FASE 6 began.
