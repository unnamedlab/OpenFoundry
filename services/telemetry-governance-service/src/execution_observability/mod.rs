//! `execution_observability` — absorbed from the retired
//! `execution-observability-service` per ADR-0030 (S8 / B22).
//! Source was a `tools/scaffold_p59_p85.py` placeholder; held as
//! dead-code library namespace until target main wires it.
#![allow(dead_code)]

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

pub mod config;
pub mod handlers;
pub mod models;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
}
