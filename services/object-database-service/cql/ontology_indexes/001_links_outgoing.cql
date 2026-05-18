-- 001_links_outgoing.cql — OSV2 source-side traversal index
-- Access pattern: (tenant, link_type_id, source_rid) [+ target_rid cursor].

CREATE TABLE IF NOT EXISTS ontology_indexes.links_outgoing (
    tenant          text,
    link_type_id    text,
    source_rid      timeuuid,
    target_rid      timeuuid,
    target_type     text,
    properties_blob blob,
    markings_blob   blob,
    created_at      timestamp,
    PRIMARY KEY ((tenant, link_type_id, source_rid), target_rid)
)
WITH CLUSTERING ORDER BY (target_rid ASC)
 AND compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
 }
 AND gc_grace_seconds = 86400
 AND comment = 'OSV2.5 — outbound link index keyed by (link_type_id, source_rid).';
