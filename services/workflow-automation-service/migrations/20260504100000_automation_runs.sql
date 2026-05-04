-- FASE 5 / Tarea 5.2 — `workflow_automation` state-machine table.
--
-- Backs the AutomationRun aggregate that replaces the Temporal
-- `WorkflowAutomationRun` workflow registered today on
-- `workers-go/workflow-automation/` (see FASE 5 / Tarea 5.1 inventory
-- at `docs/architecture/refactor/workflow-automation-worker-inventory.md`).
--
-- After the FASE 5 cutover (Tarea 5.3 wires the consumer + dispatcher;
-- DSN flips from the per-service `workflow-automation-pg` cluster to
-- `pg-runtime-config`) the authoritative location of this table is:
--
--     pg-runtime-config.workflow_automation.automation_runs
--
-- The `workflow_automation` schema and the owning role
-- `svc_workflow_automation` are pre-created by the cluster bootstrap
-- at `infra/helm/infra/postgres-clusters/templates/clusters/`
-- `pg-runtime-config-bootstrap-sql.yaml` (the `workflow_automation`
-- bounded context already lives in the bootstrap `bcs[]` array), so
-- this migration only needs `CREATE TABLE` — no `CREATE SCHEMA` or
-- `GRANT` boilerplate.
--
-- Conventions are inherited from `libs/state-machine`'s template
-- (`libs/state-machine/migrations/0001_state_machine_template.sql`):
-- the standard six columns (`id`, `state`, `state_data`, `version`,
-- `expires_at`, `created_at`, `updated_at`) plus the three columns
-- explicitly required by the migration plan (`tenant_id`,
-- `definition_id`, `correlation_id`) so dashboards and operator
-- queries do not have to crack open `state_data`.
--
-- Per the migration plan FASE 5 / Tarea 5.2, the allowed states are:
--
--     queued       — row created from a `automate.condition.v1`
--                    event (or a synchronous handler) before the
--                    consumer picks it up.
--     running      — the consumer is actively dispatching the effect
--                    (HTTP POST `ontology-actions-service::POST
--                    /api/v1/ontology/actions/{id}/execute`).
--     suspended    — multi-step automation is waiting on an external
--                    signal (human-in-the-loop / cron / downstream
--                    event). Re-entered as `running` when the wait
--                    resolves.
--     compensating — saga rollback in progress (active reversal of
--                    completed sub-steps). Always terminal-bound
--                    (`failed`).
--     completed    — terminal success. `state_data.effect_response`
--                    carries the upstream payload.
--     failed       — terminal failure. `state_data.last_error`
--                    carries the operator-facing message.
--
-- Allowed transitions are enforced in code by
-- `services/workflow-automation-service::domain::automation_run::
-- AutomationRunState::can_transition_to` and (defence-in-depth)
-- by the SQL `CHECK` constraint on the `state` column.

CREATE TABLE IF NOT EXISTS workflow_automation.automation_runs (
    -- Aggregate id, surfaced by `StateMachine::aggregate_id`.
    -- For idempotency on retried producer publishes, callers should
    -- derive this as `uuid_v5(definition_id || correlation_id)` (per
    -- ADR-0038 §Idempotency contract); this is enforced by Tarea 5.3
    -- in the condition consumer, not at the schema level.
    id              uuid        PRIMARY KEY,

    -- Owning tenant. Carried for partition-by-tenant queries on the
    -- read side and for tenant-scoped audit. The producer paths in
    -- §4.1 of the inventory pass `tenant_id` either as a string from
    -- `workflow.trigger_config.tenant_id` or as the workflow owner
    -- UUID; Tarea 5.3 normalises both into a single Uuid.
    tenant_id       uuid        NOT NULL,

    -- Foreign reference to the workflow definition that produced
    -- this run. The `workflows` catalog table (created by
    -- `20260421140000_workflows.sql`) lives in a different cluster
    -- today (`workflow-automation-pg`); after the FASE 5 cutover it
    -- will be co-located here. We do **not** declare a
    -- REFERENCES constraint yet — that lands in the cutover migration
    -- so this file remains applicable in both topologies.
    definition_id   uuid        NOT NULL,

    -- Short string rendering of the current `AutomationRunState`.
    -- Indexed for `timeout_sweep` and ad-hoc operator queries.
    -- The `CHECK` constraint enforces the same enum the Rust
    -- code accepts; adding a new state requires a schema migration
    -- AND a code change in lockstep.
    state           text        NOT NULL CHECK (state IN (
                        'queued',
                        'running',
                        'suspended',
                        'compensating',
                        'completed',
                        'failed'
                    )),

    -- Full `AutomationRun` aggregate serialised as JSON (per
    -- `libs/state-machine` PgStore contract). Operator-facing
    -- summaries live in the dedicated columns below; everything
    -- else (attempt counters, last_error, effect_response, the
    -- multi-step `progress` blob) lives here.
    state_data      jsonb       NOT NULL,

    -- Optimistic concurrency token. Bumped by every `PgStore::apply`.
    -- Two consumer replicas racing on the same row produce at most
    -- one successful UPDATE; the loser sees `StoreError::Stale` and
    -- reloads.
    version         bigint      NOT NULL DEFAULT 1 CHECK (version > 0),

    -- Optional timeout deadline. The cron-backed timeout sweep
    -- (Tarea 5.3 follow-up; mirror of FASE 7 / Tarea 7.4) reads
    --     SELECT … FROM automation_runs
    --      WHERE expires_at IS NOT NULL AND expires_at <= now()
    -- via `PgStore::timeout_sweep` and feeds matching rows the
    -- timeout transition. The partial index below keeps that scan
    -- cheap when the bulk of rows have no deadline.
    expires_at      timestamptz,

    -- End-to-end audit correlation id. Today the Temporal adapter
    -- ([`temporal_adapter.rs:80-89`]) sets the audit search attribute
    -- from the `run_id`; post-migration the same UUID flows on the
    -- `automate.condition.v1` event header and onto the outbound
    -- `x-audit-correlation-id` HTTP header on the effect call.
    correlation_id  uuid        NOT NULL,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Operator query: "what is currently running for this definition?".
-- Replaces the use case served today by the (write-orphan)
-- `workflow_run_projections` table (see inventory §4.3).
CREATE INDEX IF NOT EXISTS automation_runs_definition_idx
    ON workflow_automation.automation_runs (definition_id, created_at DESC);

-- Operator query: "what is in flight for this tenant right now?".
CREATE INDEX IF NOT EXISTS automation_runs_tenant_state_idx
    ON workflow_automation.automation_runs (tenant_id, state)
    WHERE state IN ('queued', 'running', 'suspended', 'compensating');

-- Restart-time recovery: every row not yet in a terminal state.
-- The condition consumer drives the catch-up loop off this index
-- on startup; it is also what the timeout sweep narrows further by
-- joining on `expires_at`.
CREATE INDEX IF NOT EXISTS automation_runs_state_idx
    ON workflow_automation.automation_runs (state)
    WHERE state IN ('queued', 'running', 'suspended', 'compensating');

-- Timeout-sweep partial index — exactly mirrors the
-- `__table___expires_at_idx` from `libs/state-machine`'s template.
CREATE INDEX IF NOT EXISTS automation_runs_expires_at_idx
    ON workflow_automation.automation_runs (expires_at)
    WHERE expires_at IS NOT NULL;

-- Audit / lineage lookup by correlation id (single row in the
-- common case; supports the rare cross-run trace stitching).
CREATE INDEX IF NOT EXISTS automation_runs_correlation_idx
    ON workflow_automation.automation_runs (correlation_id);
