-- 004_objects_by_marking.cql — marking enforcement / governance scans
-- Drives: marking-based scans (audit, mass re-classification).
-- Access pattern: by (tenant, marking_id), paged by object_id.
-- Mitigation hook: re-PK to ((tenant,marking_id,created_day))
-- when a marking exceeds 100 k rows (e.g. "PUBLIC").

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_marking (
    tenant      text,
    marking_id  text,
    object_id   timeuuid,
    type_id     text,
    owner_id    uuid,
    updated_at  timestamp,
    deleted     boolean,
    PRIMARY KEY ((tenant, marking_id), object_id)
)
WITH CLUSTERING ORDER BY (object_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — marking-scoped index for governance scans.';
