-- Phase 1 (B3 Workspace): explicit "shared with user/group" entries.
--
-- This is the cross-resource sharing table that backs both the
-- "Shared with me" and "Shared by me" tabs in the workspace UI.
-- Project- and folder-level RBAC continues to live in the ontology
-- service; this table only models *direct* shares of a single
-- resource with a single principal.
--
-- Exactly one of (shared_with_user_id, shared_with_group_id) is set.

CREATE TABLE IF NOT EXISTS resource_shares (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_kind         TEXT NOT NULL,
    resource_id           UUID NOT NULL,
    shared_with_user_id   UUID NULL,
    shared_with_group_id  UUID NULL,
    sharer_id             UUID NOT NULL,
    access_level          TEXT NOT NULL CHECK (access_level IN ('viewer', 'editor', 'owner')),
    note                  TEXT NOT NULL DEFAULT '',
    expires_at            TIMESTAMPTZ NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT resource_shares_principal
        CHECK ((shared_with_user_id IS NOT NULL) <> (shared_with_group_id IS NOT NULL))
);

-- A given (resource, principal) pair can only have one active share row.
CREATE UNIQUE INDEX IF NOT EXISTS idx_resource_shares_user
    ON resource_shares (resource_kind, resource_id, shared_with_user_id)
    WHERE shared_with_user_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_resource_shares_group
    ON resource_shares (resource_kind, resource_id, shared_with_group_id)
    WHERE shared_with_group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_resource_shares_recipient_user
    ON resource_shares (shared_with_user_id, created_at DESC)
    WHERE shared_with_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_resource_shares_sharer
    ON resource_shares (sharer_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_resource_shares_expiry
    ON resource_shares (expires_at) WHERE expires_at IS NOT NULL;
