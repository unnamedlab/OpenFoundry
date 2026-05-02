# Archived migrations — `object-database-service`

These DDL files used to live at
`services/object-database-service/migrations/`. They define the
**CQRS write-path** (revisions + outbox), the **type→dataset
binding** primitive ("Models in the Ontology") and the **traversal
+ FTS** support that backed graph queries on Postgres.

## Tables (3 migrations, 5 tables + 1 alter + 1 SQL function)

### `20260429120000_write_path_tables.sql`

* `object_revisions` — append-only audit log for every mutation on
  `object_instances` (one row per write). Hot, append-only.
* `link_revisions` — append-only audit log for every mutation on
  `link_instances`. Hot, append-only.
* `write_outbox` — transactional outbox feeding NATS JetStream
  without coupling the write tx to the broker. Hot, ephemeral
  (rows deleted post-publish; the WAL conserves them for Debezium).

### `20260501090000_object_type_bindings.sql`

* `object_type_bindings` — declares that an `ObjectType` is
  materialised from one or more datasets (sync_mode =
  `snapshot|incremental|view`). Definition-shaped, rarely written.

### `20260501150000_traversal_and_fulltext.sql`

* Adds `searchable_text` (`tsvector`) + GIN index to
  `object_revisions` and `object_instances`.
* Composite indexes on `link_instances` for the recursive multi-hop
  traversal CTE (`traverse_neighbors`).
* Adds `marking` column to `link_instances`.
* SQL function `ontology_jsonb_searchable_text(properties JSONB)`.

## S1.7 split (hot vs declarative)

* **Hot → Cassandra**:
  - `object_revisions` → reuses the schema laid down in S1.1.b under
    `ontology_objects.objects_by_id` (the existing Cassandra row
    carries `revision_number`; the per-write audit log lives as
    `ontology_objects.object_revisions` keyed by
    `(tenant_id, object_id), revision_number DESC` — to be added in
    a follow-up CQL migration).
  - `link_revisions` → analogous CF in `ontology_indexes`.
  - `write_outbox` → **stays in Postgres** (`pg-policy.outbox`); this
    is the durable transaction-coordination point for Debezium and is
    explicitly out of scope for Cassandra (decision sealed in
    ADR-0020 and ADR-0024).
* **Declarative → `pg-schemas.ontology_schema`**:
  - `object_type_bindings` (low-cardinality config of dataset→type
    materialisation; written by ontology authors).
* **Search projections → `libs/search-abstraction`** (Vespa /
  OpenSearch per ADR-0024):
  - `searchable_text` columns + GIN indexes are dropped after S1.5
    indexer drains; the SQL function
    `ontology_jsonb_searchable_text` is preserved here as the
    canonical reference for the indexer's payload normaliser.
  - The `traverse_neighbors` recursive CTE is replaced by Cassandra
    `links_outgoing` / `links_incoming` (S1.1.b) page-walks driven
    from the kernel.

## Why archived

The CQRS write path collapses onto `apply_object_with_outbox`
(`libs/ontology-kernel/src/domain/writeback.rs`, S1.4.c) and the
Cassandra `objects_by_id` revision counter; bindings move to
`pg-schemas`; FTS/traversal move to the search abstraction. The
service binary is substrate-only and no longer applies
`sqlx::migrate!`.

These files remain the canonical source for the S1.7 data-migration
tooling and for incident-response schema reference.
