-- FASE 5 / Tarea 5.3 — outbox + per-batch idempotency for the
-- `workflow-automation-service` Foundry-pattern runtime.
--
-- Adds the two cross-cutting tables every Foundry-pattern consumer
-- needs (per ADR-0022 outbox + ADR-0038 idempotency):
--
--   * `outbox.events` — transactional outbox table that the
--     condition handler (HTTP) and the condition consumer both write
--     into so the `automate.outcome.v1` (and future) Kafka publishes
--     happen in the same database transaction as the state-machine
--     write. Captured by the per-cluster Debezium Postgres connector
--     and routed to Kafka via the EventRouter SMT.
--
--   * `workflow_automation.processed_events` — single-column
--     primary-key table that backs `idempotency::PgIdempotencyStore`.
--     The condition consumer records the inbound condition's
--     deterministic `event_id` here BEFORE side-effecting (per
--     ADR-0038 record-before-process), so a Kafka redelivery after a
--     crash skips the dispatch instead of re-issuing the effect call.
--
-- These tables are scoped to this bounded context's Postgres cluster
-- (today: `workflow-automation-pg`). The pattern is per-bounded-
-- context and never shared across services — `outbox.events` rows
-- are owned by the producer service and Debezium captures each
-- service's WAL stream independently.

-- ──────────────────────────── Outbox table ────────────────────────────

CREATE SCHEMA IF NOT EXISTS outbox;

-- The minimal Debezium-outbox shape (matches `libs/outbox::OutboxEvent`):
--   event_id     — UUID primary key, deterministic per-aggregate
--                  (UUIDv5 of `aggregate || aggregate_id || version`)
--                  so writer retries collapse onto the same row.
--   aggregate    — owning aggregate name (e.g. `automation_run`).
--   aggregate_id — opaque string id of the aggregate row.
--   payload      — JSON event body (becomes the Kafka record value).
--   headers      — JSON object of `key → string` headers (copied to
--                  Kafka headers by the EventRouter SMT — used for
--                  OpenLineage `ol-*` propagation).
--   topic        — destination Kafka topic (used by the SMT's
--                  `route.by.field=topic` configuration).
--   created_at   — operator-facing timestamp; not consumed by the SMT.
CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     uuid        PRIMARY KEY,
    aggregate    text        NOT NULL,
    aggregate_id text        NOT NULL,
    payload      jsonb       NOT NULL,
    headers      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    topic        text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- REPLICA IDENTITY FULL is required so the WAL record carries the
-- full row payload (Debezium needs every column to render the
-- Kafka record body). The libs/outbox helper INSERTs and
-- immediately DELETEs in the same transaction; without FULL the
-- DELETE WAL entry would only carry the primary key, which is
-- enough for tombstones but not for the EventRouter SMT's payload
-- routing on the original INSERT. Setting it once at table creation
-- avoids per-row alterations later.
ALTER TABLE outbox.events REPLICA IDENTITY FULL;

-- Operator query: "show me events queued in the last minute" — the
-- outbox table is steady-state empty (INSERT+DELETE in same TX) so
-- a row showing here either means a Debezium outage or a producer
-- bug. The index keeps the diagnostic query cheap.
CREATE INDEX IF NOT EXISTS outbox_events_created_at_idx
    ON outbox.events (created_at);

-- ─────────────────────────── Idempotency table ────────────────────────

-- Mirrors `libs/idempotency::PgIdempotencyStore` contract: a single
-- `event_id uuid PRIMARY KEY` column plus an operator-facing
-- `processed_at timestamptz`. Lives in this service's bounded-
-- context schema so the role that writes to `automation_runs` does
-- not need cross-schema privileges.
CREATE TABLE IF NOT EXISTS workflow_automation.processed_events (
    event_id     uuid        PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);

-- Retention sweep: rows older than the longest plausible Kafka
-- redelivery window (14 days, matching the DLQ retention on
-- `__dlq.automate.condition.v1`) are no longer protective and can
-- be GC'd by an offline job. Index supports that sweep cheaply.
CREATE INDEX IF NOT EXISTS processed_events_processed_at_idx
    ON workflow_automation.processed_events (processed_at);
