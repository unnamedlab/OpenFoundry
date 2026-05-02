//! `retention-policy-service` library — exposes the domain, models,
//! handlers and shared `AppState` so tests can exercise the pure
//! filtering / update logic without spinning up the full binary.
//!
//! The HTTP `main.rs` is currently a stub; once it grows, it should
//! consume `build_router` from this lib (the same pattern other
//! services follow).

use std::sync::Arc;

use sqlx::PgPool;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: Arc<auth_middleware::jwt::JwtConfig>,
}
