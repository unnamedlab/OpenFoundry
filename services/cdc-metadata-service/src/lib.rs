//! Library entry-point for `cdc-metadata-service`.
//!
//! The binary (`src/main.rs`) is currently a placeholder; this lib exists
//! so the schema-registry module can be exercised by unit tests and
//! consumed by future router wiring without forcing the full HTTP boot
//! code into scope today.

use sqlx::PgPool;

pub mod schema_registry;

/// Shared application state. Mirrors what the future `main.rs` will build
/// (a single connection pool plus configuration). Kept narrow on purpose:
/// downstream modules clone the pool out of state, they do not capture
/// the whole struct.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
}
