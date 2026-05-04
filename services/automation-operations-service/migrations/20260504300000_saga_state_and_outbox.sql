-- FASE 6 / Tarea 6.2 — saga state + outbox + idempotency for the
-- Foundry-pattern runtime of `automation-operations-service`.
--
-- This migration provisions the three tables every Foundry-pattern
-- saga runtime needs (per ADR-0022 outbox + ADR-0037 saga
-- choreography + ADR-0038 idempotency):
--
--   * `automation_operations.saga_state` — per-bounded-context copy
--     of the `libs/saga` template
--     (`libs/saga/migrations/0001_saga_state.sql`). Backs
--     `state_machine::PgStore`-style optimistic-concurrency writes
--     done by `saga::SagaRunner`. Source of truth for
--     in-flight / terminal sagas owned by this service.
--
--   * `outbox.events` — transactional outbox (`libs/outbox`
--     contract). The condition consumer (Tarea 6.3 deliverable) and
--     the HTTP "start saga" handler both write here so the
--     `saga.step.*` Kafka publishes happen in the same Postgres
--     transaction as the state-machine row update. Captured by the
--     per-cluster Debezium Postgres connector.
--
--   * `automation_operations.processed_events` — single-column
--     primary-key dedup table backing
--     `idempotency::PgIdempotencyStore`. The saga consumer records
--     the deterministic `event_id` of every inbound
--     `saga.step.requested.v1` here BEFORE side-effecting (per
--     ADR-0038 record-before-process), so a Kafka redelivery after
--     a crash skips re-dispatching the step instead of issuing the
--     effect call twice.
--
-- Scope notes:
--   * The system-wide audit projection of every saga event lives in
--     `pg-policy.audit_compliance.saga_audit_log`
--     (see `services/audit-compliance-service/migrations/`); that
--     table is read-only from this service.
--   * Tarea 6.3 wires the consumer + step graph; this migration just
--     provisions the schema.
--   * The `automation_operations` schema and the owning role
--     `svc_automation_operations` are pre-created by the
--     pg-runtime-config bootstrap
--     (`infra/helm/infra/postgres-clusters/templates/clusters/`
--     `pg-runtime-config-bootstrap-sql.yaml` — the bounded context
--     already lives in the `bcs[]` array). When the service DSN
--     points at the legacy per-service cluster the schema is
--     created on the fly here so the migration is portable across
--     both topologies.

CREATE SCHEMA IF NOT EXISTS automation_operations;

-- ──────────────────────────── Outbox table ────────────────────────────

CREATE SCHEMA IF NOT EXISTS outbox;

-- Mirrors `services/workflow-automation-service/migrations/`
-- `20260504200000_outbox_and_idempotency.sql`. Per-bounded-context
-- outbox tables are independent (ADR-0022 §"per-service outbox") so
-- each service's Debezium connector watches its own table.
CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     uuid        PRIMARY KEY,
    aggregate    text        NOT NULL,
    aggregate_id text        NOT NULL,
    payload      jsonb       NOT NULL,
    headers      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    topic        text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE outbox.events REPLICA IDENTITY FULL;

CREATE INDEX IF NOT EXISTS outbox_events_created_at_idx
    ON outbox.events (created_at);

-- ───────────────────────── Saga operational state ────────────────────

-- One row per saga instance, owned by `saga::SagaRunner`.
-- Schema mirrors the template at
-- `libs/saga/migrations/0001_saga_state.sql` exactly — the only
-- difference is the schema name (`automation_operations` instead of
-- the helper crate's `saga`) so the bounded-context role does not
-- need cross-schema privileges.
--
-- Idempotency contract (per `libs/saga` README §Idempotency):
-- re-running a saga with the same `saga_id` reads back
-- `completed_steps` and short-circuits the already-finished prefix.
-- Producer redeliveries that hit the same `saga_id` collapse onto
-- the same row via `INSERT ... ON CONFLICT (saga_id) DO NOTHING`.
CREATE TABLE IF NOT EXISTS automation_operations.saga_state (
    saga_id          uuid PRIMARY KEY,
    -- Saga type — the dispatch key the runtime's step-graph
    -- registry uses. Free-form at the schema level; the consumer
    -- rejects unknown types into `failed`.
    name             text NOT NULL,
    -- Mirrors `saga::SagaStatus`.
    status           text NOT NULL DEFAULT 'running' CHECK (status IN (
                         'running',
                         'completed',
                         'failed',
                         'compensated',
                         'aborted'
                     )),
    -- Step currently in flight; NULL when idle (between steps or
    -- after a terminal transition).
    current_step     text,
    -- Names of every step that has succeeded so far, in execution
    -- order. Drives both the idempotent-replay short-circuit and
    -- the compensation order on failure (LIFO over this array).
    completed_steps  text[] NOT NULL DEFAULT '{}',
    -- JSON object keyed by step_name carrying the serialised
    -- `Output` of each completed step. Read back when an idempotent
    -- retry "re-executes" a step that already finished.
    step_outputs     jsonb  NOT NULL DEFAULT '{}'::jsonb,
    -- Name of the step that raised the error, if any. NULL on
    -- happy-path terminations.
    failed_step      text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- Operator query: "what is currently running for this tenant /
-- saga type?". The most common dashboard cut.
CREATE INDEX IF NOT EXISTS saga_state_status_idx
    ON automation_operations.saga_state (status)
    WHERE status IN ('running');

-- Restart-time recovery: every saga that crashed mid-step. The
-- consumer's catch-up loop drives off this index instead of
-- replaying Kafka.
CREATE INDEX IF NOT EXISTS saga_state_running_with_step_idx
    ON automation_operations.saga_state (saga_id)
    WHERE status = 'running' AND current_step IS NOT NULL;

-- ─────────────────────────── Idempotency table ────────────────────────

-- Mirrors `libs/idempotency::PgIdempotencyStore` contract: a single
-- `event_id uuid PRIMARY KEY` plus an operator-facing
-- `processed_at`. Used by the saga consumer to dedup inbound
-- `saga.step.requested.v1` events; the deterministic `event_id` is
-- UUIDv5 of `(saga_id, correlation_id)` per ADR-0038.
CREATE TABLE IF NOT EXISTS automation_operations.processed_events (
    event_id     uuid        PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS processed_events_processed_at_idx
    ON automation_operations.processed_events (processed_at);
