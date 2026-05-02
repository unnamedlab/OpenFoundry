# Legacy in-process executor — `workflow-automation-service`

This directory archives the **in-process scheduler** previously
hosted at `services/workflow-automation-service/src/domain/executor.rs`
(1 297 LOC at the time of archival, 2026-05). It is preserved as
`.legacy` files (non-Rust extension) so the workspace doesn't
attempt to compile them.

## Why archived

Per Stream **S2.3.a** of
[`migration-plan-cassandra-foundry-parity.md`](../../migration-plan-cassandra-foundry-parity.md)
and **ADR-0021**, every workflow scheduling primitive moves to
**Apache Temporal**. The Rust service becomes a thin REST adapter
that translates HTTP requests into `WorkflowClient::start_workflow`
calls (S2.3.d) — no more in-process tick loops, no more
`compute_next_run_at`, no more `execute_workflow_run` running on the
HTTP request thread.

## What lives here

| File                         | Original path                                                | Replacement                                                    |
|------------------------------|--------------------------------------------------------------|----------------------------------------------------------------|
| `executor.rs.legacy`         | `services/workflow-automation-service/src/domain/executor.rs` | Workflows in `workers-go/workflow-automation/workflows/` (S2.3.b). |

## Migration map (handler-by-handler, deferred per ADR-0024 cadence)

* `executor::execute_workflow_run` → workflow type `WorkflowAutomationRun`
  (`workers-go/workflow-automation/workflows/automation_run.go`).
  The Rust adapter calls
  `WorkflowAutomationClient::start_run` from
  `libs/temporal-client`.
* `executor::compute_next_run_at` → **deleted**. Cron logic lives in
  Temporal Schedules (S2.4) — the schedule's `cron_expressions` are
  the single source of truth.
* `executor::continue_after_approval` → workflow Signal `decide`
  (S2.5). The Rust adapter calls
  `ApprovalsClient::decide`.
* Branch / parallel / compensation / human-in-the-loop helpers →
  child workflows + selectors inside the Go workflow definition.

## Do NOT resurrect

If a handler in `services/workflow-automation-service/src/handlers/`
still imports `domain::executor`, it is **orphaned code from before
the archival** (the binary is `fn main() {}`, so nothing was being
compiled). The handler must be rewritten against the
`temporal-client` facade in its migration PR; do not copy code back
out of the `.legacy` files.
