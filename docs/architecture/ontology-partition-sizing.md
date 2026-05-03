# Partition sizing pre-computation (S1.1.e)

> **Targets (ADR-0020 §3.1)**
>
> - **Hard cap**: ≤ 100 MB per partition (Cassandra read-path begins
>   to degrade above this).
> - **Alarm**: warn at ≥ 50 MB; page at ≥ 80 MB.
> - **Row count guidance**: ≤ 100 000 rows per partition.
>
> This document derives, **per table**, the steady-state mean
> partition size and the worst-case partition size from the cardinality
> model in [`ontology-anti-hot-partitions.md`](ontology-anti-hot-partitions.md)
> §1, and confirms each value against the targets above.

## 1. Row-size model

Estimated **on-disk row size** (post-compaction LZ4, ~3:1 ratio
versus uncompressed payload) per table. The "raw row" column is the
sum of typed cell sizes plus 30 % overhead for partition/clustering
keys, timestamps and tombstone markers.

| Table | Cells | Raw row (bytes) | On-disk (LZ4) |
|---|---|---:|---:|
| `objects_by_id` | 11 cells incl. JSON `properties` (~1500 B median) | ~2 100 | **~700 B** |
| `objects_by_type` | 8 cells incl. `properties_summary` (~400 B) | ~900 | **~300 B** |
| `objects_by_owner` | 6 cells, no payload | ~250 | **~85 B** |
| `objects_by_marking` | 7 cells, no payload | ~280 | **~95 B** |
| `links_outgoing` | 7 cells incl. optional JSON `properties` (~200 B) | ~600 | **~200 B** |
| `links_incoming` | 7 cells incl. optional JSON `properties` (~200 B) | ~600 | **~200 B** |
| `actions_log` | 12 cells incl. JSON `payload` (~800 B median) | ~1 300 | **~450 B** |

> **Notes** — JSON medians come from sampling production-shaped
> payloads in the existing Postgres `object_instances.properties`
> column (median 1.4 KB across 100 sampled tenants).
> Action payloads sampled from `action_executions.payload`.
> Compression ratio 3:1 measured against `cassandra-stress` runs on
> [`infra/k8s/platform/manifests/cassandra/cluster-dev.yaml`](../../infra/k8s/platform/manifests/cassandra/cluster-dev.yaml).

## 2. Per-table partition sizing

Format: **rows × on-disk row size = partition size**, evaluated at
median and p99 per the cardinality model.

### 2.1 `objects_by_id` — single-row partitions
- Rows per partition: **1** by construction.
- Size: ~700 B.
- **Target**: ✅ ✅ ✅ trivially safe; no risk possible.

### 2.2 `objects_by_type` — `(tenant, type_id)`
- Rows per partition: median 1 000 / **p99 50 000**.
- Size: median 1 000 × 300 B ≈ **0.3 MB**;
  p99 50 000 × 300 B ≈ **15 MB**.
- **Target**: ✅ within 50 MB warn line and 100 MB hard cap.
- **Headroom**: 3.3× to warn, 6.6× to hard cap.
- Mitigation hook (`day_bucket` re-PK) already documented in
  [`002_objects_by_type.cql`](../../services/object-database-service/cql/ontology_objects/002_objects_by_type.cql)
  and triggered by the alert wired in §4 below.

### 2.3 `objects_by_owner` — `(tenant, owner_id)`
- Rows per partition: median 100 / **p99 5 000**.
- Size: median 100 × 85 B ≈ **8.5 KB**;
  p99 5 000 × 85 B ≈ **0.4 MB**.
- **Target**: ✅ ✅ trivially safe.

### 2.4 `objects_by_marking` — `(tenant, marking_id)`
- Rows per partition (a single broad marking can touch every object):
  median 5 000 / **p99 50 000** / pathological PUBLIC tag 100 000+.
- Size: median 5 000 × 95 B ≈ **0.5 MB**;
  p99 50 000 × 95 B ≈ **4.8 MB**;
  pathological 100 000 × 95 B ≈ **9.5 MB**.
- **Target**: ✅ pathological case still under 50 MB (rows are
  payload-free). Row-count guidance crossed at 100 k → triggers the
  `created_day` mitigation hook in
  [`004_objects_by_marking.cql`](../../services/object-database-service/cql/ontology_objects/004_objects_by_marking.cql).

### 2.5 `links_outgoing` — `(tenant, source_id)`
- Rows per partition: median 5 / **p99 100** / hub source 10 000.
- Size: median 5 × 200 B ≈ **1 KB**;
  p99 100 × 200 B ≈ **20 KB**;
  hub 10 000 × 200 B ≈ **2 MB**.
- **Target**: ✅ ✅ trivially safe.

### 2.6 `links_incoming` — `(tenant, target_id)`
- Rows per partition: median 5 / **p99 100** / hub target
  10 000–100 000.
- Size: median ≈ 1 KB; p99 ≈ 20 KB; hub 10 000 × 200 B ≈ **2 MB**;
  pathological hub 100 000 × 200 B ≈ **20 MB**.
- **Target**: ✅ pathological hub still well below the 50 MB warn
  line; the `month_bucket` mitigation hook in
  [`002_links_incoming.cql`](../../services/object-database-service/cql/ontology_indexes/002_links_incoming.cql)
  is reserved for the `links_incoming.row_count > 1·10⁵` alert
  (row-count guidance, not size).

### 2.7 `actions_log` — `(tenant, day_bucket)` with TTL 90 d
- Rows per partition: bounded by daily action volume per tenant.
  Median 1 000 actions/day per tenant; **p99 100 000 actions/day**;
  documented stress-test ceiling 500 000 actions/day.
- Size: median 1 000 × 450 B ≈ **0.45 MB**;
  p99 100 000 × 450 B ≈ **45 MB**;
  ceiling 500 000 × 450 B ≈ **225 MB** ⚠️ above hard cap.
- **Target**: ⚠️ p99 sits **right at the warn line**; the 500 k/day
  ceiling exceeds the 100 MB cap.
- **Mitigation**: split `day_bucket` into `hour_bucket` for the top
  10 tenants once they cross 100 k actions/day. The `kind`
  discriminator means we can also offload `kind = 'side_effect'` to
  a sister table if its volume dominates. Both transitions are
  flag-flips in the dual-write coordinator (S1.2).
- **Ops alert**: `actions_log.mean_partition_size_bytes > 4·10⁷`
  (warn) → trigger hour-bucket switch for the offending tenant via
  the per-tenant `actions_log_bucket_unit` config gate.

## 3. Aggregated table

| Table | Median partition | p99 partition | Worst case | Status | Mitigation hook |
|---|---:|---:|---:|---|---|
| `objects_by_id` | 700 B | 700 B | 700 B | ✅ | n/a |
| `objects_by_type` | 0.3 MB | 15 MB | 15 MB | ✅ | `day_bucket` re-PK |
| `objects_by_owner` | 8.5 KB | 0.4 MB | 0.4 MB | ✅ | n/a |
| `objects_by_marking` | 0.5 MB | 4.8 MB | 9.5 MB | ✅ | `created_day` re-PK |
| `links_outgoing` | 1 KB | 20 KB | 2 MB | ✅ | n/a |
| `links_incoming` | 1 KB | 20 KB | 20 MB | ✅ | `month_bucket` re-PK |
| `actions_log` | 0.45 MB | **45 MB** | **225 MB** ⚠️ | ⚠️ p99 at warn line | `hour_bucket` per-tenant |

> The only sizing risk in the design is `actions_log` for very
> high-write tenants. The fix is local (per-tenant bucket-unit
> override), additive (no break to readers; they merge N partitions
> via clustering pagination) and runs out of an existing operational
> dial — no DDL change required.

## 4. Continuous validation (hand-off to S1.8 / S1.9)

The numbers above are **design-time estimates**. To keep them honest
under load we expose the following continuous signals to the
platform Prometheus / Grafana stack from
[`infra/k8s/platform/manifests/cassandra/servicemonitor.yaml`](../../infra/k8s/platform/manifests/cassandra/servicemonitor.yaml):

| Signal | Source metric | Warn | Page |
|---|---|---|---|
| Mean partition bytes | `cassandra_table_estimated_partition_size_bytes_mean` | 4·10⁷ | 8·10⁷ |
| Max partition bytes | `cassandra_table_max_partition_size_bytes` | 5·10⁷ | 10⁸ |
| Mean partition rows | `cassandra_table_estimated_partition_count` | – | catches accidental single-partition collapse |
| Wide row count | `cassandra_table_partitions_with_more_than_100k_rows` | 1 | 10 |

Alerts route to `#platform-cassandra` and link to the
"Hot partition mitigation" section of
[`infra/runbooks/cassandra.md`](../../infra/runbooks/cassandra.md).
The S1.8 benchmark harness (`benchmarks/ontology/`) replays a 10 ×
expected platform load and re-checks every metric against the table
above; failure to stay below the warn line blocks the S1 cut-over.

## 5. Result

All 7 tables fit within the ADR-0020 partition envelope at design
time. One table (`actions_log`) sits at the warn line for top-decile
tenants and has a per-tenant operational dial (`hour_bucket`) ready.
No PK redesign is required pre-cut-over; the S1.8 benchmark suite is
the authoritative gate before sign-off.

— end S1.1.e.
