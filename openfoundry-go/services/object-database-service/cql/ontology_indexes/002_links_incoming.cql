-- 002_links_incoming.cql — target-side traversal index
-- Drives: handlers/links.rs target reads, domain/traversal.rs inbound walk.
-- Access pattern: (tenant, target_id) [+ link_type prefix].
-- Mitigation hook: if hub targets accumulate > 100 k incoming links,
-- re-PK to ((tenant, target_id, month_bucket)) via flag-flip migration.

CREATE TABLE IF NOT EXISTS ontology_indexes.links_incoming (
    tenant      text,
    target_id   timeuuid,
    link_type   text,
    source_id   timeuuid,
    source_type text,
    properties  text,
    created_at  timestamp,
    PRIMARY KEY ((tenant, target_id), link_type, source_id)
)
WITH CLUSTERING ORDER BY (link_type ASC, source_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — inbound link index.';
