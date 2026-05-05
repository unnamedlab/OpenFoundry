//! Tabular-analysis bounded context — internal module of `sql-bi-gateway-service`.
//!
//! Owns the `tabular_analysis_jobs` and `tabular_analysis_results` tables
//! that used to belong to the standalone `tabular-analysis-service`.
//! After the S8 consolidation (ADR-0030 /
//! `docs/architecture/service-consolidation-map.md`) AI-driven schema
//! inference and column profiling jobs are submitted through the same
//! HTTP surface as the rest of the BI gateway.

pub mod handlers;
pub mod models;
