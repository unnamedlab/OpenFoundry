-- 001_objects_by_id.cql — OSV2 primary object record
-- Drives: object-database-service CRUD, Action writeback, and OSV2 point reads.
-- Access pattern: per ontology tenant + object type + primary-key hash bucket.
-- OSV2.1: partition by object type, then 64-way primary-key hash bucket to
-- avoid hot tablets for fat object types. OSV2.2: hot row uses typed blobs keyed
-- by property id; property names are not stored in the hot row.

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_id (
    tenant             text,
    type_id            text,
    primary_key_hash   tinyint,
    object_id          timeuuid,
    rid                text,
    primary_key        text,
    owner_id           uuid,
    properties_blob    blob,                -- OSV2 typed property blob keyed by property id
    markings_blob      blob,                -- OSV2 markings envelope
    organizations      uuid,
    revision_number    bigint,
    created_at         timestamp,
    updated_at         timestamp,
    last_updated       timestamp,
    last_updater       text,
    deleted            boolean,
    PRIMARY KEY ((tenant, type_id, primary_key_hash), object_id)
)
WITH compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
}
 AND gc_grace_seconds = 86400
 AND default_time_to_live = 0
 AND comment = 'OSV2.1/OSV2.2 — partition by tenant,type,primary-key hash; typed property/marking blobs.';

CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_id_by_rid (
    tenant             text,
    object_id          timeuuid,
    type_id            text,
    primary_key_hash   tinyint,
    rid                text,
    primary_key        text,
    owner_id           uuid,
    properties_blob    blob,
    markings_blob      blob,
    organizations      uuid,
    revision_number    bigint,
    created_at         timestamp,
    updated_at         timestamp,
    last_updated       timestamp,
    last_updater       text,
    deleted            boolean,
    PRIMARY KEY ((tenant, object_id))
)
WITH compaction = {
    'class': 'LeveledCompactionStrategy',
    'sstable_size_in_mb': 160
}
 AND gc_grace_seconds = 86400
 AND default_time_to_live = 0
 AND comment = 'OSV2.2 — point-read mirror by RID/object_id for Get(type, primary_key) compatibility.';
