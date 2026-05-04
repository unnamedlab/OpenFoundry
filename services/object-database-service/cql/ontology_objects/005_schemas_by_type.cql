-- 005_schemas_by_type.cql — runtime JSON Schema versions
-- Owner: object-database-service
-- Drives: storage_abstraction::repositories::SchemaStore.
--
-- Boundary: this is not the declarative ontology catalog. Object type,
-- link type, action type, interface and project definitions remain in
-- pg-schemas.ontology_schema through ontology-definition-service. These
-- rows contain only versioned JSON Schema documents used by runtime object
-- validation.

CREATE TABLE IF NOT EXISTS ontology_objects.schemas_by_type (
    type_id     text,
    version     int,
    json_schema text,
    created_at  timestamp,
    PRIMARY KEY ((type_id), version)
)
WITH CLUSTERING ORDER BY (version DESC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'S1.4-S1.6 — versioned runtime JSON Schema payloads by object type.';

CREATE TABLE IF NOT EXISTS ontology_objects.schemas_latest (
    type_id     text PRIMARY KEY,
    version     int,
    json_schema text,
    created_at  timestamp
)
WITH compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
}
 AND gc_grace_seconds = 86400
 AND comment = 'S1.4-S1.6 — latest runtime JSON Schema pointer by object type.';
