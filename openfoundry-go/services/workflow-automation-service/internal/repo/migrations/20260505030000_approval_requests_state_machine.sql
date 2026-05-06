-- FASE 7 / Tarea 7.2 — `approval_requests` state machine + outbox +
-- idempotency for the Foundry-pattern runtime of `approvals-service`.
--
-- Three tables every Foundry-pattern state-machine consumer needs
-- (per ADR-0022 outbox + ADR-0037 + ADR-0038 idempotency):
--
--   * `audit_compliance.approval_requests` — state machine row
--     consumed by `state_machine::PgStore::<ApprovalRequest>`. The
--     bounded-context schema (`audit_compliance`) is fixed by the
--     migration plan §7.2; a row's lifecycle is
--         pending → approved | rejected | expired | escalated
--     The `expires_at TIMESTAMPTZ` column lets the timeout sweep
--     CronJob (Tarea 7.4) drive the `pending → expired` transition.
--
--   * `outbox.events` — transactional outbox the HTTP handler
--     (open / decide / cancel) and the timeout sweep both write
--     into so the `approval.*.v1` Kafka publishes happen in the
--     same Postgres transaction as the row update. Captured by
--     the per-cluster Debezium Postgres connector. Per-bounded-
--     context outbox tables are independent (ADR-0022 §"per-
--     service outbox").
--
--   * `audit_compliance.processed_events` — single-column primary-
--     key dedup table backing `idempotency::PgIdempotencyStore`.
--     The future approval-decision Kafka consumer records inbound
--     event_ids here BEFORE side-effecting (ADR-0038 record-before-
--     process). Tarea 7.3 wires the consumer; this migration is
--     schema only.
--
-- The `audit_compliance` schema and the `svc_audit_compliance` role
-- are pre-created by the pg-policy bootstrap
-- (`infra/helm/infra/postgres-clusters/templates/clusters/`
-- `pg-policy-bootstrap-sql.yaml`). When the service DSN points at
-- the legacy per-service cluster the schema is created on the fly
-- here so the migration is portable across both topologies.

CREATE SCHEMA IF NOT EXISTS audit_compliance;
CREATE SCHEMA IF NOT EXISTS outbox;

-- ──────────────────────────── Outbox table ────────────────────────────

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

-- ───────────────────────── Approval state machine ─────────────────────

-- One row per approval request. Mirrors the
-- `libs/state-machine/migrations/0001_state_machine_template.sql`
-- shape: the seven standard columns (id, state, state_data, version,
-- expires_at, created_at, updated_at) plus the projected columns
-- `tenant_id`, `subject`, `assigned_to`, `decided_by` so dashboards
-- and operator queries do not have to crack open `state_data`.
--
-- State enum:
--   pending     — open; waiting for a decision or for the timeout
--                 sweep to fire.
--   approved    — terminal happy-path success.
--   rejected    — terminal happy-path failure (caller chose to
--                 reject).
--   expired     — terminal failure driven by the timeout sweep
--                 CronJob (Tarea 7.4) when expires_at <= now() and
--                 no decision arrived.
--   escalated   — RESERVED for the time-based-escalation kind in
--                 the migration plan §7.1 taxonomy. No code
--                 emits this state today; the column accepts it
--                 so a future caller does not need a schema
--                 migration when the escalation flow ships.
CREATE TABLE IF NOT EXISTS audit_compliance.approval_requests (
    -- Caller-supplied UUID (typically the Temporal workflow id from
    -- the legacy substrate) so cross-cluster joins through the
    -- audit ledger keep the same identity.
    id              uuid        PRIMARY KEY,

    -- Owning tenant — projected for dashboards and per-tenant
    -- audit exports. The state_data JSON also carries the value
    -- but querying through the column is faster.
    tenant_id       text        NOT NULL,

    -- Operator-facing summary, indexed for full-text-ish lookups.
    -- Mirrors the Temporal input's `subject` field.
    subject         text        NOT NULL,

    -- Single-approver semantics today. NULL means the approval is
    -- unassigned (any approver in the approver_set inside
    -- state_data may decide). The migration plan §7.1 reserves
    -- multi-approver and threshold-based kinds; their semantics
    -- live inside state_data.quorum (introduce when a real caller
    -- needs them — see inventory §5).
    assigned_to     uuid,

    -- Stamped on the row when `state` transitions to a terminal
    -- value. NULL while pending.
    decided_by      uuid,

    -- Short string rendering of the current state. Indexed for
    -- timeout sweep + ad-hoc operator queries. The CHECK constraint
    -- enforces the same enum the Rust code accepts; adding a new
    -- state requires both a schema migration AND a code change.
    state           text        NOT NULL CHECK (state IN (
                        'pending',
                        'approved',
                        'rejected',
                        'expired',
                        'escalated'
                    )),

    -- Full ApprovalRequest aggregate serialised as JSON (per
    -- libs/state-machine PgStore contract). Fields:
    --   request_id, tenant_id, subject, approver_set,
    --   action_payload, decided_by?, decided_at?, comment?,
    --   correlation_id, expires_at?, attempts, last_error?,
    --   quorum?: { required: int, decisions: [...] }   (future)
    state_data      jsonb       NOT NULL,

    -- Optimistic concurrency token. Bumped by every
    -- `PgStore::apply`. Two consumer replicas racing on the same
    -- row produce at most one successful UPDATE; the loser sees
    -- `StoreError::Stale` and reloads.
    version         bigint      NOT NULL DEFAULT 1 CHECK (version > 0),

    -- The deadline the timeout sweep CronJob enforces. NULL means
    -- "no deadline" — those rows never expire. Set per-row at
    -- insert time from a per-tenant policy column (or the request
    -- payload), replacing the legacy worker's hard-coded 24h
    -- timer.
    expires_at      timestamptz,

    -- End-to-end audit correlation id propagated from the inbound
    -- request span. Same UUID flows on the `approval.*.v1` outbox
    -- event headers and on the `x-audit-correlation-id` HTTP
    -- header of every effect call.
    correlation_id  uuid        NOT NULL,

    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Restart-time recovery + dashboard query: every approval still
-- pending, ordered by deadline so the timeout sweep can scan from
-- the oldest first.
CREATE INDEX IF NOT EXISTS approval_requests_pending_expires_at_idx
    ON audit_compliance.approval_requests (expires_at)
    WHERE state = 'pending' AND expires_at IS NOT NULL;

-- Per-tenant operator query: "what is currently pending for this
-- tenant?".
CREATE INDEX IF NOT EXISTS approval_requests_tenant_state_idx
    ON audit_compliance.approval_requests (tenant_id, state, created_at DESC);

-- Per-assignee query: "what does this user need to decide?".
CREATE INDEX IF NOT EXISTS approval_requests_assigned_pending_idx
    ON audit_compliance.approval_requests (assigned_to, created_at DESC)
    WHERE state = 'pending' AND assigned_to IS NOT NULL;

-- Audit / lineage lookup by correlation id (single row in the
-- common case; supports cross-run trace stitching).
CREATE INDEX IF NOT EXISTS approval_requests_correlation_idx
    ON audit_compliance.approval_requests (correlation_id);

-- ─────────────────────────── Idempotency table ────────────────────────

-- Mirrors `libs/idempotency::PgIdempotencyStore` contract. Used by
-- the future Kafka decision consumer (Tarea 7.3 leaves it deferred;
-- the table exists so the consumer can wire in without another
-- schema migration when a real producer of `approval.decided.v1`
-- shows up).
CREATE TABLE IF NOT EXISTS audit_compliance.processed_events (
    event_id     uuid        PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS processed_events_processed_at_idx
    ON audit_compliance.processed_events (processed_at);
