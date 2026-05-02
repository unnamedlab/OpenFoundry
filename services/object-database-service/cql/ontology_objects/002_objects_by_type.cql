-- 002_objects_by_type.cql — type-tab listing (newest first)
-- Drives: handlers/objects.rs:174 list_by_type, type-tab UI,
--         domain/indexer.rs fan-out into Vespa/OpenSearch.
-- Access pattern: by (tenant, type_id), DESC by updated_at, paged.

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_type (
    tenant             text,
    type_id            text,
    updated_at         timestamp,
    object_id          timeuuid,
    owner_id           uuid,
    marking            frozen<set<text>>,
    properties_summary text,                -- denormalised projection (UI list cells)
    deleted            boolean,
    PRIMARY KEY ((tenant, type_id), updated_at, object_id)
)
WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — type-tab listing. Drives objects.rs:174 list_by_type. Mitigation hook: re-PK to ((tenant,type_id,day_bucket)) if mean partition > 40 MB.';
