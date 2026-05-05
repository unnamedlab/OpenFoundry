//! `virtual-table-service` library surface.
//!
//! Splitting the binary's modules behind a `lib.rs` lets integration
//! tests under `tests/` reach `domain::*` and `models::*` without
//! recompiling the whole binary tree, mirroring the pattern used by
//! `connector-management-service`.

pub mod config;
pub mod connectors;
pub mod domain;
pub mod handlers;
pub mod metrics;
pub mod models;

use std::time::Duration;

use sqlx::PgPool;

/// Generated `VirtualTableCatalog` server / client stubs. Compiled by
/// `build.rs` from `proto/virtual_tables/virtual_tables.proto`.
pub mod proto {
    #![allow(clippy::all, missing_docs, unused_qualifications)]
    tonic::include_proto!("open_foundry.virtual_tables");
}

pub mod grpc;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub http_client: reqwest::Client,
    pub jwt_config: auth_middleware::jwt::JwtConfig,
    pub dataset_service_url: String,
    pub pipeline_service_url: String,
    pub ontology_service_url: String,
    pub network_boundary_service_url: String,
    /// `connector-management-service` base URL (P2 — used by
    /// `domain::source_validation` to fetch the source's worker_kind
    /// and egress.kind before allowing virtual-table registration).
    pub connector_management_service_url: String,
    pub allow_private_network_egress: bool,
    pub allowed_egress_hosts: Vec<String>,
    pub agent_stale_after: chrono::Duration,
    pub auto_register_poll_interval: Duration,
    pub update_detection_default_interval: Duration,
    pub max_bulk_register_batch: usize,
    /// When `true`, `register_virtual_table` calls
    /// `domain::source_validation::validate_for_virtual_tables`
    /// against `connector-management-service` and rejects any source
    /// that does not satisfy the Foundry doc § "Limitations" rules.
    pub strict_source_validation: bool,
    pub metrics: crate::metrics::Metrics,
}
