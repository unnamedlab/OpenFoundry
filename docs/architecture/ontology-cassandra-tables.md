# Ontology Cassandra access-pattern tables (S1.1.b)

> **Scope** — 7 Cassandra tables that absorb the hot path of the
> ontology services, derived from the access patterns inventoried in
> [`ontology-queries-inventory.md`](ontology-queries-inventory.md)
> (S1.1.a) and constrained by the modeling rules of
> [ADR-0020](adr/ADR-0020-cassandra-as-operational-store.md):
>
> - composite PK obligatory; no secondary indexes,
> - ≤ 50 MB / ≤ 100 k rows per partition,
> - LCS for mutable tables, TWCS (window = bucket) for log tables,
> - `LOCAL_QUORUM` writes; reads `LOCAL_ONE` unless caller pins
>   `X-Consistency: strong`,
> - `object_id` is `timeuuid` (k-sortable, conflict-free).
>
> **Out of scope** — Per-service CQL files under `services/.../cql/`
> are S1.1.c. This document is the design contract; S1.1.c will copy
> the DDL into the per-service repo trees and S1.1.d will produce the
> wiring code. Schema-shaped tables that **stay in Postgres**
> (`object_types`, `link_types`, `properties`, `ontology_projects`,
> etc.) are listed in the inventory and are deliberately not modelled
> here.
>
> **Keyspaces and replication** — three keyspaces, all NTS
> `{dc1:3, dc2:3, dc3:3}`, default consistency `LOCAL_QUORUM`:
>
> ```cql
> CREATE KEYSPACE IF NOT EXISTS ontology_objects
>   WITH replication = {'class':'NetworkTopologyStrategy','dc1':3,'dc2':3,'dc3':3}
>    AND durable_writes = true;
>
> CREATE KEYSPACE IF NOT EXISTS ontology_indexes
>   WITH replication = {'class':'NetworkTopologyStrategy','dc1':3,'dc2':3,'dc3':3}
>    AND durable_writes = true;
>
> CREATE KEYSPACE IF NOT EXISTS actions_log
>   WITH replication = {'class':'NetworkTopologyStrategy','dc1':3,'dc2':3,'dc3':3}
>    AND durable_writes = true;
> ```

---

## 1. `ontology_objects.objects_by_id` — primary object record

**Drives**: every `objects.rs` get/create/update/delete site, every
funnel/binding write path, every action mutation. This is the single
source of truth for an object instance.

**Access patterns**

- `GET object` by `(tenant, object_id)` → 1 partition, 1 row.
- `INSERT/UPDATE` with optimistic concurrency on `revision_number`
  using LWT `IF revision_number = ?`.
- `DELETE` propagates a tombstone to all `objects_by_*` materialised
  tables via the application-level dual-write coordinator (S1.2).

**Cardinality / sizing**

- 1 row per object → partition is always 1 row, ~1–10 KB.
- `properties` stored as JSON `text` (not `map<text, blob>`): the read
  path always materialises the full document into the API response
  (`ObjectInstance` model), and per-property update is rare. JSON keeps
  the migration straightforward and avoids exploding cells. Re-evaluate
  in S1.5 if cell-level update becomes hot.

**DDL**

```cql
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
WITH compaction = {'class': 'LeveledCompactionStrategy', 'sstable_size_in_mb': 160}
 AND gc_grace_seconds = 86400
 AND default_time_to_live = 0
 AND comment = 'S1.1.b — primary object record. Source of truth for objects.rs CRUD.';
```

**Notes**

- `revision_number STATIC` mirrors the `MAX(revision_number)` scalar
  in `objects.rs:2644`; we increment it via LWT
  `UPDATE … SET revision_number = ? WHERE … IF revision_number = ?`
  so the application keeps the single-row-per-partition invariant
  while still getting monotonic versioning. Each successful LWT then
  fans out a normal `INSERT` into `actions_log` with the new revision
  body.
- `marking` is a `frozen<set<text>>` so it can be replaced atomically
  on every update (same semantics as the PG `text[]`).

---

## 2. `ontology_objects.objects_by_type` — type-tab listing

**Drives**: `objects.rs:174` `list_by_type`, the type-tab UI, and the
indexer fan-out (`domain/indexer.rs`).

**Access patterns**

- `SELECT … FROM objects_by_type WHERE tenant=? AND type_id=? ORDER BY updated_at DESC LIMIT n`
- `paging_state` for next page.
- Newest-first reads; secondary key is `(updated_at DESC, object_id)`.

**Cardinality / sizing**

- ~25 000 distinct `(tenant, type_id)` partitions at steady state
  (5 000 tenants × 5 active types).
- Worst-case ~50 000 objects per type → ~50 MB per partition at 1 KB
  payload, exactly at the ADR-0020 ceiling. Day-bucketing is provided
  as **mitigation hook** (`day_bucket date` not in PK by default; the
  S1.5 backfill switches to `((tenant, type_id, day_bucket))` if
  `nodetool tablestats` shows mean partition size > 40 MB).

**DDL**

```cql
CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_type (
    tenant            text,
    type_id           text,
    updated_at        timestamp,
    object_id         timeuuid,
    owner_id          uuid,
    marking           frozen<set<text>>,
    properties_summary text,               -- denormalised projection (UI list cells)
    deleted           boolean,
    PRIMARY KEY ((tenant, type_id), updated_at, object_id)
)
WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)
 AND compaction = {'class': 'LeveledCompactionStrategy', 'sstable_size_in_mb': 160}
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — type-tab listing. Drives objects.rs:174 list_by_type.';
```

**Notes**

- `properties_summary` is a denormalised JSON subset (top-N fields
  declared on the `object_type`) so the listing endpoint avoids a
  scatter-gather to `objects_by_id`. Refreshed on every parent update
  via the dual-write coordinator.
- Updates rewrite the cluster row at the new `updated_at` and tombstone
  the old one — this is the standard "delete + insert by clustering
  column" trick. The tombstone is reaped after `gc_grace_seconds`.

---

## 3. `ontology_objects.objects_by_owner` — my-objects view

**Drives**: future Workshop "my objects" tab; today the SQL surface
is a join inside `ontology-query-service` filters. We ship the table
empty and start dual-writing once the consumer surface ships
(see open item #2 in the inventory).

**Access patterns**

- `SELECT … WHERE tenant=? AND owner_id=?` → all of an owner's
  objects, optionally filtered by `type_id` clustering prefix.

**Cardinality / sizing**

- ~250 000 distinct partitions (5 000 tenants × 50 users).
- Median ≤ 5 000 objects per owner; safe.

**DDL**

```cql
CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_owner (
    tenant      text,
    owner_id    uuid,
    type_id     text,
    object_id   timeuuid,
    updated_at  timestamp,
    deleted     boolean,
    PRIMARY KEY ((tenant, owner_id), type_id, object_id)
)
WITH CLUSTERING ORDER BY (type_id ASC, object_id ASC)
 AND compaction = {'class': 'LeveledCompactionStrategy', 'sstable_size_in_mb': 160}
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — owner-scoped object index.';
```

---

## 4. `ontology_objects.objects_by_marking` — marking enforcement

**Drives**: marking-based scans (audit, mass re-classification). Today
this is a full-scan on PG; Cassandra cuts it to a partition read.

**Access patterns**

- `SELECT … WHERE tenant=? AND marking_id=?` → all objects bearing
  a marking. Pages with `paging_state`.

**Cardinality / sizing**

- ~5 000 distinct `(tenant, marking_id)` partitions.
- "PUBLIC"-style markings can balloon. **Mitigation**: append
  `created_day` to the PK (`((tenant, marking_id, created_day))`)
  via the migration script when monitoring detects a marking with
  > 100 000 rows. The current DDL keeps the plan-prescribed PK and
  documents the mitigation as a flag flip.

**DDL**

```cql
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
 AND compaction = {'class': 'LeveledCompactionStrategy', 'sstable_size_in_mb': 160}
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — marking-scoped index for governance scans.';
```

---

## 5. `ontology_indexes.links_outgoing` — source-side traversal

**Drives**: `links.rs` source reads, `domain/traversal.rs` outbound
walk, all `(source) -[link_type]→ ?` queries.

**Access patterns**

- `SELECT … WHERE tenant=? AND source_id=? AND link_type=?`
- `SELECT … WHERE tenant=? AND source_id=?` → all outgoing links.

**Cardinality / sizing**

- 1 partition per `(tenant, source_id)`; typical fan-out 1–100. Hub
  objects (e.g. an `Org`) can reach 10 000+ outgoing links — still
  well within partition limits.

**DDL**

```cql
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
 AND compaction = {'class': 'LeveledCompactionStrategy', 'sstable_size_in_mb': 160}
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — outbound link index.';
```

---

## 6. `ontology_indexes.links_incoming` — target-side traversal

**Drives**: `links.rs` target reads, `domain/traversal.rs` inbound
walk.

**Access patterns**

- `SELECT … WHERE tenant=? AND target_id=? AND link_type=?`
- `SELECT … WHERE tenant=? AND target_id=?` → all incoming links.

**Cardinality / sizing**

- Hubs (popular targets) may exceed 10 000 incoming links.
  **Mitigation**: bucket by `link_type` first (already in CK) plus
  add `month_bucket` to PK if `nodetool tablestats` reports mean size
  > 40 MB; flag flip in the dual-write coordinator. Default DDL
  honours the plan PK as-is.

**DDL**

```cql
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
 AND compaction = {'class': 'LeveledCompactionStrategy', 'sstable_size_in_mb': 160}
 AND gc_grace_seconds = 86400
 AND comment = 'S1.1.b — inbound link index.';
```

---

## 7. `actions_log.actions_log` — append-only action / revision / run log

**Drives**: `action_executions` writes (`actions.rs:3559`),
`action_execution_side_effects` (`actions.rs:4687`), `object_revisions`
(`objects.rs:2656`, `bindings.rs:572`), `ontology_funnel_runs`
(`funnel.rs:1179`), `ontology_rule_runs` (rule engine). 5 PG tables
collapsed into one log discriminated by `kind`.

**Access patterns**

- `INSERT` per event (append-only, no LWT).
- `SELECT … WHERE tenant=? AND day_bucket=? ORDER BY applied_at DESC LIMIT n`
  for the activity feed.
- `SELECT … WHERE tenant=? AND day_bucket=? AND applied_at < ?`
  for back-paging. Cross-day paging issues N partition queries
  client-side — capped at 7 partitions per page request.

**Cardinality / sizing**

- 1 partition per `(tenant, day_bucket)`. Window aligned with TWCS
  bucket = 1 day → SSTables drop wholesale on TTL expiry.
- TTL 90 d (7 776 000 s).

**DDL**

```cql
CREATE TABLE IF NOT EXISTS actions_log.actions_log (
    tenant         text,
    day_bucket     date,
    applied_at     timestamp,
    action_id      timeuuid,
    kind           text,                 -- 'action' | 'side_effect' | 'revision' | 'funnel_run' | 'rule_run'
    actor_id       uuid,
    target_object_id timeuuid,
    target_type_id text,
    payload        text,                 -- canonical JSON (event body)
    status         text,
    failure_type   text,
    duration_ms    int,
    PRIMARY KEY ((tenant, day_bucket), applied_at, action_id)
)
WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
 AND compaction = {
   'class': 'TimeWindowCompactionStrategy',
   'compaction_window_unit': 'DAYS',
   'compaction_window_size': 1
 }
 AND default_time_to_live = 7776000   -- 90 days
 AND gc_grace_seconds = 10800          -- 3 h: TWCS doesn't need long grace
 AND comment = 'S1.1.b — append-only log for actions, revisions, funnel runs, rule runs. TTL 90 d.';
```

**Notes**

- `kind` discriminates the 5 source streams. Downstream Iceberg ETL
  pivots on this column to write per-event-type tables in
  `of.audit.*` (matches the audit lake under stream S5).
- No LWT, no read-before-write — the log is monotonic append. Failure
  semantics: write retry on timeout is idempotent because
  `action_id timeuuid` is generated client-side.
- `applied_at` is part of the CK (not PK) so it sorts within the day;
  bucketing by date keeps partition size ~ daily volume, predictable
  per tenant.

---

## 8. Cross-cutting policies

| Aspect | Choice | Rationale |
|---|---|---|
| Default consistency (write) | `LOCAL_QUORUM` | ADR-0020 §4 |
| Default consistency (read) | `LOCAL_ONE` | hot path; caller opts into `LOCAL_QUORUM` via `X-Consistency: strong` (S1.5.c) |
| Conflict resolution | server-side timestamps + LWT for `objects_by_id.revision_number` | matches PG `revision_number` semantics in `objects.rs:2624` |
| TTL | 90 d on `actions_log` only | inventory shows mutable rows must persist indefinitely |
| Compaction | LCS for mutable, TWCS (1 d) for `actions_log` | LCS keeps read amp low for hot-read tables; TWCS lets old SSTables drop atomically |
| Tombstones | soft-delete via `deleted boolean` for `objects_by_*` | dual-write coordinator must propagate; hard delete only via repair-aware tombstone |
| Anti-hot-partition | inventory §5 mitigations wired as flag-flip migrations | keep the plan-prescribed PKs by default; only re-PK when monitoring justifies |

## 9. Mapping back to the inventory

| Inventory access pattern | Cassandra table |
|---|---|
| object by id (get/create/update/delete) | `objects_by_id` |
| object listing by type (paged DESC by `updated_at`) | `objects_by_type` |
| object listing by owner | `objects_by_owner` |
| object listing by marking | `objects_by_marking` |
| outbound graph traversal (source → ?) | `links_outgoing` |
| inbound graph traversal (? → target) | `links_incoming` |
| action / revision / funnel-run / rule-run append + activity feed | `actions_log` |

All other PG tables enumerated in [`ontology-queries-inventory.md`](ontology-queries-inventory.md)
remain in Postgres or are already off-loaded to Vespa/Iceberg under
prior streams.

— end S1.1.b design.
