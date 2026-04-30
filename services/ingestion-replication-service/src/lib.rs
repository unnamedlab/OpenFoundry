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
//! `ingestion-replication-service` — Kubernetes-native control plane.
//!
//! This crate exposes a gRPC API (`IngestionControlPlane`) that turns
//! [`proto::IngestJobSpec`] requests into Strimzi `KafkaConnector` (Debezium)
//! and, optionally, Apache-Flink-Kubernetes-Operator `FlinkDeployment`
//! resources in the target cluster. Submitted jobs are persisted in the
//! service Postgres so a simple reconcile loop can re-apply the resources if
//! they drift. See [`README.md`](https://github.com/) for the architecture
//! overview.
//!
//! The service does **not** move any bytes itself: byte movement is delegated
//! to the workloads it materialises (Debezium connectors run inside Strimzi's
//! KafkaConnect cluster, Flink jobs run inside the Flink-Kubernetes-Operator).
//!
//! Only the new control-plane modules are part of the compiled crate. The
//! pre-existing skeleton files under `src/handlers`, `src/connectors`,
//! `src/domain`, `src/models` and the legacy `src/config.rs` remain on disk
//! as drafts for future migrations and are intentionally not wired in to keep
//! this crate compilable.

pub mod app_config;
pub mod control_plane;
pub mod crds;
pub mod grpc_service;
pub mod reconcile;
pub mod repository;

#[allow(clippy::all, clippy::pedantic, missing_docs, unreachable_pub)]
pub mod proto {
    //! Tonic-generated gRPC types for the `IngestionControlPlane` service.
    tonic::include_proto!("open_foundry.ingestion_replication.v1");
}
