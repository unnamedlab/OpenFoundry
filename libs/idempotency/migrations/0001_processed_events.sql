-- ADR-0038 — consumer idempotency.
--
-- Stores one row per `event_id` that a consumer has accepted for
-- processing. The atomic `INSERT … ON CONFLICT DO NOTHING RETURNING`
-- in `PgIdempotencyStore::check_and_record` makes "is this a
-- duplicate?" a single round-trip with no race window between the
-- check and the record.
--
-- The schema and table name are chosen by the operator at deploy
-- time. This file is the canonical shape; if you want a per-service
-- table just substitute the names. The crate's
-- `tests/postgres_it.rs` provisions `idem.processed_events`.
--
-- Retention: there is no built-in TTL on Postgres. Pair this with a
-- nightly `DELETE … WHERE processed_at < now() - <retention>` job;
-- the retention window MUST be greater than or equal to the source
-- Kafka topic retention, otherwise an event that gets re-delivered
-- after the row was pruned will be reprocessed.

CREATE SCHEMA IF NOT EXISTS idem;

CREATE TABLE IF NOT EXISTS idem.processed_events (
    event_id     uuid        PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);

-- Index supports the retention-pruning sweep above.
CREATE INDEX IF NOT EXISTS processed_events_processed_at_idx
    ON idem.processed_events (processed_at);
