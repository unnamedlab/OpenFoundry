-- T2.1 — dataset_branches v2 + fallback chain.
--
-- Refines the v1 schema introduced by
--   services/data-asset-catalog-service/migrations/20260421174000_dataset_branches.sql
-- and the dataset-versioning-service initial migration
--   20260501000001_versioning_init.sql
--
-- Foundry semantics enforced here:
--
--   * Every branch carries the textual `dataset_rid`
--     (`ri.foundry.main.dataset.<uuid>`) alongside the internal `dataset_id`
--     so the table is queryable from any service that only has the public
--     RID at hand. Backfilled from `datasets.rid`.
--   * `parent_branch_id IS NULL` ⇔ root branch (the doc-default `master`).
--     Multiple roots per dataset are forbidden by a partial UNIQUE index so
--     no dataset can end up with two competing trunks.
--   * Soft-delete via `deleted_at`: history of branches is retained for
--     audit (`Datasets.md` § "Branching guarantees"), the active set is
--     `deleted_at IS NULL`. The existing partial unique index on
--     `(dataset_id, name)` is replaced with a partial one that ignores
--     tombstoned branches so a name can be re-used after deletion.
--   * `created_by` traces who minted the branch (audit).
--   * Fallback chain (`dataset_branch_fallbacks`) used by
--     `pipeline-build-service::resolve_input_dataset` to walk
--     `feature → develop → master` when a branch is missing on an input
--     dataset.

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS dataset_rid TEXT,
    ADD COLUMN IF NOT EXISTS created_by  UUID,
    ADD COLUMN IF NOT EXISTS deleted_at  TIMESTAMPTZ;

-- One-shot backfill of `dataset_rid` from `datasets.rid` (no-op for rows
-- created after this migration: callers are expected to set it on insert).
UPDATE dataset_branches b
   SET dataset_rid = d.rid
  FROM datasets d
 WHERE b.dataset_id = d.id
   AND b.dataset_rid IS NULL;

ALTER TABLE dataset_branches
    ALTER COLUMN dataset_rid SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dataset_branches_dataset_rid
    ON dataset_branches(dataset_rid);

-- Replace the existing UNIQUE(dataset_id, name) (created in
-- 20260421174000_dataset_branches.sql with implicit name
-- `dataset_branches_dataset_id_name_key`) with a partial unique index that
-- excludes tombstoned rows. We keep a second partial unique on
-- `(dataset_rid, name)` to support RID-only callers (matches the spec).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
         WHERE conname = 'dataset_branches_dataset_id_name_key'
    ) THEN
        ALTER TABLE dataset_branches
            DROP CONSTRAINT dataset_branches_dataset_id_name_key;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_branches_dataset_id_name_active
    ON dataset_branches(dataset_id, name)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_branches_dataset_rid_name_active
    ON dataset_branches(dataset_rid, name)
    WHERE deleted_at IS NULL;

-- Exactly one root (parent IS NULL) per active dataset. Multiple historic
-- roots are allowed (deleted_at IS NOT NULL) so renames don't break.
CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_branches_one_active_root
    ON dataset_branches(dataset_id)
    WHERE parent_branch_id IS NULL AND deleted_at IS NULL;

-- ──────────────────────────────────────────────────────────────────────────
-- Fallback chain (T2.3): per-branch ordered list of branches to fall back
-- to when an input dataset doesn't have the requested branch.
--
-- Example for a `feature/bookings-v2` build branch:
--   position 0 → develop
--   position 1 → master
--
-- `position` is 0-indexed; lower wins.
-- ──────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS dataset_branch_fallbacks (
    branch_id            UUID NOT NULL
                              REFERENCES dataset_branches(id) ON DELETE CASCADE,
    position             INT  NOT NULL CHECK (position >= 0),
    fallback_branch_name TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (branch_id, position),
    -- Logical (not FK) reference: the fallback may resolve to different
    -- branches on different datasets (`develop` on dataset A, `master` on
    -- dataset B), so we keep the name and resolve at build time.
    CONSTRAINT chk_fallback_name_nonempty CHECK (length(trim(fallback_branch_name)) > 0)
);

CREATE INDEX IF NOT EXISTS idx_dataset_branch_fallbacks_branch
    ON dataset_branch_fallbacks(branch_id, position);
