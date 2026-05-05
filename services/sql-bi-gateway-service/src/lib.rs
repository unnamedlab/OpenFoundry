//! `sql-bi-gateway-service` library surface.
//!
//! The service implements an [Apache Arrow Flight SQL] server (port 50133)
//! that is the **single edge SQL surface** of OpenFoundry for external BI
//! clients (Tableau, Superset, JDBC/ODBC notebooks). It supersedes the
//! Trino edge BI deployment that lived under `infra/k8s/platform/manifests/trino/` —
//! see [ADR-0014].
//!
//! The Flight SQL server is backed by DataFusion (via `libs/query-engine`)
//! and a [`routing::BackendRouter`] that dispatches each statement to the
//! appropriate backend per [ADR-0009], [ADR-0014] and [ADR-0029]:
//!
//! | Workload                          | Backend                                   |
//! | --------------------------------- | ----------------------------------------- |
//! | Iceberg / lakehouse analytical    | DataFusion (local) or `sql-warehousing-service` over Flight SQL |
//! | Iceberg analytics                 | Trino (registered as Flight SQL endpoint)                       |
//! | Search / hybrid retrieval         | Vespa (registered as Flight SQL endpoint)                       |
//! | OLTP reference                    | Postgres (registered as Flight SQL endpoint)                    |
//!
//! A small axum side router runs on `healthz_port` and exposes:
//!
//! * `GET  /healthz` — liveness probe
//! * `POST /api/v1/queries/saved` — create saved query (BI dashboards)
//! * `GET  /api/v1/queries/saved` — list saved queries
//! * `DELETE /api/v1/queries/saved/:id` — delete a saved query
//! * `…/api/v1/warehouse/*`         — warehousing jobs / transformations / artifacts
//!   (absorbed from the retired `sql-warehousing-service`, see [`warehousing`])
//! * `…/api/v1/tabular/*`           — tabular-analysis jobs and results
//!   (absorbed from the retired `tabular-analysis-service`, see [`tabular`])
//!
//! Reusable analytical expressions (the previous
//! `analytical-logic-service` payload) are exposed via the internal
//! [`analytical_logic`] crate — there is **no** duplicated HTTP route on
//! the gateway, per the S8 task notes ("expresiones reutilizables: deben
//! ser una crate interna, no rutas HTTP duplicadas"). Callers depend on
//! the crate directly.
//!
//! Authentication, tenant quotas and audit are applied uniformly on both
//! surfaces (Flight SQL gRPC and HTTP REST) by reading the platform JWT
//! through `auth_middleware`.
//!
//! [Apache Arrow Flight SQL]: https://arrow.apache.org/docs/format/FlightSql.html
//! [ADR-0014]: ../../../docs/architecture/adr/ADR-0014-retire-trino-flight-sql-only.md
//! [ADR-0029]: ../../../docs/architecture/adr/ADR-0029-reintroduce-trino-for-iceberg-analytics.md
//! [ADR-0009]: ../../../docs/architecture/adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md

pub mod audit;
pub mod auth;
pub mod config;
pub mod flight_sql;
pub mod http;
pub mod models;
pub mod routing;
pub mod tabular;
pub mod warehousing;

/// Re-export of the [`analytical_logic`] crate so consumers of this
/// library see the merged surface in one place. The crate is the
/// canonical home of saved-expression types and the Postgres repository;
/// no HTTP routes are exposed on this service for it (S8 — "no rutas
/// HTTP duplicadas").
pub use analytical_logic;
