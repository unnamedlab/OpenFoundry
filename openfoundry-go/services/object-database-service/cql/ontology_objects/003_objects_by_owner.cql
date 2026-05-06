-- 003_objects_by_owner.cql — my-objects index
-- Drives: future Workshop "my objects" tab. Empty at MVP;
--         dual-write activates with consumer surface (open item #2).
-- Access pattern: by (tenant, owner_id), optional type_id prefix.

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_owner (
    tenant      text,
    owner_id    uuid,
    type_id     text,
    object_id   timeuuid,
    updated_at  timestamp,
    deleted     boolean,
    PRIMARY KEY ((tenant, owner_id), type_id, object_id)
)
WITH CLUSTERING ORDER BY (type_id ASC, object_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — owner-scoped object index.';
