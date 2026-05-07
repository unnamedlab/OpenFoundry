-- 001_objects_by_id.cql — primary object record
-- Drives: handlers/objects.rs CRUD (get/create/update/delete),
--         handlers/funnel.rs writes, handlers/bindings.rs writes,
--         handlers/actions.rs mutations.
-- Access pattern: by (tenant, object_id) — single-row partition.
-- LWT used for optimistic concurrency on revision_number.

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_id (
    tenant            text,
    object_id         timeuuid,
    type_id           text,
    owner_id          uuid,
    properties        text,                 -- canonical JSON document
    marking           frozen<set<text>>,    -- multi-marking labels
    organization_id   uuid,
    revision_number   bigint    STATIC,     -- monotonic counter, partition-static
    created_at        timestamp,
    updated_at        timestamp,
    deleted           boolean,              -- soft-delete tombstone
    PRIMARY KEY ((tenant, object_id))
)
WITH compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
}
 AND gc_grace_seconds = 86400
 AND default_time_to_live = 0
 AND comment = 'S1.1.b — primary object record. Source of truth for objects.rs CRUD.';
