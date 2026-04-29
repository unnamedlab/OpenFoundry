//! `sql-warehousing-service` library surface.
//!
//! Exposes the building blocks of the service so they can be exercised by
//! integration tests and by other crates in the workspace. The actual binary
//! lives in `src/main.rs`.

pub mod config;
pub mod flight_sql;
