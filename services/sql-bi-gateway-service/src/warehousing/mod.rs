//! Warehousing bounded context — internal module of `sql-bi-gateway-service`.
//!
//! Owns the `warehouse_jobs`, `warehouse_transformations` and
//! `warehouse_storage_artifacts` tables that used to belong to the
//! standalone `sql-warehousing-service`. After the S8 consolidation
//! (ADR-0030 / `docs/architecture/service-consolidation-map.md`) the
//! warehousing CRUD lives next to the BI gateway so external BI
//! clients and pipeline-build callers hit a single service.
//!
//! The Flight SQL execution path of the old service collapses into
//! the gateway's existing [`crate::flight_sql`]: there is no separate
//! Flight SQL endpoint to forward to. The `warehousing_flight_sql_url`
//! configuration field is retained as `Option<String>` for backwards
//! compatibility but is expected to be unset in the consolidated
//! deployment.

pub mod handlers;
pub mod models;
