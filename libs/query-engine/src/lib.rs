//! Apache DataFusion wrappers, custom UDFs, and table providers used across
//! OpenFoundry data-plane services.

pub mod context;
pub mod datasource;
pub mod optimizer_rules;
pub mod udf;

#[cfg(feature = "flight-client")]
pub mod flight_provider;
