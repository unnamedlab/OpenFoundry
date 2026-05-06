-- media-sets-service · retention windows.
--
-- Foundry retention contract ("Advanced media set settings.md"):
--
--   * Items become inaccessible after `retention_seconds` from
--     `created_at` (0 = retain forever).
--   * Reducing the window makes already-old items immediately
--     inaccessible.
--   * Expanding the window NEVER restores items that already expired.
--
-- Implementation notes
-- --------------------
-- We snapshot `retention_seconds` from the parent `media_sets` row at
-- INSERT time into `media_items.retention_seconds`. The companion
-- `expires_at` column then materialises the formula
--   `created_at + retention_seconds * interval '1 second'`
-- so the partial reaper index can scan it cheaply.
--
-- We can't make `expires_at` a `GENERATED ALWAYS` column because
-- PostgreSQL classifies `timestamptz + interval` as STABLE (the result
-- depends on the active timezone for month/day-bearing intervals) and
-- generation expressions must be IMMUTABLE. We therefore plug the
-- formula in via a BEFORE trigger that fires on INSERT and on any
-- UPDATE of `created_at` / `retention_seconds`. The semantics stay
-- identical to the spec's `GENERATED ALWAYS AS …`.
--
-- The reaper worker computes the **current effective** expiration by
-- JOINing each item with its parent `media_sets` row, so a PATCH that
-- reduces retention takes effect on the next reaper pass — and the
-- PATCH handler runs a one-shot reaper synchronously to satisfy the
-- "immediate" half of the contract.
--
-- Once `deleted_at` is stamped, no path can clear it back to NULL —
-- this is what gives us "expansion does not restore" for free.

ALTER TABLE media_items
    ADD COLUMN IF NOT EXISTS retention_seconds BIGINT NOT NULL DEFAULT 0;

ALTER TABLE media_items
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE OR REPLACE FUNCTION media_items_set_expires_at()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.retention_seconds > 0 THEN
        NEW.expires_at := NEW.created_at + NEW.retention_seconds * INTERVAL '1 second';
    ELSE
        NEW.expires_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS media_items_set_expires_at_trigger ON media_items;
CREATE TRIGGER media_items_set_expires_at_trigger
    BEFORE INSERT OR UPDATE OF created_at, retention_seconds ON media_items
    FOR EACH ROW EXECUTE FUNCTION media_items_set_expires_at();

-- Backfill existing rows so the snapshot reflects the current parent
-- setting. The trigger fires on the UPDATE and populates `expires_at`
-- in the same pass.
UPDATE media_items i
   SET retention_seconds = s.retention_seconds
  FROM media_sets s
 WHERE i.media_set_rid = s.rid;

-- Partial index on the live + retention-bound subset. Hot path for the
-- background reaper.
CREATE INDEX IF NOT EXISTS idx_media_items_expires_at
    ON media_items(expires_at)
    WHERE deleted_at IS NULL AND expires_at IS NOT NULL;
