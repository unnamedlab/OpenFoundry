-- SG.32 — Third-party application enablement, consent, and OAuth token runtime.
--
-- SG.31 stores durable client registration. This migration adds the
-- authorization-code, consent, and refresh-token state needed to
-- enforce organization enablement, CSRF state echoing, PKCE checks,
-- scope narrowing, token rotation, and revocation.

CREATE TABLE IF NOT EXISTS third_party_oauth_authorization_codes (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash             TEXT NOT NULL UNIQUE,
    application_id         UUID NOT NULL REFERENCES third_party_applications(id) ON DELETE CASCADE,
    client_id              TEXT NOT NULL,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id        UUID NOT NULL,
    redirect_uri           TEXT NOT NULL,
    state                  TEXT NOT NULL,
    code_challenge         TEXT NOT NULL,
    code_challenge_method  TEXT NOT NULL DEFAULT 'S256',
    requested_scopes       TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    granted_scopes         TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at             TIMESTAMPTZ NOT NULL,
    consumed_at            TIMESTAMPTZ,
    revoked_at             TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_third_party_oauth_codes_app_user
    ON third_party_oauth_authorization_codes (application_id, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_third_party_oauth_codes_expires
    ON third_party_oauth_authorization_codes (expires_at)
    WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS third_party_oauth_refresh_tokens (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash       TEXT NOT NULL UNIQUE,
    family_id        UUID NOT NULL,
    application_id   UUID NOT NULL REFERENCES third_party_applications(id) ON DELETE CASCADE,
    client_id        TEXT NOT NULL,
    subject_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id  UUID NOT NULL,
    scopes           TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL,
    used_at          TIMESTAMPTZ,
    revoked_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_third_party_oauth_refresh_subject
    ON third_party_oauth_refresh_tokens (subject_user_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_third_party_oauth_refresh_family
    ON third_party_oauth_refresh_tokens (family_id);
CREATE INDEX IF NOT EXISTS idx_third_party_oauth_refresh_app
    ON third_party_oauth_refresh_tokens (application_id, organization_id)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS third_party_oauth_consents (
    application_id   UUID NOT NULL REFERENCES third_party_applications(id) ON DELETE CASCADE,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id  UUID NOT NULL,
    scopes           TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    consented_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at       TIMESTAMPTZ,
    PRIMARY KEY (application_id, user_id, organization_id)
);

CREATE INDEX IF NOT EXISTS idx_third_party_oauth_consents_user
    ON third_party_oauth_consents (user_id, consented_at DESC);
