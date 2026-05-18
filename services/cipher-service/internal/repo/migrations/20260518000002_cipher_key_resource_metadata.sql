-- CIP.2 cipher_key resource metadata. These columns are safe to add to
-- existing Milestone A installs because they carry non-material metadata
-- only; plaintext DEKs remain exclusively in the KMS-wrapped version rows.

ALTER TABLE cipher_keys
    ADD COLUMN IF NOT EXISTS owner_id UUID,
    ADD COLUMN IF NOT EXISTS organizations JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS markings JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS intended_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS access_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_cipher_keys_owner
    ON cipher_keys (tenant_id, owner_id);
