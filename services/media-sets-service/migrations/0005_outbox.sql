-- H3 closure — local outbox substrate for `media-sets-service`.
--
-- Each handler enqueues an `audit_trail::events::AuditEnvelope` into
-- `outbox.events` inside the same SQL transaction as the primary write
-- (per ADR-0022). Debezium reads the WAL and the EventRouter SMT
-- routes by the `topic` column to `audit.events.v1`, where audit-sink
-- consumes the records into the `of_audit.events` Iceberg table.
--
-- Schema mirrors the canonical layout owned by `libs/outbox` so the
-- helper writes the same columns regardless of which service hosts
-- the table.

CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     uuid PRIMARY KEY,
    aggregate    text NOT NULL,
    aggregate_id text NOT NULL,
    payload      jsonb NOT NULL,
    headers      jsonb NOT NULL DEFAULT '{}'::jsonb,
    topic        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE outbox.events REPLICA IDENTITY FULL;
