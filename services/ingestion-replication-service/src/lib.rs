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
