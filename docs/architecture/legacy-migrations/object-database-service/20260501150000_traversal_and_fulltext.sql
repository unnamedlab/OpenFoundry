-- Full-text search & multi-hop traversal support for ontology data.
--
-- 1. Adds a tsvector `searchable_text` column to `object_revisions` and to
--    the live `object_instances` table, both backed by GIN indexes. The
--    column is recomputed on every write through a trigger so the indexer
--    can rely on Postgres FTS (`ts_rank_cd` ≈ BM25 ranking) without an
--    out-of-band batch job.
-- 2. Adds composite indexes on `link_instances` to make the recursive
--    multi-hop traversal CTE (`traverse_neighbors`) fast in both directions.
-- 3. Adds a marking column to `link_instances` (mirroring the one on
--    `object_instances`) so the traversal can apply marking-based filters
--    without a join.

-- ---------------------------------------------------------------------------
-- Helper to flatten a JSONB object's leaf string/number values into a single
-- whitespace-separated text. Used by the searchable_text trigger.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION ontology_jsonb_searchable_text(properties JSONB)
RETURNS TEXT
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    result TEXT;
BEGIN
    IF properties IS NULL OR jsonb_typeof(properties) <> 'object' THEN
        RETURN '';
    END IF;
    SELECT string_agg(value, ' ')
    INTO result
    FROM (
        SELECT
            CASE jsonb_typeof(value)
                WHEN 'string' THEN trim(both '"' FROM value::text)
                WHEN 'number' THEN value::text
                WHEN 'boolean' THEN value::text
                ELSE value::text
            END AS value
        FROM jsonb_each(properties)
    ) AS expanded;
    RETURN COALESCE(result, '');
END;
$$;

-- ---------------------------------------------------------------------------
-- object_instances.searchable_text
-- ---------------------------------------------------------------------------
ALTER TABLE object_instances
    ADD COLUMN IF NOT EXISTS searchable_text TSVECTOR;

CREATE OR REPLACE FUNCTION object_instances_refresh_searchable_text()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.searchable_text :=
        setweight(to_tsvector('simple', COALESCE(NEW.id::text, '')), 'A') ||
        setweight(to_tsvector('simple', COALESCE(ontology_jsonb_searchable_text(NEW.properties), '')), 'B') ||
        setweight(to_tsvector('simple', COALESCE(NEW.marking, '')), 'D');
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_object_instances_searchable_text ON object_instances;
CREATE TRIGGER trg_object_instances_searchable_text
    BEFORE INSERT OR UPDATE OF properties, marking, id
    ON object_instances
    FOR EACH ROW
    EXECUTE FUNCTION object_instances_refresh_searchable_text();

-- Backfill existing rows.
UPDATE object_instances
SET searchable_text =
    setweight(to_tsvector('simple', COALESCE(id::text, '')), 'A') ||
    setweight(to_tsvector('simple', COALESCE(ontology_jsonb_searchable_text(properties), '')), 'B') ||
    setweight(to_tsvector('simple', COALESCE(marking, '')), 'D')
WHERE searchable_text IS NULL;

CREATE INDEX IF NOT EXISTS idx_object_instances_searchable_text
    ON object_instances USING GIN (searchable_text);

-- ---------------------------------------------------------------------------
-- object_revisions.searchable_text
-- ---------------------------------------------------------------------------
ALTER TABLE object_revisions
    ADD COLUMN IF NOT EXISTS searchable_text TSVECTOR;

CREATE OR REPLACE FUNCTION object_revisions_refresh_searchable_text()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.searchable_text :=
        setweight(to_tsvector('simple', COALESCE(NEW.object_id::text, '')), 'A') ||
        setweight(to_tsvector('simple', COALESCE(ontology_jsonb_searchable_text(NEW.properties), '')), 'B') ||
        setweight(to_tsvector('simple', COALESCE(NEW.marking, '')), 'D');
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_object_revisions_searchable_text ON object_revisions;
CREATE TRIGGER trg_object_revisions_searchable_text
    BEFORE INSERT OR UPDATE OF properties, marking
    ON object_revisions
    FOR EACH ROW
    EXECUTE FUNCTION object_revisions_refresh_searchable_text();

UPDATE object_revisions
SET searchable_text =
    setweight(to_tsvector('simple', COALESCE(object_id::text, '')), 'A') ||
    setweight(to_tsvector('simple', COALESCE(ontology_jsonb_searchable_text(properties), '')), 'B') ||
    setweight(to_tsvector('simple', COALESCE(marking, '')), 'D')
WHERE searchable_text IS NULL;

CREATE INDEX IF NOT EXISTS idx_object_revisions_searchable_text
    ON object_revisions USING GIN (searchable_text);

-- ---------------------------------------------------------------------------
-- link_instances.marking + traversal indexes
-- ---------------------------------------------------------------------------
ALTER TABLE link_instances
    ADD COLUMN IF NOT EXISTS marking TEXT NOT NULL DEFAULT 'public';

-- Composite indexes used by the recursive traversal CTE in
-- `ontology_kernel::handlers::links::traverse`. Forward direction goes from
-- (source -> target) filtered by link_type_id; reverse direction goes from
-- (target -> source).
CREATE INDEX IF NOT EXISTS idx_link_instances_source_type
    ON link_instances (source_object_id, link_type_id);

CREATE INDEX IF NOT EXISTS idx_link_instances_target_type
    ON link_instances (target_object_id, link_type_id);

CREATE INDEX IF NOT EXISTS idx_link_instances_marking
    ON link_instances (marking);
