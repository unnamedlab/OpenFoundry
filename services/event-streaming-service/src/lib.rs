//! `event-streaming-service` library root.
//!
//! Exposes the building blocks of the streaming control plane so they can
//! be exercised both from the binary and from the unit-test harness.
//!
//! The service has two faces:
//! * a gRPC routing facade (legacy `Publish` / `Subscribe`) preserved
//!   under [`grpc`] + [`router`] + [`backends`].
//! * a REST control plane ([`handlers`] + [`models`] + [`domain`] +
//!   [`storage`]) used by the OpenFoundry web UI to drive streams,
//!   windows, topologies, dead letters and live tails.

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use sqlx::PgPool;

pub mod app_config;
pub mod backends;
pub mod domain;
pub mod grpc;
pub mod handlers;
pub mod metrics;
pub mod models;
pub mod outbox;
pub mod router;
pub mod runtime;
pub mod storage;

/// Generated gRPC bindings for the router service.
pub mod proto {
    pub mod router {
        // tonic_build emits files named after the protobuf package.
        tonic::include_proto!("openfoundry.streaming.router.v1");
    }
    /// Generated bindings for the stream-config contract
    /// (`proto/streaming/streams.proto`). Surfaces the
    /// `StreamConsistency`/`StreamType`/`StreamConfig` types so the
    /// REST validator in `handlers::streams` can map JSON payloads
    /// straight onto the proto-defined enums.
    pub mod streams {
        tonic::include_proto!("openfoundry.streaming.streams.v1");
    }
}

/// Shared application state passed to every Axum handler.
///
/// Every field that is *currently* dereferenced by a handler or the
/// streaming engine is kept tight (no allocations on clone — `PgPool`,
/// `JwtConfig`, `reqwest::Client`, `Arc<…>` and `String` are all cheap).
/// The remaining fields (`backends`, `table`, `metrics`, `dataset_writer`,
/// `connector_management_service_url`) are reserved for the next runtime
/// expansion (in-process publish from REST handlers, dataset materialisers
/// triggered by topology runs, etc.) and are populated at startup so they
/// don't require a second wiring pass when those features land.
#[derive(Clone)]
pub struct AppState {
    /// Postgres pool shared by every handler.
    pub db: PgPool,
    /// JWT verifier configuration. Used both by the auth middleware and by
    /// `domain::engine::processor` to mint internal tokens for the dataset
    /// service.
    pub jwt_config: JwtConfig,
    /// Outbound HTTP client (kept around so we don't pay the TLS/connection
    /// pool startup cost on every request).
    #[allow(dead_code)]
    pub http_client: reqwest::Client,
    /// In-process backend registry (NATS / Kafka). Populated even when the
    /// REST plane runs alone so that future REST→backend bridges have a
    /// ready handle.
    #[allow(dead_code)]
    pub backends: Arc<crate::backends::BackendRegistry>,
    /// Compiled routing table (`topic-routes.yaml`).
    #[allow(dead_code)]
    pub table: Arc<crate::router::RouteTable>,
    /// Prometheus metrics registry shared between the gRPC and REST
    /// listeners.
    #[allow(dead_code)]
    pub metrics: Arc<crate::metrics::Metrics>,
    /// Iceberg / legacy dataset writer used by topology runs.
    #[allow(dead_code)]
    pub dataset_writer: Arc<dyn crate::storage::DatasetWriter>,
    /// Hot buffer (Kafka or NATS) used by `push_events` to mirror events
    /// into the realtime tier. When neither backend is configured this is
    /// a [`crate::domain::hot_buffer::NoopHotBuffer`].
    pub hot_buffer: Arc<dyn crate::domain::hot_buffer::HotBuffer>,
    /// Runtime store for hot events, checkpoint offsets and cold archive
    /// bookkeeping. Keeps Postgres limited to control-plane metadata.
    pub runtime_store: Arc<dyn crate::domain::runtime_store::RuntimeStore>,
    /// Operator state backend used by the checkpoint subsystem
    /// (Bloque C). Defaults to
    /// [`crate::domain::engine::state_store::InMemoryStateBackend`].
    pub state_backend: Arc<dyn crate::domain::engine::state_store::StateBackend>,
    /// Base URL of `connector-management-service` (proxied for the
    /// `/connectors` and `/live-tail` REST endpoints).
    #[allow(dead_code)]
    pub connector_management_service_url: String,
    /// Base URL of `data-asset-catalog-service` — used to commit dataset
    /// snapshots during topology runs.
    pub dataset_service_url: String,
    /// Local directory used by the file-based archive / dead-letter sink.
    pub archive_dir: String,
    /// Flink runtime knobs (Bloque D). Always present — the deployer
    /// and metrics poller modules themselves are gated by the
    /// `flink-runtime` feature, but the config is read so the REST
    /// handlers can surface the planned manifest even without kube.
    pub flink_config: std::sync::Arc<crate::runtime::flink::FlinkRuntimeConfig>,
    /// Public base URL of the streaming service surfaced to push
    /// consumers. Used by the reset endpoint to render the
    /// `https://{base}/streams-push/{view_rid}/records` POST URL the
    /// UI displays. Read from `STREAMING_PUBLIC_BASE_URL` (env) at
    /// startup; defaults to `http://localhost:8080` for dev.
    pub public_base_url: String,
}
