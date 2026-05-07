//! `security_governance` — absorbed from the retired `security-governance-service`
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
    /// Audit-side Postgres pool used by the governance reports
    /// query. In production this points at the audit-compliance
    /// schema on the same `pg-policy` cluster.
    pub audit_db: PgPool,
    /// Policy-side Postgres pool used by the governance template
    /// reads (authorization policies + restricted views).
    pub policy_db: PgPool,
}
