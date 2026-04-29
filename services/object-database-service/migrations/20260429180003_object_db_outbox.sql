-- object-database-service: Phase 1.3 – transactional outbox
-- Every mutation (insert, update, delete) writes one row here in the same
-- transaction as the state change. A background relay reads unpublished rows
-- and forwards them to NATS JetStream, then marks them published.
--
-- JetStream subjects:
--   object.upserted   — ObjectUpsertedEvent
--   object.deleted    — ObjectDeletedEvent
--   link.upserted     — LinkUpsertedEvent
--   link.deleted      — LinkDeletedEvent

CREATE TABLE IF NOT EXISTS object_db.write_outbox (
    id              UUID PRIMARY KEY,
    -- "object.upserted" | "object.deleted" | "link.upserted" | "link.deleted"
    subject         TEXT NOT NULL,
    -- Protobuf-serialised event payload (binary) or JSON fallback
    payload         BYTEA NOT NULL,
    -- Entity ID (object or link) for deduplication and tracing
    entity_id       UUID NOT NULL,
    -- Sequence monotonically increments per entity to order events
    entity_version  BIGINT NOT NULL DEFAULT 1,
    published       BOOLEAN NOT NULL DEFAULT FALSE,
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Relay reads unpublished rows ordered by creation time
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON object_db.write_outbox(published, created_at ASC)
    WHERE published = FALSE;

-- Deduplication lookups by entity
CREATE INDEX IF NOT EXISTS idx_outbox_entity
    ON object_db.write_outbox(entity_id, entity_version DESC);
