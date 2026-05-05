//! Reusable analytical expressions and visual function templates.
//!
//! This crate is the runtime owner of the `analytical_expressions` and
//! `analytical_expression_versions` tables that used to belong to the
//! standalone `analytical-logic-service`. Per ADR-0030 (S8 consolidation)
//! and the S8 task notes for `sql-bi-gateway-service`:
//!
//! > Analytical-logic son expresiones reutilizables: deben ser una crate
//! > interna, no rutas HTTP duplicadas.
//!
//! Consumers (today: `sql-bi-gateway-service`; tomorrow: any service that
//! needs to look up or persist a saved expression) embed this crate and
//! call into [`AnalyticalExpressionRepo`] directly. There is **no
//! standalone HTTP surface** — the previous `/api/v1/analytical-logic/...`
//! routes were retired with the source service.
//!
//! The schema is the same one shipped by
//! `migrations/0001_analytical_expressions_foundation.sql` (also installed
//! into `services/sql-bi-gateway-service/migrations/` so the gateway's
//! pre-install Helm Job applies it as part of the consolidated bounded
//! context).

pub mod model;
pub mod repo;

pub use model::{
    AnalyticalExpression, AnalyticalExpressionVersion, NewExpression, NewExpressionVersion,
};
pub use repo::{AnalyticalExpressionRepo, RepoError};
