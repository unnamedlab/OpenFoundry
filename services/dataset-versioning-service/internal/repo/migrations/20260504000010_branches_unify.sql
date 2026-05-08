-- P1 — unify the public Branch model + Foundry guarantees.
--
-- Aligns `dataset_branches` with `proto/dataset/branch.proto`:
--
--   * `rid TEXT` — branch RID (`ri.foundry.main.branch.<uuid>`),
--     stored-generated from `id` so callers never have to mint it.
--   * `created_from_transaction_id` — the transaction the branch was
--     forked off when minted via `source.from_transaction_rid`.
--     Distinct from `head_transaction_id`, which moves on every commit.
--   * `last_activity_at` — bumped by trigger on every transaction
--     INSERT for this branch (sortable "recent activity" lists).
--   * `labels JSONB` — free-form metadata (persona, ticket, …).
--   * `fallback_chain TEXT[]` — denormalised cache of
--     `dataset_branch_fallbacks` so the proto/Branch payload is a
--     single SELECT.
--
-- Foundry guarantees re-asserted here (see "Dataset branch guarantees"
-- in /docs/foundry/data-integration/branching/):
--
--   * Every non-root branch has exactly one parent. The XOR
--     "parent_branch_id IS NULL ⇔ is_root" is *derived* (`is_root` is
--     not stored), so no CHECK is needed; `parent_branch_id` is the
--     single source of truth and the partial unique
--     `uq_dataset_branches_one_active_root` (introduced in
--     20260502000001) enforces "one root per active dataset".
--   * "One open transaction per branch" is preserved by
--     `uq_dataset_transactions_one_open_per_branch`.

-- ──────────────────────────────────────────────────────────────────────────
-- New columns.
-- ──────────────────────────────────────────────────────────────────────────

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS rid TEXT
        GENERATED ALWAYS AS ('ri.foundry.main.branch.' || id::text) STORED;

CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_branches_rid
    ON dataset_branches(rid);

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS created_from_transaction_id UUID NULL
        REFERENCES dataset_transactions(id) ON DELETE SET NULL;

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS labels JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE dataset_branches
    ADD COLUMN IF NOT EXISTS fallback_chain TEXT[] NOT NULL DEFAULT '{}';

-- Backfill `fallback_chain` from the existing `dataset_branch_fallbacks`
-- table so the cache lines up with the source of truth at migration time.
-- New writes go through `RuntimeStore::replace_fallbacks`, which keeps
-- both surfaces in sync.
UPDATE dataset_branches AS b
   SET fallback_chain = COALESCE(f.chain, '{}')
  FROM (
        SELECT branch_id,
               ARRAY_AGG(fallback_branch_name ORDER BY position ASC) AS chain
          FROM dataset_branch_fallbacks
         GROUP BY branch_id
       ) AS f
 WHERE b.id = f.branch_id;

-- ──────────────────────────────────────────────────────────────────────────
-- Trigger: bump `last_activity_at` on every transaction insert. We use
-- AFTER INSERT so the txn row is observable before the timestamp moves.
-- ──────────────────────────────────────────────────────────────────────────

CREATE OR REPLACE FUNCTION dataset_branches_touch_last_activity()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE dataset_branches
       SET last_activity_at = NOW()
     WHERE id = NEW.branch_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tr_dataset_branches_touch_last_activity
    ON dataset_transactions;

CREATE TRIGGER tr_dataset_branches_touch_last_activity
    AFTER INSERT ON dataset_transactions
    FOR EACH ROW EXECUTE FUNCTION dataset_branches_touch_last_activity();
