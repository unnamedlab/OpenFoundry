# Archived automation-operations migrations

This folder collects the **legacy Postgres state** for the
automation-ops control plane that was authoritative before Stream
**S2.7** (see `docs/architecture/migration-plan-cassandra-foundry-parity.md`).

## What changed

- **Authoritative store** for an automation-ops task is now a Temporal
  workflow execution (`AutomationOpsTask` on task queue
  `openfoundry.automation-ops`). Queue entries, retries and
  dependencies live in workflow event history.
- The Postgres tables `automation_queues` and `automation_queue_runs`
  (defined in
  `services/automation-operations-service/migrations/20260427070600_06_automation_queues_foundation.sql`)
  are **deprecated** and must not be written to from new code paths.
  The substrate keeps them in place for read-side projections during
  the cutover.

## Cutover gate

The DROP migration (`drop_automation_queues.sql.disabled`) **MUST
NOT** run before:

1. every persisted `automation_queues` row with an active state has
   been replayed into Temporal or has terminated naturally;
2. all callers of `automation_operations_service::handlers::*`
   write paths have switched to
   `automation_operations_service::domain::temporal_adapter::AutomationOpsAdapter`;
3. the audit-event consumer reports zero `automation_ops.*` events
   sourced from the Postgres path for at least 7 days.

## Pointers

- New entry point: [`automation_operations_service::domain::temporal_adapter::AutomationOpsAdapter`](../../../services/automation-operations-service/src/domain/temporal_adapter.rs).
- Worker: [`workers-go/automation-ops/workflows/automation_ops_task.go`](../../../workers-go/automation-ops/workflows/automation_ops_task.go).
- Plan reference: `docs/architecture/migration-plan-cassandra-foundry-parity.md` §S2.7.

> **Do not resurrect** the legacy CRUD path on `automation_queues` /
> `automation_queue_runs`. New write paths must go through the
> Temporal adapter.
