-- CRW.2 — Git storage backend for Code Repositories.
-- Each Code Repository receives a managed bare Git repository. The service
-- exposes Smart HTTP clone/fetch/push URLs authenticated with OIDC JWTs; SSH
-- URLs are persisted only when the deployment enables the optional feature.

ALTER TABLE repositories ADD COLUMN IF NOT EXISTS git_storage_path TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS git_http_url TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS git_ssh_url TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS git_ssh_enabled BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_repositories_git_storage_path
    ON repositories(git_storage_path)
    WHERE git_storage_path <> '';
