-- identity-federation-service slice 4 — WebAuthn credentials + challenges.
--
-- The Rust crate stores both in Cassandra (auth_runtime.webauthn_credentials
-- + auth_runtime.webauthn_challenges). Go slice 4 keeps them in Postgres
-- until the slice-2b Cassandra wiring lands. The schema mirrors the
-- Rust DDL field-for-field so a future migration to Cassandra is a
-- mechanical move.

CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id                 UUID PRIMARY KEY,
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- credential_id is the WebAuthn public credential identifier; UNIQUE
    -- per RP because the spec requires it (deduplication on register).
    credential_id      BYTEA NOT NULL UNIQUE,
    public_key         BYTEA NOT NULL,
    sign_count         BIGINT NOT NULL DEFAULT 0,
    transports         TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    attestation_type   TEXT NOT NULL DEFAULT 'none',
    aaguid             UUID,
    label              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user
    ON webauthn_credentials (user_id);

CREATE TABLE IF NOT EXISTS webauthn_challenges (
    challenge_id  UUID PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL,             -- 'register' | 'login'
    session_data  BYTEA NOT NULL,            -- go-webauthn SessionData JSON blob
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webauthn_challenges_user
    ON webauthn_challenges (user_id);
