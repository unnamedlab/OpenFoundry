-- ADR-0037 — Foundry-pattern orchestration / Tarea 1.2.
--
-- Persistent saga state for `saga::SagaRunner`. The table stores
-- enough information to resume an interrupted saga and to keep
-- `execute_step` idempotent across retries with the same `saga_id`:
-- a step that is already listed in `completed_steps` is skipped and
-- its cached output (in `step_outputs`) is returned instead of being
-- re-executed. This matches the contract documented in the migration
-- plan ("Idempotencia: re-ejecutar saga con mismo saga_id no
-- duplica steps").
--
--   saga_id          — caller-chosen aggregate id (typically a v7 UUID)
--   name             — human-readable saga type, e.g. "create_order"
--   status           — 'running' | 'completed' | 'failed' | 'compensated' | 'aborted'
--   current_step     — name of the step currently in flight (NULL when idle)
--   completed_steps  — names of every step that has succeeded so far,
--                      in execution order. Used both for idempotency
--                      and to derive the compensation order on failure.
--   step_outputs     — JSON object keyed by step_name carrying the
--                      serialised output of each completed step. The
--                      runner reads it back when an idempotent retry
--                      "re-executes" a step that already finished.
--   failed_step      — name of the step that raised the error, if any
--   created_at /     — bookkeeping
--   updated_at
--
-- This table is owned by the `saga` crate and lives in its own
-- schema. Services that already have a `saga` schema for unrelated
-- reasons should rename the schema in their own copy of the
-- migration; the rest of the column shape is the contract.

CREATE SCHEMA IF NOT EXISTS saga;

CREATE TABLE IF NOT EXISTS saga.state (
    saga_id          uuid PRIMARY KEY,
    name             text NOT NULL,
    status           text NOT NULL DEFAULT 'running',
    current_step     text,
    completed_steps  text[] NOT NULL DEFAULT '{}',
    step_outputs     jsonb  NOT NULL DEFAULT '{}'::jsonb,
    failed_step      text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS saga_state_status_idx
    ON saga.state (status);
