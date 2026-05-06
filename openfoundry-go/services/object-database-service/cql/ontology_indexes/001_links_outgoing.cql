-- 001_links_outgoing.cql — source-side traversal index
-- Drives: handlers/links.rs source reads, domain/traversal.rs outbound walk.
-- Access pattern: (tenant, source_id) [+ link_type prefix].

CREATE TABLE IF NOT EXISTS ontology_indexes.links_outgoing (
    tenant      text,
    source_id   timeuuid,
    link_type   text,
    target_id   timeuuid,
    target_type text,
    properties  text,                       -- canonical JSON, optional
    created_at  timestamp,
    PRIMARY KEY ((tenant, source_id), link_type, target_id)
)
WITH CLUSTERING ORDER BY (link_type ASC, target_id ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — outbound link index.';
