-- SG.31 — OAuth2 third-party application registration.
--
-- Registration metadata is durable in identity-federation-service. The
-- OAuth authorization/consent runtime is tracked by SG.32; this slice
-- owns app registration, client credentials, service-user creation,
-- owner metadata, discovery, and organization enablement.

CREATE TABLE IF NOT EXISTS third_party_applications (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id                     TEXT NOT NULL UNIQUE,
    name                          TEXT NOT NULL,
    description                   TEXT,
    logo_url                      TEXT,
    client_type                   TEXT NOT NULL CHECK (client_type IN ('confidential', 'public')),
    enabled_grant_types           TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    redirect_uris                 TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    scopes                        TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    owner_user_ids                UUID[] NOT NULL DEFAULT '{}'::UUID[],
    managing_organization_id      UUID NOT NULL,
    discoverable_organization_ids UUID[] NOT NULL DEFAULT '{}'::UUID[],
    service_user_id               UUID REFERENCES users(id) ON DELETE SET NULL,
    client_secret_hash            TEXT,
    client_secret_prefix          TEXT,
    client_secret_created_at      TIMESTAMPTZ,
    preferred_management_surface  TEXT NOT NULL DEFAULT 'developer_console'
                                  CHECK (preferred_management_surface IN ('developer_console', 'control_panel_fallback')),
    control_panel_fallback        BOOLEAN NOT NULL DEFAULT TRUE,
    created_by                    UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by                    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at                    TIMESTAMPTZ,
    CHECK (
      (client_type = 'confidential')
      OR NOT ('client_credentials' = ANY(enabled_grant_types))
    ),
    CHECK (
      NOT ('authorization_code' = ANY(enabled_grant_types))
      OR array_length(redirect_uris, 1) IS NOT NULL
    ),
    CHECK (
      (client_type = 'confidential' AND client_secret_hash IS NOT NULL)
      OR (client_type = 'public' AND client_secret_hash IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_third_party_applications_managing_org
    ON third_party_applications (managing_organization_id)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_third_party_applications_service_user
    ON third_party_applications (service_user_id)
    WHERE service_user_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS third_party_application_enablements (
    application_id       UUID NOT NULL REFERENCES third_party_applications(id) ON DELETE CASCADE,
    organization_id      UUID NOT NULL,
    enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    project_resource_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    marking_ids          TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    organization_consent BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by           UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (application_id, organization_id)
);

CREATE INDEX IF NOT EXISTS idx_third_party_application_enablements_org
    ON third_party_application_enablements (organization_id, enabled);
