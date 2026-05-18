-- 003_object_property_index.cql — OSV2 per-type/per-property LSM index
-- Supports exact match, range scans and IN-list fan-out over value_key.
-- Null-aware semantics: null values are indexed with value_kind='null',
-- value_key='null', null_value=true.

CREATE TABLE IF NOT EXISTS ontology_indexes.object_property_index (
    tenant           text,
    type_id          text,
    property_id      text,
    primary_key_hash tinyint,
    value_kind       text,
    value_key        text,
    null_value       boolean,
    object_id        timeuuid,
    updated_at       timestamp,
    PRIMARY KEY ((tenant, type_id, property_id, primary_key_hash), value_key, object_id)
)
WITH CLUSTERING ORDER BY (value_key ASC, object_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'OSV2.4 — property index per type/property/hash bucket for eq/range/IN predicates.';
