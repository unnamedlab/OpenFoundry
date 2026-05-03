-- S2.5.d — DROP `workflow_approvals` after Temporal cutover.
-- Cutover gate and rationale: docs/architecture/legacy-migrations/approvals-service/README.md.
-- The authoritative store for approvals is the Temporal workflow
-- `ApprovalRequestWorkflow` (task queue `openfoundry.approvals`).

DROP INDEX IF EXISTS idx_workflow_approvals_assigned;
DROP INDEX IF EXISTS idx_workflow_approvals_run;
DROP TABLE IF EXISTS workflow_approvals;
