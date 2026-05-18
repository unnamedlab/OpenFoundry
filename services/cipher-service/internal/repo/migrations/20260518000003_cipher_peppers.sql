-- CIP.14 pepper registry for irreversible tokenization.
CREATE TABLE IF NOT EXISTS cipher_peppers (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    name TEXT NOT NULL,
    algorithm TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    access_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    rotated_at TIMESTAMPTZ,
    CONSTRAINT cipher_peppers_name_per_tenant UNIQUE (tenant_id, name),
    CONSTRAINT cipher_peppers_version_positive CHECK (version >= 1)
);

CREATE INDEX IF NOT EXISTS idx_cipher_peppers_tenant
    ON cipher_peppers (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cipher_pepper_versions (
    pepper_id UUID NOT NULL REFERENCES cipher_peppers(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    wrapped_pepper_material BYTEA NOT NULL,
    kms_key_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pepper_id, version)
);
