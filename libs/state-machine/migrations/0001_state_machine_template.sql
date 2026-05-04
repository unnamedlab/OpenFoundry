-- ADR-0037 — Foundry-pattern orchestration / Tarea 1.1.
--
-- Template migration for any service-owned state machine table that
-- uses `state_machine::PgStore`. Service migrations should COPY this
-- file, replace `__table__` with the concrete table name (for example
-- `approvals.requests` or `automation.runs`) and adjust the schema
-- name. The PgStore implementation only assumes the column set and
-- types declared here.
--
-- Conventions:
--   * `id`         — aggregate identifier, surfaced by
--                    `StateMachine::aggregate_id`.
--   * `state`      — short string identifying the current state. The
--                    PgStore writes whatever `StateMachine::state_str`
--                    returns. Indexed for `timeout_sweep` and ad-hoc
--                    operator queries.
--   * `state_data` — full machine serialised as JSON. PgStore uses
--                    `serde_json` to round-trip the implementor.
--   * `version`    — optimistic concurrency token. Each successful
--                    `apply` bumps it by one via
--                    `UPDATE … WHERE id = $1 AND version = $2`.
--   * `expires_at` — optional timeout deadline. `timeout_sweep` claims
--                    rows where `expires_at <= now()` and feeds them
--                    to the caller for the timeout transition.
--   * `created_at` / `updated_at` — operator-facing audit fields,
--                    maintained by PgStore on insert / apply.
--
-- The partial index on `expires_at` keeps `timeout_sweep` cheap even
-- when the bulk of rows have no deadline (terminal states, async
-- flows that completed naturally, etc.).

CREATE TABLE IF NOT EXISTS __table__ (
    id          uuid PRIMARY KEY,
    state       text NOT NULL,
    state_data  jsonb NOT NULL,
    version     bigint NOT NULL DEFAULT 1,
    expires_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS __table___state_idx
    ON __table__ (state);

CREATE INDEX IF NOT EXISTS __table___expires_at_idx
    ON __table__ (expires_at)
    WHERE expires_at IS NOT NULL;
