-- Adds per-property classification marking metadata.
--
-- The column is the conjunctive marking allowlist a caller must hold to
-- read a single property at query time. An empty array (the default)
-- preserves the previous behaviour: no per-property mask, only the
-- object-level marking check gates access.
--
-- Consumed by services/ontology-query-service: the read handlers fetch
-- the latest schema from SchemaStore and redact properties whose
-- required_markings the caller does not satisfy. The write path is
-- untouched.

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS required_markings JSONB NOT NULL DEFAULT '[]'::jsonb;
