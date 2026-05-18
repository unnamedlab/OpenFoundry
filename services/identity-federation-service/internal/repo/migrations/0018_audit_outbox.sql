-- T8 compliance closure — local outbox substrate for identity-federation-service.
--
-- The SSO callback (and ACS handler) enqueues `auth.login`,
-- `auth.identity_linked` and `auth.token_issued` envelopes into
-- `outbox.events` from inside the auth tx. Debezium reads the WAL and
-- the EventRouter SMT routes by the `topic` column to
-- `audit.events.v1`, where audit-sink lands them in the
-- `of_audit.events` Iceberg table.
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

DO $$ BEGIN ALTER TABLE outbox.events REPLICA IDENTITY FULL; EXCEPTION WHEN insufficient_privilege THEN NULL; END $$;
