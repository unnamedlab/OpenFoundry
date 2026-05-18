-- 002_objects_by_type.cql — OSV2 type listing / object-type partition fan-out
-- Access pattern: tenant + type, fan out over primary_key_hash buckets for hot
-- object types; newest first within each bucket.

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_type (
    tenant             text,
    type_id            text,
    primary_key_hash   tinyint,
    updated_at         timestamp,
    object_id          timeuuid,
    owner_id           uuid,
    markings_blob      blob,
    properties_summary text,                -- denormalised non-hot projection for UI list cells
    deleted            boolean,
    PRIMARY KEY ((tenant, type_id, primary_key_hash), updated_at, object_id)
)
WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'OSV2.1 — type-tab listing bucketed by primary-key hash to prevent hot tablets.';
