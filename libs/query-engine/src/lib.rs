//! OpenFoundry query engine library.
//!
//! Thin wrappers, custom UDFs and table providers around [Apache DataFusion].
//! The crate exposes a [`QueryContext`] that bundles a DataFusion
//! `SessionContext` configured with OpenFoundry defaults, used by services
//! such as `sql-warehousing-service` to execute SQL.
//!
//! [Apache DataFusion]: https://datafusion.apache.org/

pub mod context;
pub mod datasource;
pub mod optimizer_rules;
pub mod udf;

pub use context::QueryContext;
