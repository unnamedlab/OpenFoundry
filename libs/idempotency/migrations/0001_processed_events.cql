-- ADR-0038 — consumer idempotency, Cassandra flavour.
--
-- Stores one row per `event_id` that a consumer has accepted for
-- processing. `CassandraIdempotencyStore::check_and_record` uses an
-- LWT `INSERT … IF NOT EXISTS` (LOCAL_SERIAL) so the read-modify-
-- write is atomic across replicas in the local DC.
--
-- TTL semantics: rows expire after 30 days (`default_time_to_live =
-- 2592000`). This MUST be ≥ the source Kafka topic's retention,
-- otherwise an event re-delivered after its idempotency row has
-- expired will be reprocessed. Bump the TTL via
-- `ALTER TABLE … WITH default_time_to_live = …` if your topic
-- retention is longer.

CREATE KEYSPACE IF NOT EXISTS idem
    WITH replication = {'class': 'NetworkTopologyStrategy', 'datacenter1': 1};

CREATE TABLE IF NOT EXISTS idem.processed_events (
    event_id     uuid PRIMARY KEY,
    processed_at timestamp
) WITH default_time_to_live = 2592000;
