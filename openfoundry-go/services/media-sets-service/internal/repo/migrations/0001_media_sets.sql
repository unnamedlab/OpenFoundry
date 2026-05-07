-- media-sets-service: Foundry-style media set definitions, branches and
-- transactional write batches.
--
-- A media set is a Foundry-RID-addressed collection of unstructured media
-- files (image/audio/video/document/spreadsheet/email) that share a common
-- schema. Branches are git-style isolation boundaries; transactions seal
-- a batch of writes atomically when the set's policy is TRANSACTIONAL.
--
-- RID convention:
--   * media set:    ri.foundry.main.media_set.<uuid>
--   * media item:   ri.foundry.main.media_item.<uuid>
--   * transaction:  ri.foundry.main.media_transaction.<uuid>

CREATE TABLE IF NOT EXISTS media_sets (
    rid                 TEXT        PRIMARY KEY,
    project_rid         TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    schema              TEXT        NOT NULL CHECK (schema IN
                            ('IMAGE','AUDIO','VIDEO','DOCUMENT','SPREADSHEET','EMAIL')),
    allowed_mime_types  TEXT[]      NOT NULL DEFAULT '{}',
    transaction_policy  TEXT        NOT NULL DEFAULT 'TRANSACTIONLESS'
                            CHECK (transaction_policy IN ('TRANSACTIONLESS','TRANSACTIONAL')),
    -- 0 = retain forever (matches Foundry "Advanced media set settings").
    retention_seconds   BIGINT      NOT NULL DEFAULT 0,
    virtual             BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Only populated when `virtual = TRUE`; points to the external source.
    source_rid          TEXT,
    markings            TEXT[]      NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_media_sets_project ON media_sets(project_rid);
CREATE INDEX IF NOT EXISTS idx_media_sets_schema  ON media_sets(schema);

-- Branches: git-style isolation. Every media set boots with an implicit
-- `main` branch (created by the application on first write) — advanced
-- branching (reparent, fork-from-transaction, etc.) is deferred to H4.
CREATE TABLE IF NOT EXISTS media_set_branches (
    media_set_rid       TEXT NOT NULL REFERENCES media_sets(rid) ON DELETE CASCADE,
    branch_name         TEXT NOT NULL,
    parent_branch       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (media_set_rid, branch_name)
);

-- Transactional write batches. Only meaningful when the parent media set
-- has `transaction_policy = TRANSACTIONAL`; transactionless sets bypass
-- this table entirely and write items directly with `transaction_rid = ''`.
--
-- Foundry invariant ("Advanced media set settings.md"): only one OPEN
-- transaction per (media_set, branch). Enforced via partial unique index.
CREATE TABLE IF NOT EXISTS media_set_transactions (
    rid             TEXT        PRIMARY KEY,
    media_set_rid   TEXT        NOT NULL REFERENCES media_sets(rid) ON DELETE CASCADE,
    branch          TEXT        NOT NULL,
    state           TEXT        NOT NULL DEFAULT 'OPEN'
                        CHECK (state IN ('OPEN','COMMITTED','ABORTED')),
    opened_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ,
    opened_by       TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_media_set_transactions_set_branch
    ON media_set_transactions(media_set_rid, branch, opened_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_media_set_transactions_one_open_per_branch
    ON media_set_transactions(media_set_rid, branch)
    WHERE state = 'OPEN';
