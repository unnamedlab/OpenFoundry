//! `checkpoints_purpose` — absorbed from the retired `checkpoints-purpose-service`
//! per ADR-0030 (S8 / B14). Held as dead-code library namespace
//! until the consolidated binary's main is wired in a follow-up.
#![allow(dead_code)]

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;

/// Stub `AppState` matching the absorbed source's shape so the moved
/// handlers compile. The real wiring (full bootstrap, custom fields)
/// will land when the consolidated binary's main is wired.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
}
