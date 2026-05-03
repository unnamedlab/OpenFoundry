CREATE TABLE IF NOT EXISTS jwks_keys (
    kid               TEXT PRIMARY KEY,
    kty               TEXT NOT NULL DEFAULT 'RSA',
    public_pem        TEXT NOT NULL,
    vault_key_name    TEXT NOT NULL,
    vault_key_version INTEGER NOT NULL CHECK (vault_key_version > 0),
    status            TEXT NOT NULL CHECK (status IN ('active', 'grace', 'retired')),
    activated_at      TIMESTAMPTZ NOT NULL,
    grace_started_at  TIMESTAMPTZ,
    retire_after      TIMESTAMPTZ,
    retired_at        TIMESTAMPTZ,
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_jwks_keys_one_active
    ON jwks_keys ((status))
    WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS idx_jwks_keys_vault_version
    ON jwks_keys (vault_key_name, vault_key_version);
