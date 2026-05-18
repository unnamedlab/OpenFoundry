-- SG.33 — Service users and client-credentials governance.
--
-- Third-party applications using client_credentials execute as durable
-- service users. Their resource grants and sensitive lifecycle actions
-- are tracked separately from human owners so long-running workloads
-- can be permissioned, audited, and rotated independently.

CREATE TABLE IF NOT EXISTS third_party_service_user_grants (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  UUID NOT NULL REFERENCES third_party_applications(id) ON DELETE CASCADE,
    service_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope_type      TEXT NOT NULL CHECK (scope_type IN ('project', 'resource')),
    scope_id        TEXT NOT NULL,
    role_key        TEXT NOT NULL,
    granted_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,
    UNIQUE (application_id, service_user_id, scope_type, scope_id, role_key)
);

CREATE INDEX IF NOT EXISTS idx_third_party_service_user_grants_app
    ON third_party_service_user_grants (application_id, revoked_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_third_party_service_user_grants_service_user
    ON third_party_service_user_grants (service_user_id, revoked_at, created_at DESC);

CREATE TABLE IF NOT EXISTS third_party_service_user_audit_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  UUID NOT NULL REFERENCES third_party_applications(id) ON DELETE CASCADE,
    service_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_id        UUID REFERENCES users(id) ON DELETE SET NULL,
    action          TEXT NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_third_party_service_user_audit_app
    ON third_party_service_user_audit_events (application_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_third_party_service_user_audit_service_user
    ON third_party_service_user_audit_events (service_user_id, created_at DESC)
    WHERE service_user_id IS NOT NULL;
