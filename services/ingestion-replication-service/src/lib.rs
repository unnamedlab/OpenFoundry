//! `ingestion-replication-service` — Kubernetes-native control plane.
//!
//! This crate exposes a gRPC API (`IngestionControlPlane`) that turns
//! [`proto::IngestJobSpec`] requests into Strimzi `KafkaConnector` (Debezium)
//! and, optionally, Apache-Flink-Kubernetes-Operator `FlinkDeployment`
//! resources in the target cluster. Today the desired-state job specs are
//! persisted in the service Postgres so a simple reconcile loop can re-apply
//! the resources if they drift; the target architecture keeps only low-traffic
//! control-plane intent in Postgres and moves high-frequency runtime state out
//! of CNPG.
//!
//! The service does **not** move any bytes itself: byte movement is delegated
//! to the workloads it materialises (Debezium connectors run inside Strimzi's
//! KafkaConnect cluster, Flink jobs run inside the Flink-Kubernetes-Operator).
//! Connector/source definitions live in `connector-management-service`.
//! Postgres owns only declarative `ingest_jobs` intent plus explicit
//! low-frequency boundary fields (such as the applied resource names).
//! Runtime status and CDC checkpoints now live outside Postgres in dedicated
//! runtime stores managed by this crate.
//!
//! Only the new control-plane modules are part of the compiled crate. Some
//! pre-existing connector/catalog skeleton files remain on disk as drafts for
//! future migrations, but the legacy SQL sync runtime has been removed so this
//! crate has a single ingestion control-plane authority.

pub mod app_config;
pub mod cdc;
pub mod control_plane;
pub mod crds;
pub mod grpc_service;
pub mod reconcile;
pub mod repository;
pub mod runtime_state;

#[allow(clippy::all, clippy::pedantic, missing_docs, unreachable_pub)]
pub mod proto {
    //! Tonic-generated gRPC types for the `IngestionControlPlane` service.
    tonic::include_proto!("open_foundry.ingestion_replication.v1");
}
