-- S2.7.b — DROP `automation_queues` + `automation_queue_runs` after Temporal cutover.
-- Cutover gate and rationale: docs/architecture/legacy-migrations/automation-operations-service/README.md.
-- The authoritative store is the Temporal workflow `AutomationOpsTask`
-- (task queue `openfoundry.automation-ops`).

DROP INDEX IF EXISTS idx_automation_queue_runs_parent_id;
DROP INDEX IF EXISTS idx_automation_queues_created_at;
DROP TABLE IF EXISTS automation_queue_runs;
DROP TABLE IF EXISTS automation_queues;
