# ADR-0009: Internal query fabric — DataFusion + Flight SQL (Trino as edge BI only)

- **Status:** Superseded by [ADR-0014](./ADR-0014-retire-trino-flight-sql-only.md)
- **Date:** 2026-04-29
- **Superseded:** 2026-04-30 — Trino is **removed** entirely; the edge BI surface
  is now `sql-bi-gateway-service` implemented as a real Apache Arrow Flight SQL
  server (DataFusion + multi-backend routing). The internal Flight SQL P2P
  posture from this ADR is **retained**; only the "Trino as edge BI" leg is
  superseded.
- **Deciders:** OpenFoundry platform architecture group
- **Related work:**
  - `libs/query-engine/` (DataFusion + `FlightSqlTableProvider`)
  - `services/sql-warehousing-service/` (DataFusion compute pool, port 50123)
  - `services/sql-bi-gateway-service/` (Apache Arrow Flight SQL gateway, port 50133)
  - [ADR-0014](./ADR-0014-retire-trino-flight-sql-only.md) — supersedes the
    Trino-as-edge-BI portion of this ADR
  - [ADR-0007](./ADR-0007-search-engine-choice.md) — Vespa-only search posture
    (precedent for collapsing overlapping stateful backends)

## Context

OpenFoundry currently exposes **two overlapping SQL hubs** for analytical
queries:

- **Trino**, deployed internally under `infra/k8s/trino/*` (coordinator + worker
  pods, Iceberg/PostgreSQL/Kafka catalogs). Trino is a federated query engine
  with its own SQL dialect and a coordinator that plans every query.
- **DataFusion + Flight SQL**, provided by `libs/query-engine/` and operated by
  `services/sql-warehousing-service` (port 50123). The library exposes a
  `FlightSqlTableProvider` that lets any service consume another service's
  result set as a DataFusion table over Arrow Flight SQL, peer-to-peer.

Keeping both as first-class options for **internal, service-to-service**
queries has three concrete costs:

1. **Two SQL dialects** to support inside the platform (Trino SQL and
   DataFusion SQL). Service authors pick one and either lose pushdown when
   crossing a boundary or have to translate predicates/types by hand.
2. **A logical SPOF.** Every internal query that goes through Trino is planned
   on the Trino coordinator. Even with `coordinator.replicas: 2` and the
   experimental coordinator HA configured in `infra/k8s/trino/values.yaml`,
   the coordinator is still a single chokepoint per query, with its own
   failure modes (planner OOMs, leader elections, catalog refresh storms)
   that block service-to-service traffic that does **not** need a federated
   planner at all.
3. **Extra latency.** A service-to-service join that could be a single Flight
   SQL pull from a peer instead pays an extra hop through the Trino
   coordinator and worker pool, plus dialect translation on both ends.

At the same time, Trino is genuinely the right tool for **external BI**
clients: heterogeneous JDBC/ODBC consumers (Tableau, Superset, ad-hoc SQL
notebooks) that expect a stable Trino-shaped surface across catalogs and
benefit from its mature connector ecosystem.

## Decision

We split the SQL surface into an **internal query fabric** and an **edge BI
surface**, and we make Trino exclusively part of the latter.

### 1. Internal queries use Flight SQL P2P only

Service-to-service SQL inside the platform goes **exclusively** over Flight
SQL using `FlightSqlTableProvider` from `libs/query-engine/`. Services that
need to read another service's result set register it as a DataFusion table
and plan locally. There is **no internal hop through a central coordinator**
for these queries.

### 2. `sql-warehousing-service` is the official DataFusion compute pool

`services/sql-warehousing-service` (port `50123`) is the canonical DataFusion
compute pool for the platform. Workloads that need shared CPU/memory for
DataFusion execution (e.g. larger Iceberg scans, expensive joins that should
not run inside a small business service) target `sql-warehousing-service`
over Flight SQL rather than instantiating their own ad-hoc DataFusion
runtimes.

### 3. `sql-bi-gateway-service` is the edge SQL router

`services/sql-bi-gateway-service` (port `50133`) is the **edge SQL** entry
point. It routes incoming SQL based on the target backend:

| Workload                              | Backend                                  |
| ------------------------------------- | ---------------------------------------- |
| Iceberg / lakehouse analytical SQL    | `sql-warehousing-service` (DataFusion)   |
| Time-series queries                   | ClickHouse                               |
| Search / hybrid retrieval             | Vespa (cf. [ADR-0007](./ADR-0007-search-engine-choice.md)) |
| External BI over JDBC/ODBC            | Trino                                    |

### 4. Trino is kept, but only as edge BI

Trino under `infra/k8s/trino/` is **retained**, with its existing chart and
catalog ConfigMaps **unchanged by this ADR**, but its role is narrowed:

- Trino is the **edge BI** surface for Tableau, Superset and other
  heterogeneous JDBC/ODBC clients that need a stable federated SQL dialect
  across catalogs.
- Trino is **not** an internal query hub. No new service may declare a
  dependency on Trino for service-to-service SQL.

## Consequences

### Positive

- **No central coordinator on the internal path.** Removing the Trino
  coordinator from the inter-service hot path eliminates a logical SPOF for
  internal queries; failure of the Trino coordinator no longer blocks
  service-to-service SQL.
- **One internal dialect.** All internal SQL is DataFusion SQL, so pushdown,
  type handling and predicate semantics are consistent across services.
- **Lower latency.** Service-to-service queries become a direct Flight SQL
  pull instead of a coordinator-mediated federated plan.
- **Clear role for Trino.** Trino's JDBC/ODBC connector ecosystem is preserved
  exactly where it pays off (external BI), without forcing every internal
  service to share its dialect or its coordinator.
- **Smaller blast radius for Trino changes.** Coordinator upgrades, catalog
  refreshes and planner regressions no longer impact internal services.

### Negative / trade-offs

- Federated joins that span heterogeneous catalogs and were previously
  expressed as a single Trino query must now be expressed either through
  `sql-bi-gateway-service` (which routes per backend) or as a DataFusion
  plan that uses `FlightSqlTableProvider` to pull the remote side.
- Service authors used to Trino SQL must learn DataFusion SQL semantics for
  internal work. Trino SQL remains valid, but only for queries arriving
  through the edge BI surface.

## Migration plan

- **No new internal Trino dependency.** No new service may declare a runtime
  dependency on Trino for internal queries. New internal SQL paths must use
  `libs/query-engine/` (Flight SQL) and, where shared compute is needed,
  `sql-warehousing-service`.
- **Existing internal Trino dependencies** (services that today reach the
  Trino coordinator for service-to-service SQL) are documented in a
  **pending migration table** maintained alongside this ADR. Each entry
  records the service, the query path, and the target Flight SQL / DataFusion
  replacement. Until the entry is migrated, the existing path keeps working.
- **External BI paths are out of scope** for migration: Tableau / Superset /
  JDBC/ODBC clients keep using Trino through its existing endpoint.
- **Infrastructure under `infra/k8s/trino/`** (Helm values, catalog
  ConfigMaps, PDB) is **not changed** by this ADR. Only the `README.md`
  positioning is updated to reflect the new "edge BI" role.

### Pending internal-Trino migrations

| Service | Current internal query path | Target replacement | Status |
| --- | --- | --- | --- |
| _none recorded yet_ | — | — | — |

Entries are added as audits of existing services surface concrete internal
Trino dependencies.

## References

- `libs/query-engine/` — DataFusion + `FlightSqlTableProvider`.
- `services/sql-warehousing-service/` — DataFusion compute pool (port 50123).
- `services/sql-bi-gateway-service/` — edge SQL router (port 50133).
- `infra/k8s/trino/README.md` — Trino deployment, now scoped to edge BI.
- `docs/architecture/runtime-topology.md` — runtime topology, "Internal query
  fabric" section.
- [ADR-0007](./ADR-0007-search-engine-choice.md) — precedent for collapsing
  overlapping stateful backends.
