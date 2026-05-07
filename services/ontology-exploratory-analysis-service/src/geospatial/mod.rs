//! `geospatial` — absorbed from the retired `geospatial-intelligence-service`
//! per ADR-0030 (S8 / B20). The source's binary was a stub that wrapped a
//! `geospatial_base` substrate (engine, geocoding, indexer, tile_server,
//! handlers, models). Held as dead-code library namespace until the
//! consolidated binary's main is wired in a follow-up.
#![allow(dead_code)]

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;
// `geospatial_base/` is reached via `#[path]` directives from
// `domain.rs`, `handlers.rs`, `models.rs` — see those files.

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
}
