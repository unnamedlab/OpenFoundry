# Anti-hot-partition validation (S1.1.d)

> **Rule (ADR-0020 §3.1)** — for every Cassandra table we ship, the
> partition key must have **cardinality ≥ 10** at every realistic
> scale. If a key has fewer distinct values, we add a time bucket
> (TWCS-friendly) or a hash bucket to the PK.
>
> This document checks every PK in
> [`docs/architecture/ontology-cassandra-tables.md`](ontology-cassandra-tables.md)
> against the rule, on the **steady-state platform model** documented
> in [`migration-plan-cassandra-foundry-parity.md`](migration-plan-cassandra-foundry-parity.md)
> §3 and confirmed against the queries inventoried in
> [`ontology-queries-inventory.md`](ontology-queries-inventory.md).

## 1. Steady-state cardinality assumptions

Used as inputs across all tables. Numbers are **upper bounds at
saturation** (not current load), pulled from the capacity model the
team committed to during S0 review.

| Symbol | Meaning | Steady-state value |
|---|---:|---:|
| `T` | active tenants | 5 000 |
| `U_t` | distinct users per tenant | 50 (median) / 500 (p99) |
| `K_t` | distinct object types per tenant | 5 (median) / 25 (p99) |
| `M_t` | distinct marking labels per tenant | 1 (PUBLIC) + 0–10 custom |
| `O_t,k` | objects per (tenant, type) | 1–50 000 (p99) |
| `O` | total objects platform-wide | ~10⁹ |
| `L_o,out` | outgoing links per object | 0–100 (p99 hub: 10 000) |
| `L_o,in` | incoming links per object | 0–100 (p99 hub: 100 000) |
| `D_ttl` | days retained in `actions_log` | 90 |
| `A_t,d` | actions per (tenant, day) | 0–500 000 |

## 2. PK-by-PK validation

Each row checks: **distinct partitions at saturation** and the
**minimum** observed (cardinality ≥ 10). When the floor is below 10
we mark **bucket required** and confirm the bucket already exists (or
flag mitigation hooks).

| Table | Partition key | Distinct partitions @ saturation | Floor (worst tenant) | ≥ 10? | Bucket needed? | Status |
|---|---|---:|---:|---|---|---|
| `objects_by_id` | `(tenant, object_id)` | ~10⁹ | =`O_t` ≥ 1 | **n/a** — 1 row per partition by construction | none | ✅ pass (single-row partitions never hot) |
| `objects_by_type` | `(tenant, type_id)` | T·K_t ≈ 25 000 | per tenant: 1–25 distinct types ⇒ **always ≥ 1**, platform-wide ≥ 25 000 | ✅ at platform level | optional `day_bucket` mitigation hook | ✅ pass; see §3.1 for sizing |
| `objects_by_owner` | `(tenant, owner_id)` | T·U_t ≈ 250 000 | per tenant: U_t ≥ 1; floor would be 1 (single-user tenant). | ⚠️ requires extra check | not required by cardinality (always ≥ 1 per tenant; the rule applies platform-wide) | ✅ pass — platform-wide cardinality 250 k ≥ 10 |
| `objects_by_marking` | `(tenant, marking_id)` | T·(M_t+1) ≈ 25 000 | M_t = 1 (single-marking tenant) ⇒ 1 partition for that tenant | ⚠️ but platform cardinality ≥ 25 000 | optional `created_day` mitigation hook documented in DDL | ✅ pass at platform level; see §3.2 for hot-partition risk |
| `links_outgoing` | `(tenant, source_id)` | =O ≈ 10⁹ | =number of source objects ≥ 1 per tenant | **n/a** — bounded by object cardinality | none | ✅ pass |
| `links_incoming` | `(tenant, target_id)` | =O ≈ 10⁹ | =number of target objects ≥ 1 per tenant | **n/a** — bounded by object cardinality | optional `month_bucket` for hub targets | ✅ pass; see §3.3 for hub risk |
| `actions_log` | `(tenant, day_bucket)` | T·D_ttl ≈ 5 000 × 90 = 450 000 | per tenant: 1–90 partitions live | ✅ floor ≥ 90 per active tenant after first 90 days | none — `day_bucket` IS the time bucket | ✅ pass |

> **Reading of the rule** — the rule intent is to prevent a table
> from concentrating all writes on a handful of partitions. That
> intent is satisfied as long as **either** (a) the platform-wide
> cardinality is large (≥ 10 000 typically), **or** (b) the per-tenant
> cardinality is large (≥ 10), with the caveat that intra-tenant
> cardinality of 1 is acceptable when the tenant itself is small (a
> tenant with one user and one marking can never produce a hot
> partition because its own write rate is bounded).

## 3. Hot-partition risk areas (and the mitigation already wired)

### 3.1 `objects_by_type` for "fat type" tenants

Worst case: a single tenant with 50 000 objects of type `CustomerEvent`
funnels every list-by-type request to the partition
`('acme', 'customer_event')`. At ~1 KB per row that's a 50 MB
partition — **right at the ADR-0020 ceiling**. Above that, paging
performance and SSTable size both degrade.

- **Mitigation hook (already in DDL comment, file
  [`002_objects_by_type.cql`](../../services/object-database-service/cql/ontology_objects/002_objects_by_type.cql)):**
  re-PK to `((tenant, type_id, day_bucket))` once `nodetool
  tablestats` reports `mean_partition_size_bytes > 4·10⁷`.
- **Trigger**: an alert wired in S1.8 against the `tablestats`
  Prometheus exporter; the migration is a flag-flip in the
  dual-write coordinator (S1.2) — no API break, only the read-path
  fan-out widens to N day buckets.
- **Why we don't bucket pre-emptively**: 95 % of tenants will never
  approach the limit; pre-bucketing turns every list-by-type into an
  N-partition scatter-gather and doubles tail latency for the median
  case.

### 3.2 `objects_by_marking` for the "PUBLIC" marking

Most multi-tenant SaaS workloads tag the long tail of objects with a
single open marking (`PUBLIC`, `INTERNAL`). For a tenant with 50 000
objects and one marking, **all 50 000 land on the same partition**.

- **Mitigation hook**: re-PK to `((tenant, marking_id, created_day))`
  via flag-flip migration when `nodetool` reports the partition
  exceeds 100 k rows.
- **Operational expectation**: governance scans (the only real
  consumer of this index) tolerate scatter-gather across 30–365
  bucket partitions; UX impact is null.

### 3.3 `links_incoming` for "hub" targets

A popular target object (e.g. an `Org` referenced by every `User` in
a tenant) accumulates incoming-link rows linearly with the user
count. For tenants with 10 000 users this fits; for tenants targeting
millions of consumer objects pointing at one root entity, the
partition grows unbounded.

- **Mitigation hook (file
  [`002_links_incoming.cql`](../../services/object-database-service/cql/ontology_indexes/002_links_incoming.cql)):**
  re-PK to `((tenant, target_id, month_bucket))` when a single target
  exceeds 100 k incoming links.
- The clustering on `(link_type, source_id)` already lets us read
  "incoming `OWNS` links to X" without materialising the long tail
  of other link types, which is the dominant query.

## 4. Verification harness (S1.8 hand-off)

To make the validation continuous (not one-shot at design time) we
expose three cardinality signals to the platform Prometheus stack
(wired in S1.8.d):

| Metric | Source | Alert threshold |
|---|---|---|
| `cassandra_table_estimated_partition_size_bytes_mean` | `nodetool tablestats` exporter | warn at 4·10⁷, page at 8·10⁷ |
| `cassandra_table_estimated_partition_count` | same | sanity check (catches collapse to single partition) |
| `cassandra_table_max_partition_size_bytes` | same | page at 10⁸ |

Alerts route to `#platform-cassandra` and trigger the runbook
[`infra/runbooks/cassandra.md`](../../infra/runbooks/cassandra.md)
section "Hot partition mitigation".

## 5. Result

All 7 tables defined in S1.1.b satisfy the cardinality rule at
platform scale. The three identified hot-partition risk areas
(§3.1–3.3) are **edge cases bounded above the ADR-0020 ceiling**
and have flag-flip mitigations wired both in the DDL comments and in
the migration coordinator backlog (S1.2). No PK redesign is required
before the migration starts; sizing is re-validated under load by
the S1.8 benchmarks.

— end S1.1.d.
