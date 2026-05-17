# Foundry Object Storage V2 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Object Storage V2:
the indexed backend for ontology objects and links that powers OQL
pushdown, server-side traversal, spatial queries, time-series property
queries, and writeback from Actions and Functions. Covers physical layout,
index types (property, full-text, vector, spatial, temporal, link),
write path with staged edits + atomic commits, read path with permission
and marking filters, sub-second p95 for property+link queries on millions
of objects, change subscriptions, snapshot/restore, branched indices,
and integrations with the indexer pipeline, ontology query service,
Vertex traversal, Map spatial queries, Workshop runtime, Functions
runtime, and OSDK.

> **Scope distinction.** This checklist covers the **storage and index**
> layer that the Ontology and Vertex and Map products call into. It
> does **not** redefine the ontology model itself (owned by
> [foundry-ontology-manager-object-views-1to1-checklist.md](./foundry-ontology-manager-object-views-1to1-checklist.md))
> or the dataset/transaction model (owned by
> [foundry-data-foundation-1to1-checklist.md](./foundry-data-foundation-1to1-checklist.md)).
> It is the engine that makes OQL fast enough to be a credible product.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets, or any
non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `todo` | Not implemented or not yet verified in OpenFoundry. |
| `partial` | Some surface exists, but behavior is incomplete or not wired end-to-end. |
| `blocked` | Requires a platform dependency, public documentation, or product decision. |
| `done` | Implemented, tested, documented, and verified through UI or API smoke tests. |

## Priority vocabulary

| Priority | Meaning |
| --- | --- |
| `P0` | Required for credible OQL: property index, link index, exact-match and range predicates, paginated reads with permission filter, write path from indexer pipeline and from Actions. |
| `P1` | Required for Foundry-style parity: full-text + vector indices, spatial indices (R-tree / H3), time-series property store, branched indices, change subscriptions, snapshot/restore. |
| `P2` | Advanced parity: cost insights per query, query optimizer with cardinality estimation, materialized aggregations, cross-region replication, restricted-view enforcement at index level. |

## Official Palantir documentation library

### Product overview

- [Object Storage V2 overview](https://www.palantir.com/docs/foundry/object-storage-v2/overview)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Concepts

- [Storage layout](https://www.palantir.com/docs/foundry/object-storage-v2/layout)
- [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes)
- [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write)
- [Permissions and markings](https://www.palantir.com/docs/foundry/object-storage-v2/permissions)
- [Subscriptions](https://www.palantir.com/docs/foundry/object-storage-v2/subscriptions)
- [Snapshots and restore](https://www.palantir.com/docs/foundry/object-storage-v2/snapshots)

### Integrations

- [OQL pushdown](https://www.palantir.com/docs/foundry/ontology/oql-pushdown)
- [Vertex traversal pushdown](https://www.palantir.com/docs/foundry/vertex/traversal-pushdown)
- [Map spatial pushdown](https://www.palantir.com/docs/foundry/geospatial/spatial-queries)
- [Functions ontology client](https://www.palantir.com/docs/foundry/functions/ontology-client)

## Milestone A: credible storage + indexed reads + writes

### Physical layout

- [ ] `OSV2.1` Storage partitioning (`P0`, `todo`)
  - Per ontology, partition by object type; within type, partition by primary-key hash to avoid hot tablets.
  - Document the anti-hot-partition strategy and align with existing
    `docs/architecture/ontology-anti-hot-partitions.md` notes.
  - Docs: [Storage layout](https://www.palantir.com/docs/foundry/object-storage-v2/layout).

- [ ] `OSV2.2` Per-object row format (`P0`, `todo`)
  - Row schema: `{rid, primary_key, type_id, version, properties_blob, markings_blob, organizations, last_updated, last_updater}`.
  - Properties stored as a typed blob (Avro or Protobuf) keyed by property id; never store property names as strings in hot rows.
  - Docs: [Storage layout](https://www.palantir.com/docs/foundry/object-storage-v2/layout).

- [ ] `OSV2.3` Link row format (`P0`, `todo`)
  - Link rows: `{link_type_id, source_rid, target_rid, properties_blob, markings_blob}` with secondary indexes on (link_type_id, source_rid) and (link_type_id, target_rid).
  - Docs: [Storage layout](https://www.palantir.com/docs/foundry/object-storage-v2/layout).

### Indices

- [ ] `OSV2.4` Property index (`P0`, `todo`)
  - Per-type, per-property B-tree (or LSM) index supporting exact match, range, and IN-list predicates.
  - Per-property null-aware semantics.
  - Docs: [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes).

- [ ] `OSV2.5` Link index (`P0`, `todo`)
  - Per-link-type bidirectional index enabling sub-second neighbor expansion for objects with up to ~10⁴ neighbors.
  - Pagination with stable cursors.
  - Docs: [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes).

### Read path

- [ ] `OSV2.6` Point reads (`P0`, `todo`)
  - `Get(type, primary_key)` returns the object with `properties` filtered by caller's marking clearances.
  - Cache hot reads in-process with short TTL; bust on local writes.
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

- [ ] `OSV2.7` Search reads (`P0`, `todo`)
  - `Search(type, predicate, pagination)` runs against the property indices, applies caller permissions/markings post-filter (or via index-side filter when supported), returns paginated results.
  - Predicate AST: `AND/OR/NOT`, `eq/neq/gt/gte/lt/lte/in/contains/starts_with`, link-traversal subqueries.
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

- [ ] `OSV2.8` Permission and marking enforcement (`P0`, `todo`)
  - Every read enforces the caller's clearances. Objects (and properties) the caller cannot see are omitted, never returned with placeholder values that leak existence.
  - Index-side marking filter where the index supports it; otherwise post-filter with a count of omitted items in the result envelope.
  - Docs: [Permissions and markings](https://www.palantir.com/docs/foundry/object-storage-v2/permissions).

### Write path

- [ ] `OSV2.9` Indexer pipeline writes (`P0`, `todo`)
  - The indexer (`services/ontology-indexer/`) writes per-object and per-link rows from Kafka event streams (`TopicObjectChangedV1`, `TopicLinkChangedV1`).
  - Idempotent on event id; deduplicates retries.
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

- [ ] `OSV2.10` Action writes (`P0`, `todo`)
  - Action execution path commits writes atomically (per-object row + affected link rows in one transaction) and emits an audit event with the actor and the writeback policy.
  - Staged writes (see [Functions runtime](./foundry-functions-runtime-1to1-checklist.md)) materialize only on `commit()`.
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

## Milestone B: full-text, vector, spatial, temporal, branches, subscriptions

### Advanced index types

- [ ] `OSV2.11` Full-text index (`P1`, `todo`)
  - Per-type, per-property inverted index for tokenized text; supports phrase queries, prefix queries, and per-language analyzers.
  - Backend pluggable (OpenSearch or Vespa) reusing the existing `libs/search-abstraction`.
  - Docs: [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes).

- [ ] `OSV2.12` Vector index (`P1`, `todo`)
  - Per-type, per-property HNSW (or IVF) vector index for embedding properties; supports cosine, L2, dot-product distance.
  - Hybrid query (BM25 + ANN) usable from OQL.
  - Docs: [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes).

- [ ] `OSV2.13` Spatial index (`P1`, `todo`)
  - R-tree (or H3/S2 cell-based) index on geo properties supporting bounding-box, radius, polygon-contains queries.
  - Pushed down from Map and Vertex spatial predicates.
  - Docs: [Map spatial pushdown](https://www.palantir.com/docs/foundry/geospatial/spatial-queries).

- [ ] `OSV2.14` Time-series property store (`P1`, `todo`)
  - For object properties declared as time series, store per-tick samples (timestamp, value, optional quality) in a columnar substrate.
  - Range queries with downsampling and aggregation (min/max/avg/percentile) at fetch time.
  - Used by Quiver and by ontology time-series property queries.
  - Docs: [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes).

### Branched indices

- [ ] `OSV2.15` Branch-aware index reads (`P1`, `todo`)
  - On a branch read, prefer the branch's overlay rows where present, otherwise fall back to main.
  - Predicate evaluation correctly merges branch + main without double-counting.
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

- [ ] `OSV2.16` Branch overlay writes (`P1`, `todo`)
  - Action writes on a branch land in the branch overlay only; merge to main is a separate commit produced by the Global Branching merge flow.
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

### Subscriptions

- [ ] `OSV2.17` Change-stream subscriptions (`P1`, `todo`)
  - `Subscribe(type, predicate, since_cursor)` returns a server-sent-events stream of object/link changes matching the predicate; supports resume from cursor after disconnect.
  - Stream enforces permissions; revoked clearances terminate the stream.
  - Docs: [Subscriptions](https://www.palantir.com/docs/foundry/object-storage-v2/subscriptions).

- [ ] `OSV2.18` OSDK + Workshop integration (`P1`, `todo`)
  - OSDK `subscribe()` and Workshop reactive variables consume the subscription stream with typed payloads.
  - Docs: [Subscriptions](https://www.palantir.com/docs/foundry/object-storage-v2/subscriptions).

### Snapshots and restore

- [ ] `OSV2.19` Per-type snapshots (`P1`, `todo`)
  - Periodic snapshots of each object type's storage (including all index rows) with a content hash for verifiable restore.
  - Snapshot scheduled by the data-health/retention layer.
  - Docs: [Snapshots and restore](https://www.palantir.com/docs/foundry/object-storage-v2/snapshots).

- [ ] `OSV2.20` Restore-to-snapshot (`P1`, `todo`)
  - Restore a type or a branch's overlay to a prior snapshot with audit and dependency warning (downstream Actions, dashboards, OSDK consumers are listed before commit).
  - Docs: [Snapshots and restore](https://www.palantir.com/docs/foundry/object-storage-v2/snapshots).

## Milestone C: query optimizer, materializations, replication, governance

### Query optimizer

- [ ] `OSV2.21` Cardinality estimation (`P2`, `todo`)
  - Maintain per-property histograms and per-link fan-out distributions; expose to the OQL planner so it can choose join order and index access path.
  - Refresh histograms after large writes.
  - Docs: [OQL pushdown](https://www.palantir.com/docs/foundry/ontology/oql-pushdown).

- [ ] `OSV2.22` Cost-based plan selection (`P2`, `todo`)
  - OQL planner emits `EXPLAIN` showing index choices, estimated rows, estimated time.
  - `EXPLAIN ANALYZE` mode runs the query and reports actuals.
  - Docs: [OQL pushdown](https://www.palantir.com/docs/foundry/ontology/oql-pushdown).

### Materialized aggregations

- [ ] `OSV2.23` Materialized aggregates (`P2`, `todo`)
  - Declare common aggregations (e.g. count by status, sum by region) as materialized views that the indexer maintains incrementally.
  - OQL planner rewrites compatible queries to read from the materialization.
  - Docs: [Index types](https://www.palantir.com/docs/foundry/object-storage-v2/indexes).

### Cross-region replication

- [ ] `OSV2.24` Cross-region read replicas (`P2`, `todo`)
  - Read replicas in other regions with bounded replication lag; OSDK clients can opt in to local reads with a max-staleness hint.
  - Cross-region writes are disallowed unless a region promotion is in flight (Apollo).
  - Docs: [Read and write paths](https://www.palantir.com/docs/foundry/object-storage-v2/read-write).

### Restricted-view enforcement and cost

- [ ] `OSV2.25` Restricted-view enforcement at index level (`P2`, `todo`)
  - Restricted views translate to index-side row filters so the planner does not scan rows the caller cannot see.
  - Documented constraint: restricted views cannot be inputs of transform pipelines (mirrors the Security/Governance constraint).
  - Docs: [Permissions and markings](https://www.palantir.com/docs/foundry/object-storage-v2/permissions).

- [ ] `OSV2.26` Per-query cost accounting (`P2`, `todo`)
  - Every query records rows scanned, indices hit, returned rows, and wall time; aggregated for the Resource Management cost insights view.
  - Docs: [OQL pushdown](https://www.palantir.com/docs/foundry/ontology/oql-pushdown).

- [ ] `OSV2.27` Query rate limits (`P2`, `todo`)
  - Per-caller and per-project query budgets; soft warning at 80%, hard rate-limit at 100% with backoff hints.
  - Docs: [OQL pushdown](https://www.palantir.com/docs/foundry/ontology/oql-pushdown).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current ontology storage backend (Cassandra per `docs/architecture/ontology-cassandra-tables.md`) and decide whether OSV2 replaces it or sits on top.
- [ ] `INV.2` Identify the indexer event stream contract (`TopicObjectChangedV1`, `TopicLinkChangedV1`) and confirm idempotency.
- [ ] `INV.3` Identify the existing search backend (Vespa/OpenSearch) and define its role in full-text / vector indices.
- [ ] `INV.4` Identify the time-series substrate (existing Cassandra tables, or a new columnar store) for time-series properties.
- [ ] `INV.5` Identify the branched-storage strategy (overlay tables vs. row-level branch markers).
- [ ] `INV.6` Identify the OQL planner hook contract for pushdown.
- [ ] `INV.7` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `object-database-service` | Per-object row storage, point reads, search reads with permission/marking enforcement, write path. |
| `object-index-service` | Property/link/full-text/vector/spatial indices, index maintenance from the event stream, query-plan execution. |
| `object-timeseries-service` | Time-series property store, range queries with aggregation/downsampling. |
| `ontology-indexer` | Event-stream consumer that fans out to the index service idempotently. |
| `ontology-query-service` | OQL planner that calls into object-index-service via pushdown contracts. |
| `apps/web` | OSV2 admin views (snapshot history, restore, cost), `EXPLAIN`/`EXPLAIN ANALYZE` viewer for power users. |
