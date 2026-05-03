-- media-sets-service: individual media files inside a media set.
--
-- Path-deduplication semantics ("Importing media.md"): when a new item is
-- registered for the same `(media_set_rid, branch, path)` and there is
-- already a live (non-deleted) item at that key, the application MUST:
--
--   1. Soft-delete the previous item by setting `deleted_at = NOW()`.
--   2. Insert the new item with `deduplicated_from = <previous-rid>`.
--
-- The previous item stays reachable through its immutable RID — Foundry
-- keeps overwritten media items addressable for direct media references.
-- The partial UNIQUE index below enforces "one live item per path".

CREATE TABLE IF NOT EXISTS media_items (
    rid                 TEXT        PRIMARY KEY,
    media_set_rid       TEXT        NOT NULL REFERENCES media_sets(rid) ON DELETE CASCADE,
    branch              TEXT        NOT NULL,
    -- Empty string for items written outside a transaction
    -- (TRANSACTIONLESS media sets).
    transaction_rid     TEXT        NOT NULL DEFAULT '',
    path                TEXT        NOT NULL,
    mime_type           TEXT        NOT NULL DEFAULT '',
    size_bytes          BIGINT      NOT NULL DEFAULT 0,
    -- Filled by the upload completion notification; empty until the byte
    -- payload has actually landed in the backing store.
    sha256              TEXT        NOT NULL DEFAULT '',
    metadata            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    -- Pointer into the backing object store (S3/MinIO/local), e.g.
    --   s3://media/media-sets/<rid>/<branch>/<sha256[:2]>/<sha256>
    storage_uri         TEXT        NOT NULL DEFAULT '',
    -- RID of the previous item this one overwrote via path dedup.
    deduplicated_from   TEXT,
    deleted_at          TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_media_items_set_branch
    ON media_items(media_set_rid, branch);
CREATE INDEX IF NOT EXISTS idx_media_items_sha256
    ON media_items(sha256)
    WHERE sha256 <> '';
CREATE INDEX IF NOT EXISTS idx_media_items_path_prefix
    ON media_items(media_set_rid, branch, path);

-- Foundry "one live item per (set, branch, path)" guarantee.
CREATE UNIQUE INDEX IF NOT EXISTS uq_media_items_live_path
    ON media_items(media_set_rid, branch, path)
    WHERE deleted_at IS NULL;
