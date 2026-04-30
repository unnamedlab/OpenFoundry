//! gRPC bridge from `connector-management-service` to
//! `ingestion-replication-service`. Exposes the generated client stubs and a
//! thin helper that builds an `IngestJobSpec` from a stored `Connection`.

#![allow(clippy::all)]
#![allow(missing_docs)]

pub mod ingestion_control_plane {
    tonic::include_proto!("open_foundry.ingestion_replication.v1");
}

use serde_json::Value;

pub use ingestion_control_plane::ingestion_control_plane_client::IngestionControlPlaneClient;
pub use ingestion_control_plane::{
    CreateIngestJobRequest, IcebergSink, IngestJob, IngestJobSpec, PostgresSource,
};

#[derive(Debug, thiserror::Error)]
pub enum BridgeError {
    #[error("connector_type {0:?} is not supported by the ingestion bridge yet")]
    UnsupportedConnector(String),
    #[error("missing required source config field: {0}")]
    MissingField(&'static str),
}

/// Build an `IngestJobSpec` from a source row. Currently only `postgresql`
/// is wired through; other connectors return [`BridgeError::UnsupportedConnector`]
/// so the caller can record the run as failed with a clear message.
pub fn build_spec(
    source_name: &str,
    connector_type: &str,
    config: &Value,
    namespace: &str,
    kafka_connect_cluster: &str,
) -> Result<IngestJobSpec, BridgeError> {
    match connector_type {
        "postgresql" | "postgres" => {
            let host = config
                .get("host")
                .and_then(Value::as_str)
                .ok_or(BridgeError::MissingField("host"))?
                .to_string();
            let port = config.get("port").and_then(Value::as_i64).unwrap_or(5432) as i32;
            let database = config
                .get("database")
                .and_then(Value::as_str)
                .ok_or(BridgeError::MissingField("database"))?
                .to_string();
            let user = config
                .get("user")
                .and_then(Value::as_str)
                .ok_or(BridgeError::MissingField("user"))?
                .to_string();
            let password_secret = config
                .get("password_secret")
                .and_then(Value::as_str)
                .unwrap_or("source-password")
                .to_string();
            let slot_name = config
                .get("slot_name")
                .and_then(Value::as_str)
                .unwrap_or("openfoundry_slot")
                .to_string();
            let publication_name = config
                .get("publication_name")
                .and_then(Value::as_str)
                .unwrap_or("openfoundry_pub")
                .to_string();
            let tables = config
                .get("tables")
                .and_then(Value::as_array)
                .map(|arr| {
                    arr.iter()
                        .filter_map(Value::as_str)
                        .map(str::to_string)
                        .collect::<Vec<_>>()
                })
                .unwrap_or_default();

            Ok(IngestJobSpec {
                name: source_name.to_string(),
                namespace: namespace.to_string(),
                source: "postgres".to_string(),
                postgres: Some(PostgresSource {
                    hostname: host,
                    port,
                    database,
                    user,
                    password_secret,
                    slot_name,
                    publication_name,
                    tables,
                    topic_prefix: source_name.to_string(),
                }),
                kafka_connect_cluster: kafka_connect_cluster.to_string(),
                iceberg_sink: None,
            })
        }
        other => Err(BridgeError::UnsupportedConnector(other.to_string())),
    }
}
