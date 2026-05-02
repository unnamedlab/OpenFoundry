# Cassandra data model — design document

- **Status:** Working draft, kept in lock-step with code.
- **Date:** 2026-05-02
- **Owners:** OpenFoundry platform architecture group
- **Related:**
  - [ADR-0020](./adr/ADR-0020-cassandra-as-operational-store.md) —
    decision and hard rules.
  - [ADR-0021](./adr/ADR-0021-temporal-on-cassandra-go-workers.md) —
    Temporal keyspaces share this cluster.
  - [ADR-0022](./adr/ADR-0022-transactional-outbox-postgres-debezium.md) —
    why the outbox is **not** in Cassandra.
  - [ADR-0028](./adr/ADR-0028-search-backend-abstraction.md) — search
    indexes are projections of Cassandra state.
  - [migration-plan-cassandra-foundry-parity.md](./migration-plan-cassandra-foundry-parity.md) —
    §3.1 lists the keyspaces; this document fills in the tables.

## Modelling principles (recap)

The hard rules from [ADR-0020](./adr/ADR-0020-cassandra-as-operational-store.md):

1. **Composite partition key always.** Tenant alone is never a
   partition key.
2. **Time bucketing per table.** Daily for object-shaped state,
   hourly for time-series and ephemeral state.
3. **Partition size ceiling: ≤ 50 MB and ≤ 100 000 rows.** Alarm at
   either threshold.
4. **`object_id` is a `timeuuid`**, not a `uuid` v4. This gives us
   range scans by time inside a partition for free.
5. **TTL is mandatory** on every table whose data has a finite
   horizon. No bulk `DELETE`.
6. **Compaction strategy by access shape:**
   - LCS (LeveledCompactionStrategy) for mutable, mixed-read tables.
   - TWCS (TimeWindowCompactionStrategy) for append-only / TTL'd
     time-series, window equal to the bucket.
7. **Secondary indexes vetoed.** SAI (Cassandra 5) is allowed only
   in narrow, justified cases. The default pattern is a **materialised
   table** maintained by the application.
8. **No multi-partition `SELECT`.** Anything that needs to scan
   across partitions goes to Vespa
   ([ADR-0028](./adr/ADR-0028-search-backend-abstraction.md)) or to
   Iceberg.
9. **`ALLOW FILTERING` is forbidden.** CI lint rejects it.
10. **LWT (`IF`) is restricted** to genuinely concurrent rare cases
    (optimistic versioning on conflicting writes). Everything else
    uses idempotency keys.
11. **`BATCH LOGGED` is restricted to single-partition batches.**
    Cross-partition batches are forbidden — no cross-table atomicity.
12. **Row size ≤ 100 KB.** Larger payloads go to object storage
    (Iceberg / Ceph) with the row holding only the key.

Every table below documents (a) the dominant queries it serves,
(b) its partition key and clustering key, (c) its expected partition
size profile, (d) its compaction strategy, and (e) its TTL where
applicable.

---

## Keyspace `ontology_objects`

**Purpose.** The authoritative store for ontology object instances
and their properties. The single hottest write keyspace in the
platform.

**Replication.** `NetworkTopologyStrategy {dc1:3, dc2:3, dc3:3}`.
**Default consistency.** `LOCAL_QUORUM` for both reads and writes.

### `objects_by_id` — primary read by id

```sql
CREATE TABLE ontology_objects.objects_by_id (
    tenant_id      text,
    object_id      timeuuid,
    type_id        text,
    owner_id       text,
    markings       set<text>,
    properties     blob,        -- packed: zstd(json) or zstd(arrow)
    version        bigint,
    deleted        boolean,
    created_at     timestamp,
    updated_at     timestamp,
    PRIMARY KEY ((tenant_id, object_id))
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND comment = 'Authoritative read by primary key';
```

- **Dominant query:** "give me object by id". One partition, one row.
- **Partition size:** one row per partition. Trivial.
- **TTL:** none. Soft-delete via `deleted = true`; hard tombstone
  forbidden.
- **Versioning:** `version` is the optimistic-concurrency token.
  Writes use LWT (`IF version = ?`) only on contended paths; the
  steady-state path uses last-writer-wins with `event_id`-derived
  idempotency upstream.

### `objects_by_type` — listings by object type

```sql
CREATE TABLE ontology_objects.objects_by_type (
    tenant_id      text,
    type_id        text,
    day_bucket     date,
    updated_at     timestamp,
    object_id      timeuuid,
    owner_id       text,
    markings       set<text>,
    deleted        boolean,
    PRIMARY KEY ((tenant_id, type_id, day_bucket), updated_at, object_id)
) WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'};
```

- **Dominant query:** "objects of this type, most recently changed
  first, paged". Single partition per (tenant, type, day).
- **Time bucket:** daily. Tenants with > 100 k objects per type per
  day re-bucket to hourly via a per-tenant override (rare in
  practice).
- **Partition size profile:** target ≤ 30 MB at p99, alarm at 40 MB.
  Sizing assumes < 50 k objects per (tenant, type) per day; any
  tenant approaching that threshold is flagged for hourly bucketing.
- **Maintenance:** maintained by the same handler that writes
  `objects_by_id`, in the same write path. The two writes are
  idempotent; a retry converges.

### `objects_by_owner` — listings by owner

```sql
CREATE TABLE ontology_objects.objects_by_owner (
    tenant_id      text,
    owner_id       text,
    type_id        text,
    object_id      timeuuid,
    updated_at     timestamp,
    deleted        boolean,
    PRIMARY KEY ((tenant_id, owner_id), type_id, object_id)
) WITH CLUSTERING ORDER BY (type_id ASC, object_id ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'};
```

- **Dominant query:** "objects this user owns, grouped by type".
- **Partition size profile:** per-user fan-out. Sized for
  ≤ 20 000 objects per owner; tenants with super-owners (system
  accounts) are explicitly flagged to use a different access pattern
  (Vespa or partition split by `(owner_id, year)`).

### `objects_by_marking` — listings by marking

```sql
CREATE TABLE ontology_objects.objects_by_marking (
    tenant_id      text,
    marking_id     text,
    type_id        text,
    object_id      timeuuid,
    updated_at     timestamp,
    deleted        boolean,
    PRIMARY KEY ((tenant_id, marking_id), type_id, object_id)
) WITH CLUSTERING ORDER BY (type_id ASC, object_id ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'};
```

- **Dominant query:** "all objects under marking M". Used by
  marking-propagation maintenance Workflows.
- **Partition size profile:** markings are a few-cardinality
  dimension; tenant-marking partitions can grow large. Rule: any
  marking with > 50 000 objects must be sub-bucketed by `month`.
  CI test on the migration scripts asserts the rule on representative
  fixtures.

---

## Keyspace `ontology_indexes`

**Purpose.** Materialised projections that complement
`ontology_objects`. Maintained by the application. Allowed to lag
the source on a bounded horizon (sub-second target).

**Replication / consistency.** As `ontology_objects`.

### `links_by_source` — outgoing links from an object

```sql
CREATE TABLE ontology_indexes.links_by_source (
    tenant_id      text,
    source_id      timeuuid,
    link_type      text,
    target_id      timeuuid,
    properties     blob,
    created_at     timestamp,
    deleted        boolean,
    PRIMARY KEY ((tenant_id, source_id), link_type, target_id)
) WITH CLUSTERING ORDER BY (link_type ASC, target_id ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'};
```

- **Dominant query:** "outgoing links from object X".
- **Partition size profile:** sized for ≤ 5 000 outgoing links per
  source. Highly connected nodes (super-nodes) are detected by
  cardinality alarm and routed to Vespa for traversal.

### `links_by_target` — incoming links to an object

```sql
CREATE TABLE ontology_indexes.links_by_target (
    tenant_id      text,
    target_id      timeuuid,
    link_type      text,
    source_id      timeuuid,
    properties     blob,
    created_at     timestamp,
    deleted        boolean,
    PRIMARY KEY ((tenant_id, target_id), link_type, source_id)
) WITH CLUSTERING ORDER BY (link_type ASC, source_id ASC)
  AND compaction = {'class': 'LeveledCompactionStrategy'};
```

- Symmetric to `links_by_source`. Same sizing rules.

### Maintenance contract

Every link write is **two table writes** (`links_by_source` +
`links_by_target`), in the same handler, both keyed by a
deterministic `event_id`. Either succeeds and the second is retried
on the next handler call (idempotent), or both succeed. A
reconciliation Workflow in `workers-go/reindex/` repairs any drift
nightly by walking `objects_by_id` and verifying the two materialised
views agree.

---

## Keyspace `actions_log`

**Purpose.** Append-only log of actions applied to ontology objects.
Source of the analytical projection in
`of.ontology_history.actions_v1` on Iceberg.

**Replication / consistency.** As `ontology_objects`.

### `actions_by_tenant_hour` — primary append target

```sql
CREATE TABLE actions_log.actions_by_tenant_hour (
    tenant_id      text,
    hour_bucket    timestamp,    -- truncated to the hour
    applied_at     timestamp,
    action_id      timeuuid,
    object_id      timeuuid,
    type_id        text,
    actor_id       text,
    payload        blob,
    PRIMARY KEY ((tenant_id, hour_bucket), applied_at, action_id)
) WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_unit': 'HOURS',
                    'compaction_window_size': '1'}
  AND default_time_to_live = 7776000;   -- 90 days
```

- **Dominant query:** "show me the last N actions in the past
  X hours for tenant T".
- **Time bucket:** hourly. Sizing target ≤ 30 MB per partition at
  p99.
- **TTL:** 90 days. Long-term history lives in Iceberg.
- **Compaction:** TWCS with hourly windows. The compactor never
  rewrites a window after it ages out, keeping write amplification
  proportional to a single window.

### `actions_by_object` — actions for one object

```sql
CREATE TABLE actions_log.actions_by_object (
    tenant_id      text,
    object_id      timeuuid,
    applied_at     timestamp,
    action_id      timeuuid,
    actor_id       text,
    payload        blob,
    PRIMARY KEY ((tenant_id, object_id), applied_at, action_id)
) WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_unit': 'DAYS',
                    'compaction_window_size': '7'}
  AND default_time_to_live = 7776000;
```

- **Dominant query:** "history of object X".
- **Time bucket:** none at the partition key — partition is per
  object. Sizing assumes ≤ 1 000 actions per object over 90 days; an
  object that exceeds this threshold is flagged and re-modelled by
  bucketing by month. CI fixture asserts the threshold.

---

## Keyspace `auth_runtime`

**Purpose.** Sessions, refresh-token families, OAuth state, MFA
challenge state. Per-user, ephemeral, TTL'd.

**Replication / consistency.** `NetworkTopologyStrategy`. **Reads
default `LOCAL_ONE` for low-criticality lookups (rate limit, MFA
challenge); `LOCAL_QUORUM` for sessions and refresh-token validation
on the auth path.**

### `sessions_by_token` — primary session lookup

```sql
CREATE TABLE auth_runtime.sessions_by_token (
    token_hash     blob,         -- SHA-256 of the session token
    user_id        text,
    created_at     timestamp,
    last_seen_at   timestamp,
    issued_for     text,
    scope          set<text>,
    mfa_level      text,
    family_id      uuid,
    PRIMARY KEY (token_hash)
) WITH compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_unit': 'HOURS',
                    'compaction_window_size': '6'}
  AND default_time_to_live = 86400;   -- 24h absolute, configurable per realm
```

- **Dominant query:** "validate token". One partition, one row.
- **TTL:** mandatory. Step-up flows write a new row and let the old
  one TTL out.
- **Compaction:** TWCS, 6-hour windows.

### `refresh_token_families` — replay detection

```sql
CREATE TABLE auth_runtime.refresh_token_families (
    family_id      uuid,
    issued_at      timestamp,
    refresh_hash   blob,
    user_id        text,
    rotated_at     timestamp,    -- null if current
    rotated_to     blob,         -- null if current
    PRIMARY KEY ((family_id), issued_at, refresh_hash)
) WITH CLUSTERING ORDER BY (issued_at DESC, refresh_hash ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_unit': 'DAYS',
                    'compaction_window_size': '7'}
  AND default_time_to_live = 7776000;   -- 90 days
```

- **Dominant query:** "is this refresh the head of its family, and if
  not, who rotated it to what?". Family-scoped partition.
- **Replay detection:** see
  [ADR-0026](./adr/ADR-0026-identity-custom-retained.md) §"Refresh
  token families".

### `mfa_challenges` — short-lived challenge state

```sql
CREATE TABLE auth_runtime.mfa_challenges (
    challenge_id   uuid,
    user_id        text,
    issued_at      timestamp,
    method         text,
    payload        blob,
    PRIMARY KEY (challenge_id)
) WITH default_time_to_live = 600;   -- 10 minutes
```

- **TTL:** 10 minutes. Compaction default; short TTL keeps the
  table tiny.

---

## Keyspace `notifications_inbox`

**Purpose.** Per-user notification inbox with bounded retention.

**Replication / consistency.** `NetworkTopologyStrategy`. Reads
default `LOCAL_ONE`; writes `LOCAL_QUORUM`.

### `inbox_by_user_day` — primary access pattern

```sql
CREATE TABLE notifications_inbox.inbox_by_user_day (
    tenant_id      text,
    user_id        text,
    day_bucket     date,
    delivered_at   timestamp,
    notification_id timeuuid,
    kind           text,
    payload        blob,
    seen           boolean,
    PRIMARY KEY ((tenant_id, user_id, day_bucket), delivered_at, notification_id)
) WITH CLUSTERING ORDER BY (delivered_at DESC, notification_id ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_unit': 'DAYS',
                    'compaction_window_size': '1'}
  AND default_time_to_live = 2592000;   -- 30 days
```

- **Dominant query:** "my inbox, last N days, paged by recency".
- **Time bucket:** daily. Inbox UI fetches the last 7 buckets.
- **TTL:** 30 days. Long-term notification history is not a
  product feature; users that need it pull from the audit pipeline.

---

## Keyspace `agent_state`

**Purpose.** Conversational and short-term state for the AI agents
runtime.

**Replication / consistency.** `NetworkTopologyStrategy`,
`LOCAL_QUORUM` reads / writes.

### `conversation_turns_by_session` — primary pattern

```sql
CREATE TABLE agent_state.conversation_turns_by_session (
    tenant_id      text,
    session_id     uuid,
    hour_bucket    timestamp,
    turn_at        timestamp,
    turn_id        timeuuid,
    role           text,         -- user | assistant | tool
    content        blob,
    metadata       blob,
    PRIMARY KEY ((tenant_id, session_id, hour_bucket), turn_at, turn_id)
) WITH CLUSTERING ORDER BY (turn_at ASC, turn_id ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_unit': 'HOURS',
                    'compaction_window_size': '1'}
  AND default_time_to_live = 1209600;   -- 14 days
```

- **Dominant query:** "the turns of session S in chronological
  order".
- **Time bucket:** hourly so that long-running sessions stay within
  partition limits.
- **TTL:** 14 days. Long-term traces go to Iceberg
  `of.ai.traces`.

### `agent_context_by_session` — current scratchpad

```sql
CREATE TABLE agent_state.agent_context_by_session (
    tenant_id      text,
    session_id     uuid,
    key            text,
    value          blob,
    updated_at     timestamp,
    PRIMARY KEY ((tenant_id, session_id), key)
) WITH compaction = {'class': 'LeveledCompactionStrategy'}
  AND default_time_to_live = 86400;   -- 24h sliding via re-write
```

- **Dominant query:** "current value of key K in session S".
- **Compaction:** LCS — mutable, point-update reads.

---

## Keyspaces `temporal_persistence` and `temporal_visibility`

**Owned by Temporal.** Schema is created and migrated by
`temporal-cassandra-tool`. We do not author or modify these tables.

- **Replication:** `NetworkTopologyStrategy {dc1:3, dc2:3, dc3:3}`.
- **Consistency:** `LOCAL_QUORUM` (Temporal default for these
  backends).
- **Operational:** see
  [ADR-0021](./adr/ADR-0021-temporal-on-cassandra-go-workers.md) and
  the runbook `infra/runbooks/temporal.md`.

---

## Anti-patterns explicitly rejected

The following shapes have been considered and rejected during the
modelling exercise. They are listed here so reviewers do not need to
re-derive the reasoning.

- **A single `objects` table partitioned by `(tenant_id)`.**
  Unbounded partition: each tenant's entire object set in one
  partition. Rejected by Rule 1 and Rule 3.
- **Secondary index on `objects_by_id (type_id)`.** Rejected by
  Rule 7. Materialised `objects_by_type` instead.
- **`SELECT * FROM objects_by_type WHERE updated_at > ?`** without a
  partition key. Multi-partition scan; rejected by Rule 8.
- **Cassandra-resident outbox.** No cross-table atomicity, LWT cost,
  tombstone risk. Rejected; see
  [ADR-0022](./adr/ADR-0022-transactional-outbox-postgres-debezium.md).
- **Bulk `DELETE` for retention.** Rejected by Rule 5; TTL only.
- **`uuid` v4 as `object_id`.** Rejected by Rule 4; `timeuuid` only.
- **`BATCH LOGGED` across partitions** to "atomically" update an
  object and its index. Rejected by Rule 11; idempotent two-write
  pattern instead.

---

## CI checks that enforce the rules

- **Schema lint.** Every `CREATE TABLE` in a migration is parsed by a
  custom check that verifies:
  - The PK has a composite partition key (not a single-column
    partition).
  - Tables that are not in the explicitly-mutable allowlist set
    `default_time_to_live`.
  - Tables in the time-series allowlist use TWCS.
  - Tables in the mutable allowlist use LCS.
- **Query lint.** A `cqlsh`-style parser inspects every prepared
  statement at compile time (driven by a workspace macro) and rejects
  `ALLOW FILTERING` and missing partition-key predicates.
- **Partition-size guard.** A nightly Workflow runs `nodetool
  tablestats` and fires an alert if any table exceeds the partition
  size or row count thresholds. The runbook entry walks through
  re-bucketing.
- **Tombstone guard.** Prometheus alert on
  `tombstones_scanned > 1000` per slice.

---

## Migration / evolution rules

- **Adding a column** is a non-breaking change. Default value is
  documented in the migration.
- **Renaming a column** is two changes: add new, dual-write, copy,
  switch reads, drop old. The procedure is documented in
  `infra/runbooks/cassandra.md`.
- **Adding a new query pattern** that is not served by an existing
  table is a **new materialised table**, not a secondary index.
- **Re-bucketing** (daily → hourly, hourly → 15-minute) is a Workflow
  in `workers-go/reindex/` that backfills the new table from
  `objects_by_id` (or another authoritative source) before reads
  switch.

---

## Open questions tracked here

These belong to this document while the platform stabilises and graduate
to ADRs only if a decision needs durability beyond the data-model
layer.

- **Super-owners.** A small number of tenants may have a
  service account that owns > 100 k objects. We tentatively split
  `objects_by_owner` partitions by `(owner_id, year)` for those
  accounts; data to come from the first migration cohort.
- **Heavy markings.** Same shape as super-owners; resolution to be
  validated against real data once the migration starts.
- **Object property packing format.** `properties blob` is currently
  zstd-compressed JSON; we are tracking whether moving to
  zstd-compressed Arrow IPC reduces row size enough on the
  wide-property tenants to justify the schema cost.

This document is updated whenever a table is added, removed or
re-shaped. The migration plan tracks the rollout schedule.
