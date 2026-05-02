# ADR-0020: Apache Cassandra as the operational state store

- **Status:** Accepted
- **Date:** 2026-05-02
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes / supplements:**
  - Implicit "Postgres everywhere" pattern visible in
    [infra/k8s/cnpg/clusters/](../../../infra/k8s/cnpg/clusters/) (71
    per-service CNPG `Cluster` manifests at the time of writing).
  - The fit-for-purpose statement in
    [docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md)
    that endorses a wide-column store for high-rate operational state.
- **Related ADRs:**
  - [ADR-0008](./ADR-0008-iceberg-rest-catalog-lakekeeper.md) — Iceberg
    remains the canonical analytical store; Cassandra does **not** replace
    Iceberg.
  - [ADR-0010](./ADR-0010-cnpg-postgres-operator.md) — CNPG Postgres
    is retained for declarative schema, Cedar policies and the outbox.
  - [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md) — Temporal
    persistence + visibility live in dedicated keyspaces of this same
    Cassandra cluster.
  - [ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md) —
    The transactional outbox lives in **Postgres**, not in Cassandra.
- **Related work:** The Cassandra cluster is provisioned by the
  k8ssandra-operator under `infra/k8s/cassandra/` (see
  [§5 of the migration plan](../migration-plan-cassandra-foundry-parity.md)).

## Context

OpenFoundry's persistence layer today is dominated by per-service Postgres
clusters (71 CNPG `Cluster` CRs, ≈213 instances). The audit in
[docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md)
identifies this layout as both an operational liability and a fit-for-purpose
mismatch:

- Hot operational state (object instances of the ontology, action logs,
  sessions, refresh tokens, oauth state, agent conversation state,
  notifications inbox, Temporal persistence/visibility) is **write-heavy,
  TTL-friendly and horizontally partitionable by tenant**, which is the
  textbook workload for a wide-column store.
- The same state has **strict latency budgets** (P95 read by id <20 ms, P99
  write <50 ms) that a per-service single-leader Postgres struggles with
  once writes scale beyond a single primary's commit budget and once
  multi-region is on the table.
- Postgres remains an excellent fit for **declarative configuration**
  (ontology type definitions, dataset catalogs, OAuth client config,
  Cedar policies, the outbox table, the Lakekeeper catalog), and we want
  to keep it for those purposes.

We therefore need a single, supported wide-column store that:

1. Runs on the OpenFoundry Kubernetes substrate with the same operational
   discipline as the rest of the platform (operator, backups, repair,
   monitoring, runbook).
2. Is fully open-source under a permissive license (Apache-2.0).
3. Has a healthy Rust ecosystem (CQL driver, testcontainers).
4. Is multi-DC capable from day one, without requiring an architectural
   pivot when we cross the cross-region boundary in
   [ADR-0023](./ADR-0023-iceberg-cross-region-dr.md) (planned).

## Options considered

### Option A — Apache Cassandra 5.0 (chosen)

- Apache-2.0, mature project (15+ years), Apache top-level.
- True multi-DC topology with `NetworkTopologyStrategy` and tunable
  per-DC consistency (`LOCAL_QUORUM`, `EACH_QUORUM`).
- Cassandra 5.0 ships **Storage-Attached Indexes (SAI)** and **vector
  indexes**, removing the historical "secondary index is unusable"
  caveat for narrow, well-justified cases.
- Operationally, the **k8ssandra-operator** (Apache-2.0, maintained by
  DataStax) bundles Cassandra + Reaper (auto-repair) + Medusa (S3
  backups) + Stargate, all of which we will use.
- Endorsed as Temporal's reference persistence backend, which lets us
  collapse "operational state store" and "Temporal persistence" into one
  cluster (see [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md)).
- **Rust ecosystem:** the `scylla` crate (Apache-2.0, ScyllaDB Inc.) is
  the most mature CQL driver in Rust — token-aware routing, prepared
  statement cache, paging streams, speculative execution, full async on
  Tokio — and it speaks plain CQL, so it is fully compatible with
  Cassandra 5.0. (The crate name is a historical artifact; it is not
  ScyllaDB-specific.)

### Option B — ScyllaDB

- Higher single-node throughput, C++ shard-per-core architecture.
- Rejected because:
  - The user explicitly chose Cassandra over Scylla for this platform.
  - The OSS edition lags the Enterprise edition on multi-DC operations
    that we plan to depend on.
  - Operational tooling (Reaper, Medusa) and Temporal's integration are
    more mature against Cassandra.

### Option C — CockroachDB / YugabyteDB (distributed SQL)

- SQL-compatible, strong consistency, horizontally scalable.
- Rejected because:
  - The workload we are migrating off Postgres is **not** a SQL workload
    that needs distributed transactions; it is a key-by-partition,
    high-throughput TTL workload that is a poor fit for SQL semantics.
  - Adds a second SQL dialect to operate without removing Postgres
    (which we still need for declarative config and outbox).
  - Temporal's first-class persistence backends are Cassandra,
    PostgreSQL and MySQL — not Cockroach/Yuga.

### Option D — Stay on Postgres (Citus or vertical scale)

- Rejected. The audit
  ([docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md))
  documents in detail why per-service single-leader Postgres cannot meet
  the multi-DC, write-heavy, TTL-native requirements of the operational
  workloads in scope. Citus solves sharding but not multi-DC writes nor
  native TTL.

### Option E — DynamoDB / Bigtable (managed)

- Rejected. Vendor lock-in and incompatible with the self-hostable, OSS
  posture of the platform.

## Decision

We adopt **Apache Cassandra 5.0** as the single operational state store
for OpenFoundry, deployed and operated through the
**k8ssandra-operator** on Kubernetes, with the **`scylla` Rust crate**
(version 0.13+) as the official CQL driver across the workspace.

The Cassandra cluster is the canonical store for:

- Ontology object instances, properties and relationships
  (keyspace `ontology_objects`).
- Materialised secondary indexes for the ontology
  (keyspace `ontology_indexes`).
- Append-only action log (keyspace `actions_log`).
- Sessions, refresh tokens, oauth state (keyspace `sessions`).
- Notifications inbox (keyspace `notifications_inbox`).
- Agent / conversation short-term state (keyspace `agent_state`).
- Temporal persistence and visibility (keyspaces `temporal_persistence`,
  `temporal_visibility`) — see
  [ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md).

The transactional outbox is **explicitly not** in Cassandra — see
[ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md).

## Topology and configuration

### Replication

- Production: `NetworkTopologyStrategy` with `{dc1:3, dc2:3, dc3:3}`
  from day one, even when only `dc1` is physically deployed (so that
  cross-DC expansion in [ADR-0023](./ADR-0023-iceberg-cross-region-dr.md)
  does not require a schema migration).
- Development (compose / single-node): `SimpleStrategy` with `RF=1` is
  acceptable.

### Consistency levels

| Operation class                                | Default       | Rationale                                            |
| ---------------------------------------------- | ------------- | ---------------------------------------------------- |
| Authoritative read (strong)                    | `LOCAL_QUORUM`| Read-your-write within a DC, no cross-DC latency.    |
| Authoritative write                            | `LOCAL_QUORUM`| Same.                                                |
| Cache-friendly / eventual read                 | `LOCAL_ONE`   | Used by `ontology-query-service` behind moka cache.  |
| Append-only TTL (`actions_log`, `sessions`)    | `LOCAL_QUORUM`| TTL data is cheap to lose, but reads must be stable. |
| Cross-DC global read (rare, audit)             | `EACH_QUORUM` | Used only by tooling, not by hot path.               |

`SERIAL` / `LOCAL_SERIAL` (LWT) is **restricted to genuinely concurrent
optimistic locks** (e.g. version-conditional updates). LWT must not be
used as a substitute for transactional semantics; it costs roughly four
round-trips and breaks the hot-path SLO if applied indiscriminately.

### Compaction strategy

| Workload                                    | Strategy                                  |
| ------------------------------------------- | ----------------------------------------- |
| `objects_*` (mutable, mixed read/write)     | `LeveledCompactionStrategy` (LCS)         |
| `actions_log`, `sessions`, `agent_state`    | `TimeWindowCompactionStrategy` (TWCS)     |
| `temporal_persistence`                      | per Temporal recommendations (LCS)        |
| `temporal_visibility`                       | per Temporal recommendations (LCS)        |

TWCS window matches the time bucket used in the partition key (hourly
for `actions_log`, hourly for `sessions`).

## Data modelling rules (hard rules)

These rules are enforced by code review and, where possible, by a CI
linter on `*.cql` files.

1. **Composite partition keys are mandatory.** A bare `tenant_id` PK is
   forbidden; the canonical pattern is
   `((tenant_id, type_id, time_bucket), updated_at DESC, object_id)`.
2. **Time bucketing is mandatory** on tables with any time component.
   - `objects_by_*`: daily bucket (`yyyymmdd` int).
   - `actions_log`, `sessions`, `agent_state`: hourly bucket.
   - `outbox_*`: not applicable — outbox is in Postgres
     (see [ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md)).
3. **Hard partition size limits:** target ≤50 MB and ≤100 000 rows per
   partition. `nodetool tablestats` is alarmed at >50 MB.
4. **`object_id` is a TimeUUID**, not UUIDv4, so that range scans by
   time are natural.
5. **No multi-partition `SELECT`.** Any query that would cross more than
   one partition is **forbidden** at the Cassandra layer; it must be
   served from Vespa / OpenSearch
   (see [ADR-0028](./ADR-0028-search-backend-abstraction.md)) or from
   Iceberg via Trino (see
   [ADR-0008](./ADR-0008-iceberg-rest-catalog-lakekeeper.md)).
6. **Secondary indexes are vetoed.** Storage-Attached Indexes (SAI)
   are allowed in narrow, justified cases (one ADR exception per use
   site). The default pattern is **application-maintained materialised
   tables**.
7. **`ALLOW FILTERING` is forbidden.** Use of `ALLOW FILTERING` in any
   committed CQL is a CI failure.
8. **No bulk `DELETE`.** Mutable rows that need logical removal use a
   `deleted boolean` flag plus application-side filtering. Tables with
   genuine row turnover use TTL plus TWCS.
9. **TTL is mandatory** on tables holding ephemeral state (sessions,
   oauth state, action log, notifications inbox).
10. **`BATCH LOGGED` is restricted** to single-partition batches that
    need atomicity. Cross-partition logged batches are forbidden
    (they defeat horizontal scaling).
11. **LWT (`IF`) is restricted** to genuinely concurrent optimistic
    locks. Idempotency keys are the default mechanism for at-least-once
    write safety.
12. **Row size cap:** any row >100 KB must be split or pushed to
    Iceberg (large blobs do not belong in Cassandra).

## Anti-patterns explicitly rejected

- **Cassandra as a transactional store.** Multi-row, multi-table
  atomicity is not provided; the outbox lives in Postgres.
- **Cassandra as a search engine.** Full-text search and vector ANN at
  scale go to Vespa / OpenSearch
  ([ADR-0028](./ADR-0028-search-backend-abstraction.md)).
- **Cassandra as an analytical store.** Aggregations, joins and time
  travel go to Iceberg via Trino
  ([ADR-0008](./ADR-0008-iceberg-rest-catalog-lakekeeper.md)).
- **`Materialized Views` (the CQL feature).** They are still flagged
  experimental in Cassandra 5; we maintain materialised tables in
  application code instead.

## Operational consequences

- New sub-chart `infra/k8s/cassandra/` based on the **k8ssandra-operator**
  (Apache-2.0).
- Reaper schedules a full repair per keyspace per gc-grace window
  (default weekly), throttled off-peak.
- Medusa runs nightly snapshots to the Ceph S3 bucket
  `openfoundry-cassandra-backups`.
- A new shared crate `libs/cassandra-kernel` wraps `scylla` with the
  workspace's session lifecycle, retry policy, prepared-statement cache
  and CQL migration helper.
- A new test helper `libs/testing/src/cassandra.rs` provides an
  ephemeral `cassandra:5` testcontainer behind the `it-cassandra`
  feature flag.
- The runbook
  [infra/runbooks/cassandra.md](../../../infra/runbooks/cassandra.md)
  (created by task S0.2.g of the migration plan) covers repair,
  scale-out, replace-node and restore.

## Consequences

### Positive

- Hot-path latency budgets become achievable (P95 reads <20 ms, P99
  writes <50 ms at 5 000 RPS sustained on a 3-node cluster).
- Multi-DC is structurally available from day one with no schema change.
- TTL-driven cleanup removes a whole class of nightly cron jobs that
  Postgres requires for session and token expiry.
- Postgres footprint collapses from 71 clusters to 4 consolidated
  clusters (see [ADR-0024](./ADR-0024-postgres-consolidation.md)).
- Temporal gets a first-class persistence backend without a second
  storage technology.

### Negative

- A new storage technology to operate, monitor and on-call on. Mitigated
  by k8ssandra-operator + Reaper + Medusa, by the runbook, and by
  treating the cluster as a shared platform asset rather than a
  per-service install.
- Data modelling discipline becomes a code-review concern: violating
  the rules above is the difference between "fast" and "unusable" in
  Cassandra. Mitigated by the CI linter on `*.cql` files, by the rules
  in this ADR, and by the workshop scheduled in the migration plan
  before stream S1 starts.
- The Rust driver crate is named `scylla` even though we run Cassandra.
  This is a recurring source of confusion; documented here and in
  `libs/cassandra-kernel/README.md`.
- LWT and SAI are powerful but easy to misuse; both require an explicit
  ADR exception per use site.

### Neutral

- The public gRPC / OpenAPI / SDK surface is unaffected: persistence is
  hidden behind the repository traits introduced in
  `libs/storage-abstraction/src/repositories.rs` (task S0.4 of the
  migration plan).

## Follow-ups

- Implement task S0.2 of the migration plan: bring up the dev
  single-node and prod multi-DC clusters under
  `infra/k8s/cassandra/`.
- Implement task S0.3 of the migration plan: publish
  `libs/cassandra-kernel`.
- Add a CI check that fails on `ALLOW FILTERING`, on bare-tenant PKs
  and on CQL files lacking a `WITH compaction = ...` clause.
- Re-evaluate this ADR when Cassandra 6.0 is released or when SAI is
  promoted out of experimental status for vector workloads.
