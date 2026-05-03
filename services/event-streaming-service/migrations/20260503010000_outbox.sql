-- S4.1 — local outbox substrate for `event-streaming-service`.
--
-- The service emits control-plane lifecycle events through the shared
-- `outbox.events` table in the same database transaction as the primary
-- metadata write. Debezium reads the WAL and routes by `topic`.

CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    event_id    uuid PRIMARY KEY,
    aggregate   text NOT NULL,
    aggregate_id text NOT NULL,
    payload     jsonb NOT NULL,
    headers     jsonb NOT NULL DEFAULT '{}'::jsonb,
    topic       text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS outbox.heartbeat (
    id           text PRIMARY KEY,
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE outbox.events REPLICA IDENTITY FULL;
