//! `lineage-deletion-service` library crate.
//!
//! Exposes the deletion handlers, the new T4.2 [`retention_runner`]
//! worker, and a shared [`AppState`] so unit tests can exercise the
//! pure logic without depending on the HTTP binary.

use std::sync::Arc;

use sqlx::PgPool;
use storage_abstraction::StorageBackend;

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub storage: Arc<dyn StorageBackend>,
    pub http_client: reqwest::Client,
    pub lineage_service_url: String,
    pub dataset_service_url: String,
    pub audit_compliance_service_url: String,
    pub jwt_config: Arc<auth_middleware::jwt::JwtConfig>,
}
