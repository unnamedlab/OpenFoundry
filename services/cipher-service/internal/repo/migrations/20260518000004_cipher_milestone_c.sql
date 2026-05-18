-- CIP.19/CIP.20: cross-environment promotion metadata and KMS backend identity.
ALTER TABLE cipher_keys
    ADD COLUMN IF NOT EXISTS kms_backend TEXT NOT NULL DEFAULT 'local';

CREATE INDEX IF NOT EXISTS idx_cipher_keys_backend
    ON cipher_keys (tenant_id, kms_backend);
