pub mod config;
pub mod connectors;
pub mod domain;
pub mod grpc;
pub mod handlers;
pub mod models;

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

/// Shared application state passed to HTTP handlers and the gRPC service.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub http_client: reqwest::Client,
    pub jwt_config: JwtConfig,
    pub dataset_service_url: String,
    pub allow_private_network_egress: bool,
    pub allowed_egress_hosts: Vec<String>,
    pub agent_stale_after: std::time::Duration,
}

// Include the tonic/prost-generated code.  The nested module layout mirrors the
// proto package hierarchy so that cross-package type references resolve to
// `crate::open_foundry::common::*` as prost expects.
pub mod open_foundry {
    pub mod common {
        tonic::include_proto!("open_foundry.common");
    }
    pub mod data_integration {
        tonic::include_proto!("open_foundry.data_integration");
    }
}
