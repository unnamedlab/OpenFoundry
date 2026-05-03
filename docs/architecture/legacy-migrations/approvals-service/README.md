# Archived approvals migrations

This folder collects the **legacy Postgres state** for the approvals
domain that was authoritative before Stream **S2.5** (see
`docs/architecture/migration-plan-cassandra-foundry-parity.md`).

## What changed

- **Authoritative store** for an approval is now a Temporal workflow
  execution (`ApprovalRequestWorkflow` on task queue
  `openfoundry.approvals`). Decisions arrive as `decide` signals;
  state lives in the workflow event history.
- The Postgres table `workflow_approvals` (defined in
  `services/workflow-automation-service/migrations/20260421140000_workflows.sql`,
  rows 36–53) is **deprecated** and must not be written to from new
  code paths. As of G-S2-PG (2026-05-03), live Rust handlers no
  longer read or write it; `drop_workflow_approvals.sql.disabled`
  remains staged for the database cutover window.

## Cutover gate

The DROP migration **MUST NOT** run before:

1. every persisted `workflow_approvals` row with `status='pending'`
   has either been replayed into Temporal or has expired naturally;
2. all callers of `services/approvals-service/src/handlers/approvals.rs`
   have switched to `domain::temporal_adapter::ApprovalsAdapter`;
3. the audit-event consumer reports zero `approval.*` events sourced
   from the Postgres path for at least 7 days.

## Pointers

- New entry point: [`approvals_service::domain::temporal_adapter::ApprovalsAdapter`](../../../services/approvals-service/src/domain/temporal_adapter.rs).
- Worker: [`workers-go/approvals/workflows/approval_request.go`](../../../workers-go/approvals/workflows/approval_request.go).
- Audit activity: [`workers-go/approvals/activities/activities.go`](../../../workers-go/approvals/activities/activities.go).
- Plan reference: `docs/architecture/migration-plan-cassandra-foundry-parity.md` §S2.5.

> **Do not resurrect** the legacy CRUD path on `workflow_approvals`.
> If you find yourself reaching for it, check whether the workflow
> needs a new query handler or a new signal type instead.
