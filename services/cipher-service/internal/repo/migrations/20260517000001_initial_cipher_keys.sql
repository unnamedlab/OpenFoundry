-- cipher-service Milestone A: tenant-scoped key registry.
--
-- Two tables:
--   * cipher_keys           — the registry row (alias, algorithm,
--                             status, currently active version).
--   * cipher_key_versions   — one row per historical wrapping. The
--                             DEK is stored as KMS.Wrap(DEK); the
--                             plaintext is never persisted.
--
-- (alias, tenant_id) is unique so an operator can refer to a key by
-- its human name without collisions across tenants. Lookups in
-- handlers always go through (tenant_id, id) so a stolen id from
-- tenant A cannot resolve against tenant B.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS cipher_keys (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL,
    alias       TEXT NOT NULL,
    algorithm   TEXT NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at  TIMESTAMPTZ,
    CONSTRAINT cipher_keys_alias_per_tenant UNIQUE (tenant_id, alias),
    CONSTRAINT cipher_keys_version_positive CHECK (version >= 1)
);

CREATE INDEX IF NOT EXISTS idx_cipher_keys_tenant
    ON cipher_keys (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cipher_key_versions (
    key_id               UUID NOT NULL REFERENCES cipher_keys(id) ON DELETE CASCADE,
    version              INTEGER NOT NULL,
    wrapped_key_material BYTEA NOT NULL,
    kms_key_ref          TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    activated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at           TIMESTAMPTZ,
    PRIMARY KEY (key_id, version)
);
