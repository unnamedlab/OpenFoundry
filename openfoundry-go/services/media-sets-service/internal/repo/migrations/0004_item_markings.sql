-- media-sets-service · per-item markings (H3 — Cedar granular policies).
--
-- Foundry contract ("Configure granular policies for media items.md"):
--
--   By default, an item inherits every marking from its parent media
--   set; the application enforces that strictly by *unioning* the set's
--   markings into the item's effective set before any `media_item::*`
--   Cedar check.
--
--   Per-item markings are an *additive* override: storing a stricter
--   marking on a single item (e.g. `SECRET` on top of a `PII` set)
--   tightens access to that item without affecting siblings.
--
-- Today this column is opt-in: items keep an empty array and inherit
-- the set markings 1:1. Future "stop inheriting" knobs (Foundry's
-- per-object override) can extend the row with a `removed_inherited`
-- column; the partial-inherit semantic is out of scope for H3.

ALTER TABLE media_items
    ADD COLUMN IF NOT EXISTS markings TEXT[] NOT NULL DEFAULT '{}';
