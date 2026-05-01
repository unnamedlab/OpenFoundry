-- CDC ingestion checkpoints. One row per (subscription_id) where the
-- subscription is the logical mapping between an upstream Postgres
-- replication slot and the downstream event-streaming topic the worker
-- publishes to.
--
-- Mirrors the shape of `cdc_metadata_service::IncrementalCheckpoint` so the
-- two services can be merged later without a schema migration. We keep a
-- copy local to the ingestion plane so the worker can resume after a crash
-- without depending on cdc-metadata-service being reachable.
CREATE TABLE IF NOT EXISTS ingestion_checkpoints (
    subscription_id UUID PRIMARY KEY,
    slot_name TEXT NOT NULL,
    publication_name TEXT NOT NULL,
    last_lsn TEXT NULL,
    last_event_at TIMESTAMPTZ NULL,
    records_observed BIGINT NOT NULL DEFAULT 0,
    records_applied BIGINT NOT NULL DEFAULT 0,
    last_tx_id BIGINT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_checkpoints_slot
    ON ingestion_checkpoints (slot_name);
