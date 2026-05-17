-- identity-federation-service slice — JWT signing-key rotation (S3.1.c).
--
-- Holds the durable RSA keypairs the service signs access / refresh
-- tokens with. The private half is stored AES-256-GCM-sealed with the
-- environment-supplied JWT_SIGNING_SEALING_KEY (hex-32 bytes). The
-- public half is stored in PEM so the /.well-known/jwks.json publisher
-- can rebuild the JWK without holding the private key in memory.
--
-- Status lifecycle:
--   active   — current signer. Exactly one row at a time.
--   retiring — previous signer kept in JWKS for a short grace window
--              so in-flight tokens stay verifiable.
--   retired  — signer dropped from JWKS. Rows are kept for audit but
--              never returned by the publisher / verifier.

CREATE TABLE IF NOT EXISTS jwt_signing_keys (
    kid              TEXT PRIMARY KEY,
    algorithm        TEXT NOT NULL,
    public_key_pem   TEXT NOT NULL,
    private_key_enc  BYTEA NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_before       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_after        TIMESTAMPTZ NOT NULL,
    status           TEXT NOT NULL CHECK (status IN ('active', 'retiring', 'retired'))
);

CREATE INDEX IF NOT EXISTS jwt_signing_keys_status_idx
    ON jwt_signing_keys (status, not_after DESC);
