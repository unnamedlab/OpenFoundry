-- P4 — Branch retention + markings inheritance.
--
-- Mirrors the Foundry "Branch retention" doc (Developer toolchain →
-- Global Branching → Usage → Branch retention) and the "Branch
-- security" doc (markings parent → child snapshot semantics).

-- ────────────────────────────────────────────────────────────────────
-- Retention: per-branch policy + archived_at flag.
--
-- `retention_policy` is one of:
--   * INHERITED — walks up parent_branch_id until it finds a branch
--                 with FOREVER or TTL_DAYS. The default for new
--                 branches; matches the doc default "branches inherit
--                 retention from their parent".
--   * FOREVER   — never archived. Used by `master` and any other
--                 long-lived trunk.
--   * TTL_DAYS  — archived once `last_activity_at < now - ttl_days`
--                 AND no open transaction AND no children (which
--                 are reparented to the grandparent on archive).
-- ────────────────────────────────────────────────────────────────────

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS retention_policy TEXT NOT NULL DEFAULT 'INHERITED'
        CHECK (retention_policy IN ('INHERITED', 'FOREVER', 'TTL_DAYS')),
    ADD COLUMN IF NOT EXISTS retention_ttl_days INT NULL,
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS archive_grace_until TIMESTAMPTZ NULL;

-- Master always defaults to FOREVER; back-fill to keep prod-like data
-- compatible. New branches go through `INHERITED` and resolve at
-- archive time.
UPDATE dataset_branches
   SET retention_policy = 'FOREVER'
 WHERE name = 'master' AND retention_policy = 'INHERITED';

-- Active branches are those with `archived_at IS NULL AND deleted_at IS NULL`.
-- The archive worker uses this index to scan eligible branches
-- without touching the partial unique index on (dataset_id, name).
CREATE INDEX IF NOT EXISTS idx_dataset_branches_retention_scan
    ON dataset_branches (retention_policy, last_activity_at)
    WHERE archived_at IS NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_dataset_branches_archived_at
    ON dataset_branches (archived_at)
    WHERE archived_at IS NOT NULL;

-- ────────────────────────────────────────────────────────────────────
-- Markings inheritance snapshot.
--
-- Foundry "Branch security": when a child branch is created, the
-- markings the *parent* carries at that moment are copied into
-- `branch_markings_snapshot` as `source = PARENT`. New markings added
-- to the parent AFTER the child was created do **not** propagate
-- (deliberate snapshot semantics — see the "Best practices and
-- technical details" doc).
--
-- Markings the user adds directly on the child carry `source =
-- EXPLICIT` and stack on top of the inherited floor.
-- ────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS branch_markings_snapshot (
    branch_id   UUID NOT NULL
                  REFERENCES dataset_branches(id) ON DELETE CASCADE,
    marking_id  UUID NOT NULL,
    source      TEXT NOT NULL CHECK (source IN ('PARENT', 'EXPLICIT')),
    set_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    set_by      UUID NULL,
    PRIMARY KEY (branch_id, marking_id)
);

CREATE INDEX IF NOT EXISTS idx_branch_markings_snapshot_marking
    ON branch_markings_snapshot (marking_id);

-- ────────────────────────────────────────────────────────────────────
-- Outbox schema (Debezium router target). Idempotent — the table is
-- normally provisioned by `libs/outbox/migrations/0001_outbox_events.sql`
-- but each service that publishes to it must apply the migration too,
-- so testcontainer harnesses can boot in isolation.
-- ────────────────────────────────────────────────────────────────────

CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     UUID PRIMARY KEY,
    aggregate    TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    payload      JSONB NOT NULL,
    headers      JSONB NOT NULL DEFAULT '{}'::jsonb,
    topic        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
