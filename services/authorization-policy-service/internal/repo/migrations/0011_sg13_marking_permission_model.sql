-- 0011: SG.13 - Marking permission model and direct resource markings.
--
-- This slice turns SG.12 marking permission rows into the operational
-- checks used when applying/removing markings from protected resources.
-- Manage/apply/remove/member stay distinct; only member grants satisfy
-- access to marked data.

CREATE TABLE IF NOT EXISTS resource_markings (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NULL,
    resource_kind TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    marking_id    UUID NOT NULL REFERENCES markings(id) ON DELETE RESTRICT,
    source_kind   TEXT NOT NULL DEFAULT 'direct',
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    applied_by    UUID NOT NULL,
    applied_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT resource_markings_source_kind_check
        CHECK (source_kind IN ('direct')),
    CONSTRAINT resource_markings_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS resource_markings_tenant_unique
    ON resource_markings (tenant_id, resource_kind, resource_id, marking_id)
    WHERE tenant_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS resource_markings_global_unique
    ON resource_markings (resource_kind, resource_id, marking_id)
    WHERE tenant_id IS NULL;

CREATE INDEX IF NOT EXISTS resource_markings_resource_idx
    ON resource_markings (resource_kind, resource_id);

CREATE INDEX IF NOT EXISTS resource_markings_marking_idx
    ON resource_markings (marking_id);

CREATE TABLE IF NOT EXISTS resource_marking_audit_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID NULL,
    resource_kind  TEXT NOT NULL,
    resource_id    TEXT NOT NULL,
    marking_id     UUID NOT NULL REFERENCES markings(id) ON DELETE RESTRICT,
    actor_id       UUID NOT NULL,
    action         TEXT NOT NULL,
    before_state   JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_state    JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT resource_marking_audit_action_check
        CHECK (action IN (
            'resource_marking.applied',
            'resource_marking.apply_denied',
            'resource_marking.removed',
            'resource_marking.remove_denied'
        )),
    CONSTRAINT resource_marking_audit_before_object_check
        CHECK (jsonb_typeof(before_state) = 'object'),
    CONSTRAINT resource_marking_audit_after_object_check
        CHECK (jsonb_typeof(after_state) = 'object'),
    CONSTRAINT resource_marking_audit_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS resource_marking_audit_events_resource_idx
    ON resource_marking_audit_events (resource_kind, resource_id, created_at DESC);

CREATE INDEX IF NOT EXISTS resource_marking_audit_events_marking_idx
    ON resource_marking_audit_events (marking_id, created_at DESC);
