# Object Storage V2 storage layout baseline

Date: 2026-05-18
Scope: `OSV2.1`–`OSV2.8` storage/index and read-path baseline for ontology objects and links.

> Source note: the Palantir reference URLs listed in the migration checklist for
> Object Storage V2 storage layout and index types currently return 404 from the
> public documentation site. This design therefore implements the public-docs
> parity contract captured in
> `docs/migration/foundry-object-storage-v2-1to1-checklist.md` and keeps the
> same public-docs-only constraint.

## OSV2.1 partitioning

Object rows are partitioned by:

```text
(tenant, type_id, primary_key_hash)
```

`primary_key_hash` is a 64-way deterministic SHA-256 bucket of the object's
primary key/RID. This preserves object-type locality while preventing one large
object type from creating a single hot Cassandra tablet. The type listing table
uses the same partition key and orders by `(updated_at DESC, object_id ASC)`
inside each bucket.

This aligns with `docs/architecture/ontology-anti-hot-partitions.md`: the old
`(tenant, type_id)` type-listing partition had a documented fat-type risk around
~50 MB partitions. The OSV2 layout turns that case into 64 partitions, so a
50 MB type listing is spread to roughly 0.8 MB per bucket before replication and
compaction overhead.

## OSV2.2 object hot row

The hot object table is `ontology_objects.objects_by_id` and uses this row
shape:

```text
tenant, type_id, primary_key_hash, object_id, rid, primary_key, owner_id,
properties_blob, markings_blob, organizations, revision_number, created_at,
updated_at, last_updated, last_updater, deleted
```

`properties_blob` is an OSV2 envelope keyed by stable property identifiers. Until
the ontology schema service provides immutable property RIDs to the storage
layer, the implementation derives a deterministic `p_<sha256-64>` identifier
from the API property id/name. The hot row does not embed raw property names.
`markings_blob` stores markings in a versioned blob envelope so future marking
metadata can evolve without hot-row schema changes.

A point-read mirror, `ontology_objects.objects_by_id_by_rid`, keeps the existing
`Get(tenant, object_id)` access pattern stable while the primary physical table
is bucketed by object type and primary-key hash.

## OSV2.5 link index

Links are stored as two directional indexes:

- `ontology_indexes.links_outgoing` keyed by
  `(tenant, link_type_id, source_rid)` and clustered by `target_rid`.
- `ontology_indexes.links_incoming` keyed by
  `(tenant, link_type_id, target_rid)` and clustered by `source_rid`.

Both directions store `properties_blob` and `markings_blob`, matching object-row
blob semantics. Neighbor expansion is a single-partition read per
`(tenant, link_type_id, source_rid)` or `(tenant, link_type_id, target_rid)` and
therefore does not scan across other link types. Pagination uses an opaque cursor
containing the last emitted neighbor RID, so the next page resumes with the
clustering predicate `target_rid > cursor` or `source_rid > cursor` rather than
relying on driver paging state. This keeps traversal order stable for objects
with up to ~10^4 neighbors.

## OSV2.4 property index

`ontology_indexes.object_property_index` is an LSM-style Cassandra index with
partition key:

```text
(tenant, type_id, property_id, primary_key_hash)
```

The clustering key `(value_key, object_id)` supports exact match, range scans and
IN-list fan-out per property. Null-aware semantics are explicit: null values are
indexed as `value_kind='null'`, `value_key='null'`, and `null_value=true`.

Every object write fans out property terms into this index after the primary row
commit. Property names are normalized to the same storage property id used by the
object blob.

## OSV2.6–OSV2.8 read paths

Point reads use `GetByTypeAndPrimaryKey(tenant, type_id, primary_key)` to hit the
bucketed hot row directly via `(tenant, type_id, primary_key_hash)` instead of
scanning type partitions. The object-database-service handler wraps these reads
with a short in-process TTL cache and invalidates local cache entries after local
writes or deletes.

Search reads accept both the legacy flat filter list and a predicate AST with
`AND` / `OR` / `NOT` nodes. Single indexable predicates (`eq`, range, `in`, and
`starts_with`) are pushed into `object_property_index`; richer predicates,
`contains`, KNN, and link-traversal subqueries are evaluated after the candidate
set is assembled.

Marking enforcement is applied after index reads unless a future physical index
carries markings directly. Objects whose required markings exceed the caller's
clearances are omitted rather than returned with redacted placeholder values;
response envelopes include omitted counts where the API shape supports them.

## OSV2.9–OSV2.12 write and advanced-index paths

The ontology indexer can now run with an OSV2 `StorageProjector` in addition to
its configured search backend. Object and link events from
`ontology.objects.changed.v1` and `ontology.links.changed.v1` are projected into
`ObjectStore` and `LinkStore` first, then into the lexical/vector search backend,
and the Kafka offset is committed only after all configured sinks succeed. The
projection index tracks event ids as well as per-aggregate versions so producer
retries and stale records collapse before row or search writes are repeated.

Action and Functions writeback has an explicit staged transaction helper:
`domain.ActionWriteTransaction`. Callers stage object rows and affected link rows
without touching storage; `Commit` materializes the staged rows and appends a
single `action.writeback_committed` audit event containing the actor and
writeback policy. This helper gives runtimes a concrete `commit()` boundary while
preserving the existing object/link store abstractions.

Advanced search indices are exposed through optional `libs/storage-abstraction`
interfaces instead of hard-coding a backend:

- `FullTextSearchBackend.SearchText` models a per-type, per-property inverted
  index with phrase, prefix and language/analyzer fields.
- `HybridSearchBackend.SearchHybrid` models OSV2 vector queries with cosine, L2
  and dot-product distances plus a BM25-style lexical clause for OQL hybrid
  retrieval.

The in-memory search backend implements both optional interfaces for unit tests
and local development. OpenSearch and Vespa remain the pluggable production
backend targets through `libs/search-abstraction`.

## OSV2.13–OSV2.18 spatial, temporal, branch and subscription paths

Advanced OSV2 index contracts now cover the next parity slice in
`libs/storage-abstraction`:

- `SpatialIndexStore` models Map and Vertex spatial pushdown over geo
  properties. The in-memory implementation supports bounding-box, radius and
  polygon-contains predicates; production implementations can back the same
  surface with R-tree, H3 or S2 cells.
- `TimeSeriesPropertyStore` models columnar time-series object properties. It
  stores per-tick `(timestamp, value, quality)` samples and supports range
  retrieval with fetch-time downsampling and min/max/avg/percentile aggregation
  for Quiver and ontology time-series queries.
- `BranchOverlayStore` models branch-aware index reads and branch overlay
  writes. Reads prefer branch rows, suppress main rows hidden by branch
  tombstones and merge predicate results by object id to avoid double-counting.
  Branched action writes use `NewBranchedActionWriteTransaction`, which writes
  staged object/link changes into the overlay only and leaves merge-to-main to
  the Global Branching merge flow.
- `ChangeSubscriptionStore` models resumable object/link change streams with a
  cursor, predicate filtering and per-event authorization hooks. The stream
  contract is designed for SSE handlers: revoked clearances are represented as a
  terminal `clearances_revoked` change or by canceling the authorized stream.

The Workshop/OSDK client layer consumes the same subscription contract through
`subscribeOntologyChanges`, which returns typed payloads and a close handle.
Workshop reactive variables use `createWorkshopReactiveObjectVariable` so custom
apps and OSDK consumers share cursor/resume semantics.

## OSV2.19–OSV2.20 snapshots and restore

Per-type snapshots are exposed through `SnapshotStore`. A scheduled snapshot
captures the object rows for one `(tenant, type_id)` plus the OSV2 index rows
known to the store, serializes them in deterministic order, and records a
`sha256:` content hash. `ScheduleSnapshot` is the data-health/retention-layer
entry point; it records scheduler and retention metadata on the snapshot so a
future retention worker can prune by count or window without changing restore
semantics.

Restore is a two-step flow. `PlanRestoreSnapshot` returns dependency warnings for
downstream Actions, dashboards/Workshop modules and OSDK consumers before any
rows are changed. `RestoreSnapshot` only commits once those warning kinds are
acknowledged, then restores either main type storage or a branch overlay. Branch
restores mutate only the overlay and intentionally leave merge-to-main to the
Global Branching merge flow. Committed restores append an `osv2.snapshot_restored`
audit event containing the snapshot id, content hash, scope, branch id and listed
warnings.

## OSV2.21–OSV2.27 query optimizer and governance surfaces

The object database service now exposes the P2 optimizer contracts needed by OQL
pushdown without baking those decisions into one Cassandra implementation:

- `StatisticsProvider` maintains per-property histograms with total rows,
  null rows, distinct counts and top value buckets. `LinkStatisticsProvider`
  exposes outgoing/incoming fan-out distributions (`p50`, `p95`, `max`) so join
  ordering can prefer the most selective object and link access path first.
- Object queries accept `explain=true` for a plan-only response and
  `explain=analyze` / `analyze=true` to execute the query and include actual rows
  scanned, indices hit, returned rows and wall time. The planner chooses the
  OSV2 property index for single indexable predicates and uses histograms to
  estimate row counts.
- `MaterializedAggregateStore` declares common `count` and `sum` materializations
  (optionally grouped) and lets compatible query aggregations be rewritten to the
  maintained result instead of recomputing from the scanned object set.
- Cross-region local-read opt-in is represented by a request-level
  `max_staleness_ms` hint. The service maps eligible reads to eventual
  consistency while writes remain bound to the primary region unless a region
  promotion workflow changes the storage adapter routing.
- Restricted-view metadata is carried into the query plan as index-side filters
  (`restricted_view_id` and marking columns) so stores with security-aware
  indexes can avoid scanning rows the caller cannot read; runtime policy
  evaluation remains the compatibility fallback.
- `QueryCostRecorder` records per-query cost facts for Resource Management cost
  insights, and `QueryBudgetEnforcer` reserves per-caller/per-project scan units
  before execution with soft warning and hard retry-after semantics.
