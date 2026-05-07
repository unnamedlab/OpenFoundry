-- identity-federation-service slice 3 — MFA TOTP table.
--
-- Subset of services/identity-federation-service/migrations/
-- 20260420000002_enterprise_auth.sql relevant to TOTP. WebAuthn lives
-- in slice 4; api_keys + sso config in slice 5/6; the wider
-- enterprise-auth schema follows in later slices.

CREATE TABLE IF NOT EXISTS user_mfa_totp (
    user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret               TEXT NOT NULL,
    recovery_code_hashes JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled              BOOLEAN NOT NULL DEFAULT false,
    verified_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
