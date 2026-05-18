-- CRW.1 — Code Repository as a first-class Compass resource.
-- Public Foundry docs describe Code Repositories as Git-backed IDE
-- resources for production code, with common Git actions, pull-request
-- collaboration, and repository types for transforms/functions/models.
-- OpenFoundry stores the Compass resource facet here; later CRW tasks own
-- Git storage, editor surfaces, builds, PRs and lineage.

ALTER TABLE repositories ADD COLUMN IF NOT EXISTS rid TEXT;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS slug TEXT;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT 'system';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS organizations TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS markings TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS language_template TEXT NOT NULL DEFAULT 'python-transform';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS storage_backend_rid TEXT NOT NULL DEFAULT 'ri.openfoundry.main.storage-backend.local';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS object_store_backend TEXT NOT NULL DEFAULT 'local';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS package_kind TEXT NOT NULL DEFAULT 'transform';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[];
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS settings JSONB NOT NULL DEFAULT '{}'::JSONB;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS compass_project_rid TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS compass_folder_rid TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS acl JSONB NOT NULL DEFAULT '{"owners":[],"editors":[],"viewers":[]}'::JSONB;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT 'system';
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS trashed_at TIMESTAMPTZ NULL;
ALTER TABLE repositories ADD COLUMN IF NOT EXISTS trashed_by TEXT NULL;

UPDATE repositories
   SET rid = COALESCE(rid, 'ri.foundry.main.coderepository.' || id::text),
       slug = COALESCE(slug, lower(regexp_replace(trim(name), '[^a-zA-Z0-9]+', '-', 'g'))),
       owner = COALESCE(NULLIF(owner, ''), created_by, 'system'),
       language_template = COALESCE(NULLIF(language_template, ''), 'python-transform'),
       storage_backend_rid = COALESCE(NULLIF(storage_backend_rid, ''), 'ri.openfoundry.main.storage-backend.' || object_store_backend),
       object_store_backend = COALESCE(NULLIF(object_store_backend, ''), 'local'),
       package_kind = COALESCE(NULLIF(package_kind, ''), 'transform'),
       settings = COALESCE(settings, '{}'::JSONB),
       acl = COALESCE(acl, '{"owners":[],"editors":[],"viewers":[]}'::JSONB);

ALTER TABLE repositories ALTER COLUMN rid SET NOT NULL;
ALTER TABLE repositories ALTER COLUMN slug SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_repositories_rid ON repositories(rid);
CREATE UNIQUE INDEX IF NOT EXISTS idx_repositories_slug ON repositories(slug);
CREATE INDEX IF NOT EXISTS idx_repositories_owner ON repositories(owner);
CREATE INDEX IF NOT EXISTS idx_repositories_organizations ON repositories USING GIN(organizations);
CREATE INDEX IF NOT EXISTS idx_repositories_markings ON repositories USING GIN(markings);
CREATE INDEX IF NOT EXISTS idx_repositories_compass_location ON repositories(compass_project_rid, compass_folder_rid);
CREATE INDEX IF NOT EXISTS idx_repositories_active_updated ON repositories(updated_at DESC) WHERE trashed_at IS NULL;

CREATE OR REPLACE FUNCTION set_repository_resource_defaults()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.rid IS NULL OR NEW.rid = '' THEN
        NEW.rid := 'ri.foundry.main.coderepository.' || NEW.id::text;
    END IF;
    IF NEW.slug IS NULL OR NEW.slug = '' THEN
        NEW.slug := lower(regexp_replace(trim(NEW.name), '[^a-zA-Z0-9]+', '-', 'g'));
    END IF;
    IF NEW.storage_backend_rid IS NULL OR NEW.storage_backend_rid = '' THEN
        NEW.storage_backend_rid := 'ri.openfoundry.main.storage-backend.' || COALESCE(NULLIF(NEW.object_store_backend, ''), 'local');
    END IF;
    IF NEW.acl IS NULL THEN
        NEW.acl := '{"owners":[],"editors":[],"viewers":[]}'::JSONB;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_repository_resource_defaults ON repositories;
CREATE TRIGGER trg_repository_resource_defaults
BEFORE INSERT OR UPDATE ON repositories
FOR EACH ROW EXECUTE FUNCTION set_repository_resource_defaults();
