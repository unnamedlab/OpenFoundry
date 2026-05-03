# ADR-0014: Retire Trino, single Flight SQL edge gateway

- **Status:** Accepted
- **Date:** 2026-04-30
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes:** the "Trino as edge BI only" leg of
  [ADR-0009](./ADR-0009-internal-query-fabric-datafusion-flightsql.md). The
  internal Flight SQL P2P posture from ADR-0009 is **retained**.
- **Related work:**
  - `services/sql-bi-gateway-service/` (Apache Arrow Flight SQL gateway,
    port `50133` Flight SQL gRPC + `50134` HTTP side router)
  - `services/sql-warehousing-service/` (DataFusion compute pool,
    Flight SQL P2P, port `50123`)
  - `libs/query-engine/` (DataFusion + `FlightSqlTableProvider`)
  - `libs/auth-middleware/` (JWT decoding + per-tenant quota policy)
  - `infra/k8s/platform/manifests/lakekeeper/` — lakehouse catalog deployment
    addressed via per-statement routing
  - Deletions: `infra/k8s/platform/manifests/trino/`, `infra/runbooks/trino.md`

## Context

[ADR-0009](./ADR-0009-internal-query-fabric-datafusion-flightsql.md)
removed Trino from the **internal** SQL fabric (service-to-service SQL
travels over Apache Arrow Flight SQL P2P) but kept Trino as the
*"edge BI only"* gateway for external Tableau / Superset / JDBC clients.
Six months in, that residual Trino footprint is still expensive:

1. **A second SQL dialect** to support across docs, tests and BI
   dashboards (Trino SQL alongside DataFusion / Vespa / Postgres dialects).
2. **A second auth / quota / audit surface.** Tenant policies that we
   already implement in `libs/auth-middleware` (`TenantContext`,
   `TenantQuotaPolicy`) had to be re-encoded inside Trino's
   `access-control` plugin chain. Drift between the two surfaces is a
   recurring source of incidents.
3. **A second deployment to operate** (Trino coordinator + workers,
   secret rotation, Iceberg catalog properties, Helm values, PDBs) — roughly
   10 % of `infra/k8s/` purely for an
   external-facing read path.
4. **The `sql-bi-gateway-service` was a stub** (`fn main() {}`) that
   only existed to delegate to Trino. It already had partial
   scaffolding for saved queries, audit, JWT auth and migrations — all
   wasted as long as the actual edge surface was Trino.

Two solutions were considered (see problem statement that opened this
ADR): a thin proxy in `sql-bi-gateway-service` that still forwards to
Trino (Solution A), or a full Apache Arrow Flight SQL server inside the
gateway that subsumes Trino's edge role (Solution B). Solution B is the
"platform-grade" choice: federation is preserved at the external
boundary (Tableau still sees a single catalog tree) while collapsing
the auth / quotas / audit / saved-queries plane onto a single Rust
service we already own.

## Decision

1. **Implement `sql-bi-gateway-service` as a real Apache Arrow Flight
   SQL server** (`arrow-flight::sql::server::FlightSqlService`) on port
   `50133`, backed by DataFusion (`query-engine::QueryContext`). The
   service implements the Flight SQL verbs that Tableau / Superset and
   the Apache Arrow Flight SQL JDBC driver call during normal
   operation: `do_handshake`, `get_flight_info_catalogs`,
   `get_flight_info_schemas`, `get_flight_info_tables`,
   `get_flight_info_statement`, `get_flight_info_prepared_statement`,
   `do_get_statement` and `do_put_statement_update`.

2. **Per-statement backend routing.** Every SQL statement is classified
   by the catalog prefix of its first table reference and routed to:

   | Catalog prefix             | Backend (per ADR-0009 table)                                                          |
   | -------------------------- | ------------------------------------------------------------------------------------- |
   | `iceberg.*` / no prefix    | local DataFusion `SessionContext`, optionally delegated to `sql-warehousing-service`  |
   | `trino.*`                  | Trino via `TRINO_FLIGHT_SQL_URL`                                                      |
   | `vespa.*`                  | Vespa via `VESPA_FLIGHT_SQL_URL`                                                      |
   | `postgres.*` / `postgresql.*` | Postgres via `POSTGRES_FLIGHT_SQL_URL`                                             |

   Backends that are not configured cause the gateway to reject the
   statement at planning time with `failed_precondition` and a clear
   error message — no silent re-routing.

3. **Auth, quotas and audit on the Flight SQL surface.** JWTs are
   extracted from the gRPC `authorization: Bearer <token>` metadata and
   decoded with `auth_middleware::jwt`. The resulting
   `TenantContext` clamps the row count via `clamp_query_limit`. Every
   executed statement emits a structured `tracing` event under the
   `sql_bi_gateway.audit` target containing the tenant id, tier, user
   email, backend, remote-vs-local, SQL fingerprint, row count,
   duration and outcome — never the SQL payload itself.

4. **Saved queries on the HTTP side router.** A small `axum` server on
   port `50134` exposes `/healthz` (Kubernetes probes) and the
   `POST/GET/DELETE /api/v1/queries/saved` CRUD against the
   per-bounded-context Postgres cluster (migration
   `20260419100003_initial_queries.sql`).

5. **Catalog tree advertised to BI clients.** A single catalog
   `openfoundry` with one schema per backend (`iceberg`, `trino`,
   `vespa`, `postgres`) is returned via `GetCatalogs`/`GetSchemas`/
   `GetTables` so Tableau / Superset render a sensible navigator panel
   even before any backend metadata is wired in.

6. **Trino is deleted entirely.** The following are removed in this
   ADR's accompanying change-set:

   - `infra/k8s/platform/manifests/trino/` (coordinator, PDB, catalog ConfigMaps, values)
   - `infra/runbooks/trino.md`
   - All Trino references in `infra/k8s/platform/manifests/flink/maintenance/rewrite-data-files.yaml`,
     `infra/k8s/platform/observability/prometheus-rules/lakekeeper.yaml`,
     `docs/architecture/runtime-topology.md`,
     `docs/architecture/services-and-ports.md`,
     `docs/architecture/adr/ADR-0012-data-plane-slos.md`,
     `ROADMAP.md`.

7. **Tableau / Superset use the Apache Arrow Flight SQL JDBC driver**
   (Apache-2.0) pointed at `sql-bi-gateway-service:50133`. Connection
   string template:

   ```text
   jdbc:arrow-flight-sql://<host>:50133?useEncryption=false
   ```

   plus a JWT in the `authorization` header for production. No client
   changes are required vs. the Trino driver beyond the URL and the
   driver JAR.

## Consequences

### Positive

- **Single edge surface, single policy plane.** Tenant scoping,
  quotas, JWT validation and audit are owned by the same Rust crate
  that owns the data-plane bounded contexts; drift between Trino's
  access-control plugin and `auth-middleware` disappears.
- **Smaller infra footprint.** Trino's coordinator + workers (~1 vCPU
  / 2 GiB minimum, plus PDBs and Helm values) are gone; the gateway is
  a single Rust binary co-located with its CNPG cluster.
- **Single SQL dialect for new work.** Internal services already
  speak DataFusion SQL (per ADR-0009); BI now also reaches the same
  dialect on the local side, with routed engines (Trino, Vespa,
  Postgres) only relevant when explicitly using those catalog prefixes.
- **Observable, auditable end-to-end.** The Flight SQL surface emits
  one structured audit event per statement, including a deterministic
  SQL fingerprint that is safe to log.
- **Roadmap clear.** Column masking, row-level security and a query
  cache can be implemented at one well-defined plane in Rust.

### Negative / Risks

- **We re-implement small pieces of Trino.** The Flight SQL
  `GetCatalogs` / `GetSchemas` / `GetTables` plumbing, the
  routing-by-prefix planner and the row-quota enforcement are all
  things Trino does well. We accept this scope explicitly: the goal
  is to own the ~5 verbs Tableau actually uses, not to rebuild a
  generic federated query engine. Anything more ambitious (full
  cross-backend joins, cost-based planning) stays inside DataFusion,
  not the gateway.
- **Cross-backend joins are limited to the Iceberg/local DataFusion
  side**: a `JOIN` between, say, `trino.of_lineage.runs` and
  `vespa.documents` is **not** supported. Callers that need such joins
  must materialise into Iceberg via `sql-warehousing-service` first.
  The router selects a single backend per statement (the catalog of
  the first `FROM`/`JOIN`/`UPDATE` target).
- **`do_put_statement_update` returns `0` for affected-row count.**
  DataFusion does not surface a portable affected-row count; Flight
  SQL clients treat `0` as "unknown". This matches Trino JDBC
  behaviour for non-DML and is acceptable for BI workloads.

### Migration plan

1. Deploy `sql-bi-gateway-service:50133` alongside the existing Trino
   deployment.
2. Re-point Tableau / Superset workbooks to the Arrow Flight SQL JDBC
   driver and the new URL; verify the catalog navigator and the
   top-N production dashboards.
3. Delete the Trino Helm release and the `infra/k8s/platform/manifests/trino/` manifests
   once the cut-over has soaked for one error-budget window
   (cf. ADR-0012).
4. Remove Trino references from runbooks and docs (done in this PR).

## Alternatives considered

- **Solution A — keep `sql-bi-gateway-service` as a thin proxy that
  forwards to Trino.** Would have preserved the zero-engineering
  posture but kept all four costs from §Context (dialect, auth, ops,
  stub gateway). Rejected.
- **Promote `sql-warehousing-service` to also be the edge BI
  surface.** Would conflate the internal compute pool (large analytical
  workloads, no per-tenant quotas) with the BI edge (small interactive
  queries, strict per-tenant quotas, audit). The two have different
  scaling characteristics and different SLOs (cf. ADR-0012); kept
  separate.
