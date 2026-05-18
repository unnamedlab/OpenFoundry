-- 0018: CMP.16 - per-user Compass favorites profile.
--
-- Favorites remain keyed by (user_id, resource_kind, resource_id), but the
-- user-profile projection now carries explicit display order and optional
-- groups. Because favorites can point at resources owned by many services, the
-- resource side stays intentionally non-FKed; only local favorite groups are
-- constrained.

CREATE TABLE IF NOT EXISTS user_favorite_groups (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL,
    name          TEXT NOT NULL,
    display_order INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_favorite_groups_name_nonempty CHECK (length(trim(name)) > 0),
    CONSTRAINT user_favorite_groups_name_length CHECK (length(name) <= 120),
    UNIQUE (user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_user_favorite_groups_user_order
    ON user_favorite_groups (user_id, display_order, name);

ALTER TABLE user_favorites
    ADD COLUMN IF NOT EXISTS group_id UUID NULL,
    ADD COLUMN IF NOT EXISTS display_order INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'fk_user_favorites_group'
           AND conrelid = 'user_favorites'::regclass
    ) THEN
        ALTER TABLE user_favorites
            ADD CONSTRAINT fk_user_favorites_group
            FOREIGN KEY (group_id) REFERENCES user_favorite_groups(id)
            ON DELETE SET NULL;
    END IF;
END $$;

WITH ranked AS (
    SELECT user_id,
           resource_kind,
           resource_id,
           row_number() OVER (
               PARTITION BY user_id
               ORDER BY created_at ASC, resource_kind ASC, resource_id ASC
           ) * 1000 AS next_order
      FROM user_favorites
     WHERE display_order = 0
)
UPDATE user_favorites f
   SET display_order = ranked.next_order,
       updated_at = GREATEST(f.updated_at, f.created_at)
  FROM ranked
 WHERE f.user_id = ranked.user_id
   AND f.resource_kind = ranked.resource_kind
   AND f.resource_id = ranked.resource_id;

CREATE INDEX IF NOT EXISTS idx_user_favorites_user_group_order
    ON user_favorites (user_id, group_id, display_order, created_at DESC);
