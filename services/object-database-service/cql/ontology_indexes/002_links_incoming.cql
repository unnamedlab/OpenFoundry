-- 002_links_incoming.cql — OSV2 target-side traversal index
-- Access pattern: (tenant, link_type_id, target_rid) [+ source_rid cursor].

CREATE TABLE IF NOT EXISTS ontology_indexes.links_incoming (
    tenant          text,
    link_type_id    text,
    target_rid      timeuuid,
    source_rid      timeuuid,
    source_type     text,
    properties_blob blob,
    markings_blob   blob,
    created_at      timestamp,
    PRIMARY KEY ((tenant, link_type_id, target_rid), source_rid)
)
WITH CLUSTERING ORDER BY (source_rid ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'OSV2.5 — inbound link index keyed by (link_type_id, target_rid).';
