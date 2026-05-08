-- identity-federation-service slice 7a — restricted views (CBAC).
--
-- Mirrors services/identity-federation-service/migrations/
-- 20260426030000_markings_cbac_restricted_views.sql, restricted_views
-- table only. Slice 7b lands the wider control_panel + ABAC engine
-- + scoped session admin.

-- Permissions gain a description column (prior migrations didn't have it).
ALTER TABLE permissions ADD COLUMN IF NOT EXISTS description TEXT;

CREATE TABLE IF NOT EXISTS restricted_views (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT NOT NULL UNIQUE,
    description           TEXT,
    resource              TEXT NOT NULL,
    action                TEXT NOT NULL,
    conditions            JSONB NOT NULL DEFAULT '{}'::jsonb,
    row_filter            TEXT,
    hidden_columns        JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_org_ids       JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_markings      JSONB NOT NULL DEFAULT '[]'::jsonb,
    consumer_mode_enabled BOOLEAN NOT NULL DEFAULT false,
    allow_guest_access    BOOLEAN NOT NULL DEFAULT false,
    enabled               BOOLEAN NOT NULL DEFAULT true,
    created_by            UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_restricted_views_lookup
    ON restricted_views (resource, action, enabled, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_restricted_views_conditions
    ON restricted_views USING GIN (conditions);
