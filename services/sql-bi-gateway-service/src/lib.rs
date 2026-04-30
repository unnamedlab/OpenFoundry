//! `sql-bi-gateway-service` library surface.
//!
//! The service implements an [Apache Arrow Flight SQL] server (port 50133)
//! that is the **single edge SQL surface** of OpenFoundry for external BI
//! clients (Tableau, Superset, JDBC/ODBC notebooks). It supersedes the
//! Trino edge BI deployment that lived under `infra/k8s/trino/` —
//! see [ADR-0014].
//!
//! The Flight SQL server is backed by DataFusion (via `libs/query-engine`)
//! and a [`routing::BackendRouter`] that dispatches each statement to the
//! appropriate backend per [ADR-0009] and [ADR-0014]:
//!
//! | Workload                          | Backend                                   |
//! | --------------------------------- | ----------------------------------------- |
//! | Iceberg / lakehouse analytical    | DataFusion (local) or `sql-warehousing-service` over Flight SQL |
//! | Time-series                       | ClickHouse (registered as Flight SQL endpoint)                  |
//! | Search / hybrid retrieval         | Vespa (registered as Flight SQL endpoint)                       |
//! | OLTP reference                    | Postgres (registered as Flight SQL endpoint)                    |
//!
//! A small axum side router runs on `healthz_port` and exposes:
//!
//! * `GET  /healthz` — liveness probe
//! * `POST /api/v1/queries/saved` — create saved query (BI dashboards)
//! * `GET  /api/v1/queries/saved` — list saved queries
//! * `DELETE /api/v1/queries/saved/:id` — delete a saved query
//!
//! Authentication, tenant quotas and audit are applied uniformly on both
//! surfaces (Flight SQL gRPC and HTTP REST) by reading the platform JWT
//! through `auth_middleware`.
//!
//! [Apache Arrow Flight SQL]: https://arrow.apache.org/docs/format/FlightSql.html
//! [ADR-0014]: ../../../docs/architecture/adr/ADR-0014-retire-trino-flight-sql-only.md
//! [ADR-0009]: ../../../docs/architecture/adr/ADR-0009-internal-query-fabric-datafusion-flightsql.md

pub mod audit;
pub mod auth;
pub mod config;
pub mod flight_sql;
pub mod http;
pub mod models;
pub mod routing;
