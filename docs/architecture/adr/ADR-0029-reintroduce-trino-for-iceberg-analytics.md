# ADR-0029: Reintroduce Trino for Iceberg analytics

- **Status:** Accepted
- **Date:** 2025-S5 (week 13)
- **Supersedes (in part):** [ADR-0014](ADR-0014-retire-trino-flight-sql-only.md)
- **Related:** S5.6 of [migration plan](../migration-plan-cassandra-foundry-parity.md)

## Context

ADR-0014 (Accepted) retired Trino in favour of a single Flight SQL
edge gateway (`sql-bi-gateway-service`) backed by DataFusion plus
optional remote Flight SQL endpoints (`sql-warehousing-service`,
Vespa, Postgres). That decision was right for the
Sprint-3 timeframe: it removed an operational liability and let us
collapse OLAP, search and OLTP behind one wire protocol.

Sprint 5 changes the picture in two ways:

1. The Iceberg lakehouse is now the system-of-record for **3
   high-cardinality namespaces** — `of_lineage`, `of_ai`,
   `of_metrics_long` — and the Lakekeeper REST catalog (S5.2) is the
   canonical metadata server.
2. DataFusion is excellent for Flight SQL probes and small
   federations but cannot, today, plan multi-TB joins across
   `iceberg.*` namespaces with the Iceberg-native cost model that
   Trino's `iceberg.*` connector ships.

We therefore need an **analytical** engine that:

- speaks the Iceberg REST catalog protocol natively;
- supports cross-namespace joins and time-windowed aggregates with a
  cost-based optimiser;
- runs as a Kubernetes-native workload (HA coordinator, autoscaling
  workers).

Trino fits all three. Apache Spark also fits but Spark is now scoped
to **batch maintenance** jobs (S5.7) — not interactive query.

## Decision

Reintroduce Trino as the **Iceberg-analytics engine**, deployed via
`infra/k8s/platform/manifests/trino/values.yaml` (S5.6.a/b/c). Keep Flight SQL as the
**edge protocol**: BI clients still connect to
`sql-bi-gateway-service:50133` and the gateway delegates by catalog
prefix.

This ADR being `Accepted` does **not** by itself close Stream S5. The
stream closes only once Trino and the lakehouse sinks are live in
runtime and gate `G-S5` in the migration plan is green.

Concretely the routing table inside `services/sql-bi-gateway-service/src/routing.rs`
gains a fifth backend:

| Catalog prefix in SQL | Backend | Endpoint |
|-----------------------|---------|----------|
| `trino.*`             | `Backend::Trino` | `trino-flight-sql-proxy.trino:50133` (Flight SQL adapter in front of Trino's HTTP API) |
| `iceberg.*` (default) | `Backend::Iceberg` | local DataFusion / `sql-warehousing-service` |
| `vespa.*`             | `Backend::Vespa` | unchanged |
| `postgres.*` / `postgresql.*` | `Backend::Postgres` | unchanged |

**OLTP** statements (anything addressing Cassandra hot-path or
Postgres reference data) keep going to those backends directly.
**OLAP** analytical statements that explicitly target `trino.*`
(typically multi-namespace joins) are delegated to Trino. The
classifier remains a pure prefix match — no parser, no metadata
fetch.

## What ADR-0014 still owns

- Single Flight SQL edge protocol for BI clients — unchanged.
- DataFusion as the local execution engine for `SELECT 1` probes and
  small federations — unchanged.
- The decision to never expose Trino's HTTP API directly to BI
  clients — unchanged. The Flight SQL adapter in front is the only
  ingress.

## Operational impact

- Two more StatefulSets (Trino coordinator HA pair) + a HPA-managed
  worker Deployment.
- One new Helm chart dependency (`trinodb/trino`, Apache-2.0) bumped
  in the split Helm profile set under `infra/k8s/helm/profiles/` (deferred to runtime
  PR).
- `sql-bi-gateway-service` gains an additional optional env var
  `TRINO_FLIGHT_SQL_URL`. When unset the gateway returns
  `RoutingError::BackendUnavailable(Backend::Trino)` for `trino.*`
  statements (same pattern as Vespa / Postgres).

## Consequences

- Higher steady-state cluster footprint (~6 cores / 24Gi for the
  coordinators alone).
- Faster analytical queries over Iceberg. Internal benchmark target:
  any `service_metrics_daily` 30-day rollup ≤ 3 s P95.
- Spark workloads stay batch-only (S5.7). Any operator that
  authors interactive queries against Spark is doing it wrong.

## Rollback

Revert the gateway change (`Backend::Trino` arm) and scale the Trino
coordinator/worker StatefulSets to zero. `sql-warehousing-service`
keeps serving Iceberg traffic, with the known limitation on cross-TB
joins.
