ALTER TABLE users
    ADD COLUMN IF NOT EXISTS scim_external_id TEXT;

UPDATE users
SET scim_external_id = attributes #>> '{scim,externalId}'
WHERE scim_external_id IS NULL
  AND attributes #>> '{scim,externalId}' IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_scim_external_id
    ON users (scim_external_id)
    WHERE scim_external_id IS NOT NULL;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS scim_external_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_scim_external_id
    ON groups (scim_external_id)
    WHERE scim_external_id IS NOT NULL;
