-- identity-federation-service slice 5e — SG.5 group-administration
-- schema.
--
-- Slice 6 (migration 0005) shipped the minimal groups + group_members
-- shape (id, name, description; group_id+user_id). SG.5 widens this
-- to match the public Foundry group surface:
--
--   * kind             — 'internal' | 'external' | 'rule_based'.
--                        Internal: manually managed. External: SCIM
--                        / IdP-synced (handler refuses direct member
--                        mutations). Rule-based: membership is the
--                        evaluation of `rule_query` against the user
--                        attributes graph.
--   * display_name     — human-friendly label distinct from the
--                        URL-friendly `name` handle. Backfilled from
--                        `name` for legacy rows.
--   * realm            — same vocabulary as users.realm ('local',
--                        'okta', ...). Indexed for filter queries.
--   * organization_id  — scopes group visibility to a tenancy org.
--                        NULL means platform-wide.
--   * attributes       — JSONB free-form metadata used by rule
--                        evaluation and downstream policy.
--   * rule_query       — JSONB describing the rule predicate (only
--                        meaningful when kind = 'rule_based').
--   * status           — 'active' | 'archived'. Archived groups stay
--                        in the DB for audit but vanish from default
--                        lists.
--
-- Plus three new tables:
--
--   group_admins              — admin grants per group (manage,
--                               manage_members).
--   group_nested_members      — parent → member group edges.
--   (group_members.expires_at — added below; existing PK preserved.)
--
-- Schema is additive.

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS kind            TEXT NOT NULL DEFAULT 'internal',
    ADD COLUMN IF NOT EXISTS display_name    TEXT NULL,
    ADD COLUMN IF NOT EXISTS realm           TEXT NOT NULL DEFAULT 'local',
    ADD COLUMN IF NOT EXISTS organization_id UUID NULL,
    ADD COLUMN IF NOT EXISTS attributes      JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS rule_query      JSONB NULL,
    ADD COLUMN IF NOT EXISTS status          TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE groups SET display_name = name WHERE display_name IS NULL;

ALTER TABLE groups
    ADD CONSTRAINT groups_kind_check
        CHECK (kind IN ('internal', 'external', 'rule_based')) NOT VALID;

ALTER TABLE groups VALIDATE CONSTRAINT groups_kind_check;

CREATE INDEX IF NOT EXISTS groups_kind_idx ON groups (kind);
CREATE INDEX IF NOT EXISTS groups_realm_idx ON groups (realm);
CREATE INDEX IF NOT EXISTS groups_org_idx ON groups (organization_id);
CREATE INDEX IF NOT EXISTS groups_status_idx ON groups (status);

ALTER TABLE group_members
    ADD COLUMN IF NOT EXISTS added_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS added_by   UUID NULL,
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NULL;

CREATE INDEX IF NOT EXISTS group_members_expires_idx
    ON group_members (expires_at)
    WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS group_admins (
    group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
    scope      TEXT NOT NULL DEFAULT 'manage',
    granted_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id, scope),
    CONSTRAINT group_admins_scope_check
        CHECK (scope IN ('manage', 'manage_members'))
);

CREATE INDEX IF NOT EXISTS group_admins_user_idx ON group_admins (user_id);

CREATE TABLE IF NOT EXISTS group_nested_members (
    parent_group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    member_group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    added_by        UUID NULL,
    PRIMARY KEY (parent_group_id, member_group_id),
    CONSTRAINT group_nested_members_no_self
        CHECK (parent_group_id <> member_group_id)
);

CREATE INDEX IF NOT EXISTS group_nested_members_member_idx
    ON group_nested_members (member_group_id);
