//! `product_distribution` — absorbed from the retired `product-distribution-service`
//! per ADR-0030 (S8 / B21). Held as dead-code library namespace until
//! the consolidated binary's main is wired in a follow-up.
#![allow(dead_code)]

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;
pub mod import;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
}
