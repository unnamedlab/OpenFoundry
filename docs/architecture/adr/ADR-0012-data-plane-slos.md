# ADR-0012: Data-plane SLOs, SLIs and error budgets

- **Status:** Accepted
- **Date:** 2026-04-29
- **Deciders:** OpenFoundry platform architecture group
- **Related work:**
  - `docs/architecture/adr/ADR-0007-search-engine-choice.md` (Vespa as the
    sole production search engine).
  - `infra/k8s/clickhouse/`, `infra/k8s/vespa/`, `infra/k8s/strimzi/`,
    `infra/k8s/flink/`, `infra/k8s/trino/` — the Kubernetes packaging of the
    data-plane components covered by this ADR.
  - `infra/docker-compose.monitoring.yml` — Prometheus / Grafana / Loki /
    Tempo stack used to compute the SLIs below.

## Context

The OpenFoundry data plane spans several stateful systems (Flight SQL caches,
Iceberg + DataFusion, Kafka via Strimzi, ClickHouse, Vespa, NATS for control
events). Each of them already exposes Prometheus metrics through the
monitoring stack defined in `infra/docker-compose.monitoring.yml` and the
Helm charts under `infra/k8s/**`, but there is no single document that:

- states the **target latencies** per layer and per percentile (p50, p99,
  p99.9),
- names the **exact Prometheus metric / query** used to compute each SLI,
- defines the **monthly error budget** and the **freeze policy** when it is
  consumed,
- enumerates the **Grafana dashboards** that must exist to make the SLOs
  observable.

Without this, on-call engineers and capacity planners have to re-derive
acceptable latency from individual runbooks, which produces inconsistent
alert thresholds and makes regression detection unreliable.

## Decision

We adopt the following SLOs, SLIs, error budgets and dashboard inventory for
the data plane. They apply to all production deployments packaged from
`infra/k8s/**`. Local Compose stacks
(`infra/docker-compose.yml`, `infra/docker-compose.dev.yml`) are
**explicitly out of scope** — they are sized for DX, not for SLO compliance.

### 1. Latency SLOs per data-plane layer

All latencies are end-to-end at the component boundary unless stated
otherwise. "Hot dataset" means the working set fits in the component's
in-memory cache. "Intra-AZ" means producer and broker share an availability
zone. Measurement window for each percentile is **rolling 30 days**.

| # | Layer / operation | p50 | p99 | p99.9 |
|---|---|---|---|---|
| 1 | **Flight SQL point query** — cache hit, hot dataset | < 3 ms | **< 20 ms** | < 60 ms |
| 2 | **Iceberg scan, 1 GB, single-node DataFusion** | < 400 ms | **< 1.5 s** | < 4 s |
| 3 | **Kafka producer ack** — `acks=all`, intra-AZ | < 5 ms | **< 25 ms** | < 80 ms |
| 4 | **ClickHouse dashboard query** | < 40 ms | **< 200 ms** | < 600 ms |
| 5 | **Vespa hybrid query, top-50** (BM25 + vector + filter) | < 15 ms | **< 80 ms** | < 250 ms |
| 6 | **NATS control event end-to-end** (publish → subscriber handler entry) | < 1 ms | **< 5 ms** | < 15 ms |

The **bold p99 targets are the load-bearing SLO**: alerting, error budgets
and freeze decisions are computed against them. p50 and p99.9 targets are
informational guard-rails used to detect "long tail growing" and
"bimodal regression" patterns respectively.

### 2. SLIs — exact Prometheus metrics and queries

Each SLI is expressed as a PromQL query. Histograms are assumed to use the
component's stock buckets. If a component does not yet expose the histogram
named below, exposing it is a prerequisite to claiming SLO compliance for
that layer (tracked as part of the rollout for this ADR).

#### 2.1 Flight SQL point query (cache hit)

- **Histogram:** `flight_sql_query_duration_seconds_bucket`
  (label `cache="hit"`, `query_kind="point"`).
- **p99 SLI:**
  ```promql
  histogram_quantile(
    0.99,
    sum by (le) (
      rate(flight_sql_query_duration_seconds_bucket{cache="hit",query_kind="point"}[5m])
    )
  )
  ```
- **Good/total ratio (for error budget):**
  ```promql
  sum(rate(flight_sql_query_duration_seconds_bucket{cache="hit",query_kind="point",le="0.020"}[30d]))
  /
  sum(rate(flight_sql_query_duration_seconds_count{cache="hit",query_kind="point"}[30d]))
  ```

#### 2.2 Iceberg scan, 1 GB, single-node DataFusion

- **Histogram:** `datafusion_iceberg_scan_duration_seconds_bucket`
  (label `bytes_bucket="1g"`, `nodes="1"`).
- **p99 SLI:**
  ```promql
  histogram_quantile(
    0.99,
    sum by (le) (
      rate(datafusion_iceberg_scan_duration_seconds_bucket{bytes_bucket="1g",nodes="1"}[5m])
    )
  )
  ```
- **Good/total ratio:** same shape as 2.1 with `le="1.5"`.

#### 2.3 Kafka producer ack (`acks=all`, intra-AZ)

- **Histogram (Strimzi / kafka-exporter):**
  `kafka_producer_request_latency_seconds_bucket`
  (label `acks="all"`, `topology="intra_az"`).
- **p99 SLI:**
  ```promql
  histogram_quantile(
    0.99,
    sum by (le) (
      rate(kafka_producer_request_latency_seconds_bucket{acks="all",topology="intra_az"}[5m])
    )
  )
  ```
- **Good/total ratio:** as 2.1 with `le="0.025"`.

#### 2.4 ClickHouse dashboard query

- **Histogram:** `clickhouse_query_duration_seconds_bucket`
  (label `workload="dashboard"`). The label is set by the query router /
  user profile in `infra/k8s/clickhouse/`.
- **p99 SLI:**
  ```promql
  histogram_quantile(
    0.99,
    sum by (le) (
      rate(clickhouse_query_duration_seconds_bucket{workload="dashboard"}[5m])
    )
  )
  ```
- **Good/total ratio:** as 2.1 with `le="0.200"`.

#### 2.5 Vespa hybrid query, top-50

- **Histogram:** `vespa_query_latency_seconds_bucket`
  (label `query_profile="hybrid_top50"`).
- **p99 SLI:**
  ```promql
  histogram_quantile(
    0.99,
    sum by (le) (
      rate(vespa_query_latency_seconds_bucket{query_profile="hybrid_top50"}[5m])
    )
  )
  ```
- **Good/total ratio:** as 2.1 with `le="0.080"`.

#### 2.6 NATS control event end-to-end

- **Histogram:** `nats_control_event_e2e_seconds_bucket`
  (exposed by the OpenFoundry control-plane SDK at the subscriber side, with
  the publish timestamp propagated as a header). Label `subject_class="control"`.
- **p99 SLI:**
  ```promql
  histogram_quantile(
    0.99,
    sum by (le) (
      rate(nats_control_event_e2e_seconds_bucket{subject_class="control"}[5m])
    )
  )
  ```
- **Good/total ratio:** as 2.1 with `le="0.005"`.

### 3. Error budgets (monthly) and freeze policy

Each SLO is converted into a monthly error budget computed as
`1 - SLO_target`, where `SLO_target` is the fraction of requests in the
30-day window that must complete under the p99 latency bound listed in
section 1. The default availability target per SLI is **99.5 %** of
requests under the p99 latency, i.e. an error budget of **0.5 % of
requests per 30-day window**.

| Layer | SLO target (under p99 bound) | Monthly error budget |
|---|---|---|
| Flight SQL point query | 99.5 % under 20 ms | 0.5 % of point queries |
| Iceberg scan 1 GB | 99.0 % under 1.5 s | 1.0 % of scans |
| Kafka producer ack | 99.5 % under 25 ms | 0.5 % of produced records |
| ClickHouse dashboard query | 99.5 % under 200 ms | 0.5 % of dashboard queries |
| Vespa hybrid top-50 | 99.5 % under 80 ms | 0.5 % of hybrid queries |
| NATS control event | 99.9 % under 5 ms | 0.1 % of control events |

Iceberg scans use a relaxed 99.0 % target because long-tail behaviour is
dominated by object-store cold reads, which are partially outside the
platform's control.

#### Freeze policy

The freeze policy is evaluated **per SLI**, not globally:

1. **Burn-rate alert (page):** if the 1-hour burn rate exceeds **14.4×**
   *and* the 6-hour burn rate exceeds **6×** the budget consumption rate,
   an on-call page fires for that layer.
2. **Soft freeze (>= 50 % of monthly budget consumed):** non-urgent
   changes that touch the affected layer (schema migrations, ranking
   profile changes, broker config changes, ClickHouse merges tuning,
   Vespa application package deploys, NATS subject restructuring) require
   an explicit reviewer from the platform team.
3. **Hard freeze (>= 90 % of monthly budget consumed):** all
   non-reliability changes to the affected layer are blocked until the
   end of the rolling 30-day window or until reliability work brings the
   burn rate back under 1× for at least 24 h. Reliability work, rollbacks
   and security fixes are always allowed.
4. **Budget exhausted (100 %):** in addition to the hard-freeze rules,
   the on-call team writes a post-incident note under
   `infra/runbooks/` and the next ADR review cycle must consider
   tightening or loosening the SLO based on observed reality.

The freeze policy is enforced procedurally (review + dashboards), not by
CI gating, to avoid coupling unrelated services to the budget of one
component.

### 4. Grafana dashboards to create

The following dashboards must exist and be provisioned alongside the
existing monitoring stack in `infra/docker-compose.monitoring.yml` and the
Helm releases under `infra/k8s/**`. Each dashboard renders the p50 / p99 /
p99.9 panels and the error-budget burn-down for its SLI.

| Dashboard | UID (proposed) | Backing component / chart |
|---|---|---|
| Data Plane SLO Overview | `dp-slo-overview` | aggregates the six SLIs above |
| Flight SQL — point query SLO | `dp-slo-flightsql` | Flight SQL service metrics |
| DataFusion / Iceberg scan SLO | `dp-slo-datafusion` | DataFusion + Iceberg scan metrics |
| Kafka producer ack SLO | `dp-slo-kafka` | `infra/k8s/strimzi/` |
| ClickHouse dashboard query SLO | `dp-slo-clickhouse` | `infra/k8s/clickhouse/` |
| Vespa hybrid query SLO | `dp-slo-vespa` | `infra/k8s/vespa/` |
| NATS control event SLO | `dp-slo-nats` | NATS control plane |

The "Data Plane SLO Overview" dashboard is the single pane used by on-call
to decide whether a freeze is in effect.

## Consequences

### Positive

- One canonical source of truth for data-plane latency targets, replacing
  per-runbook ad-hoc thresholds.
- Each SLI is bound to a concrete Prometheus histogram, which makes
  regressions and missing instrumentation immediately visible (a missing
  histogram = an unmeasurable SLO).
- The freeze policy gives the platform team a predictable lever to slow
  down change velocity on a struggling component without stopping the
  whole org.

### Negative / trade-offs

- Components that do not yet expose the histograms named in section 2
  (notably the NATS end-to-end histogram) need additional instrumentation
  before their SLO can be claimed as met.
- A 30-day rolling window smooths out short incidents; teams must rely on
  the burn-rate alert (1 h / 6 h) for fast feedback.

### Migration / rollout

- Add the histograms listed in section 2 wherever they are missing; this
  is a prerequisite for declaring any SLO "live".
- Provision the seven Grafana dashboards listed in section 4 next to the
  existing monitoring stack.
- Link this ADR from `docs/operations/index.md` so on-call engineers can
  find it from the operations entry point.

## Conditions under which this decision would be reopened

This ADR should be revisited if **any** of the following becomes true:

1. A component is replaced (e.g. a different OLAP engine instead of
   ClickHouse, or a different streaming substrate instead of Kafka),
   invalidating the corresponding histogram and SLI.
2. Production traffic shape changes such that the chosen percentiles
   (p50 / p99 / p99.9) no longer represent user-visible behaviour
   (for example if batch traffic dominates and a different latency bound
   is needed per workload class).
3. The error budget for any layer is exhausted for **two consecutive
   months**, indicating the SLO is unrealistic for the current
   architecture and should be re-negotiated rather than chronically
   missed.

## References

- `infra/k8s/clickhouse/`, `infra/k8s/vespa/`, `infra/k8s/strimzi/`,
  `infra/k8s/flink/`, `infra/k8s/trino/` — data-plane component packaging.
- `infra/docker-compose.monitoring.yml` — Prometheus / Grafana / Loki /
  Tempo stack used to host the dashboards above.
- `docs/operations/index.md` — operations entry point, links to this ADR.
- `docs/architecture/adr/ADR-0007-search-engine-choice.md` — Vespa as the
  sole production search engine, on which SLI 2.5 depends.
