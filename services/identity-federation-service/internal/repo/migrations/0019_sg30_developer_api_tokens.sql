-- SG.30 — developer API token governance.
--
-- User-generated tokens remain opaque to clients: only key_hash is
-- persisted. Governance metadata captures the visible prefix,
-- explicit scope/permission snapshots, and the non-production warning
-- shown at creation/list time.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS prefix TEXT,
    ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD COLUMN IF NOT EXISTS permissions_snapshot TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD COLUMN IF NOT EXISTS roles_snapshot TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    ADD COLUMN IF NOT EXISTS warning TEXT NOT NULL DEFAULT 'Developer API tokens inherit your current permissions, are temporary, and must not be used in production applications or committed to shared or public repositories. Store them in environment variables during development and revoke them when no longer needed.';

UPDATE api_keys
SET prefix = 'ofapikey_' || LEFT(REPLACE(id::TEXT, '-', ''), 12)
WHERE prefix IS NULL;

ALTER TABLE api_keys
    ALTER COLUMN prefix SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys (prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_active_user
    ON api_keys (user_id, revoked_at, expires_at);
