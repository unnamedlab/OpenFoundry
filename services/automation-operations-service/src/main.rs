//! `automation-operations-service` binary.
//!
//! Boots the FASE 6 / Tarea 6.3 saga substrate: HTTP handlers that
//! publish to `saga.step.requested.v1` via the transactional outbox,
//! and a saga consumer task that subscribes to the same topic and
//! drives the matching step graph through `libs/saga::SagaRunner`.

use std::{net::SocketAddr, sync::Arc, time::Duration};

use auth_middleware::jwt::JwtConfig;
use automation_operations_service::{
    AppState,
    config::AppConfig,
    domain::saga_consumer::{
        self, CONSUMER_GROUP, PROCESSED_EVENTS_TABLE, SagaConsumer,
    },
    handlers::{create_item, create_secondary, get_item, list_items, list_secondary},
};
use axum::{Router, routing::get};
use event_bus_data::{
    DataBusConfig, DataPublisher, KafkaPublisher, KafkaSubscriber, ServicePrincipal,
};
use idempotency::{IdempotencyStore, postgres::PgIdempotencyStore};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("automation_operations_service=info,tower_http=info")
        }))
        .init();

    let config = AppConfig::from_env()?;

    let db = PgPoolOptions::new()
        .max_connections(10)
        .acquire_timeout(Duration::from_secs(10))
        .connect(&config.database_url)
        .await?;

    let state = AppState { db: db.clone() };
    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();

    let protected = Router::new()
        .route("/automations", get(list_items).post(create_item))
        .route("/automations/{id}", get(get_item))
        .route(
            "/automations/{parent_id}/runs",
            get(list_secondary).post(create_secondary),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest("/api/v1", protected)
        .route("/health", get(|| async { "ok" }))
        .with_state(state);

    // ── Foundry-pattern Kafka saga consumer (FASE 6 / Tarea 6.3) ──
    // Boot is best-effort: if Kafka is not configured (e.g. dev
    // running without the broker) we skip the consumer task and
    // log a warning so HTTP-only flows still come up.
    match build_saga_consumer(db.clone()) {
        Ok(consumer) => {
            let bus_config = data_bus_config_from_env(CONSUMER_GROUP)?;
            let subscriber = KafkaSubscriber::new(&bus_config, CONSUMER_GROUP)?;
            let consumer = Arc::new(consumer);
            tokio::spawn(async move {
                if let Err(error) = saga_consumer::run(consumer, subscriber).await {
                    tracing::error!("saga.step.requested.v1 consumer stopped: {error}");
                }
            });
        }
        Err(error) => {
            tracing::warn!(
                error = %error,
                "skipping saga.step.requested.v1 consumer; HTTP API still online",
            );
        }
    }

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting automation-operations-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

#[derive(Debug, thiserror::Error)]
enum ConsumerBootError {
    #[error("KAFKA_BOOTSTRAP_SERVERS is not set")]
    KafkaUnconfigured,
    #[error("kafka publisher init failed: {0}")]
    KafkaPublisher(#[from] event_bus_data::PublishError),
}

fn build_saga_consumer(db: sqlx::PgPool) -> Result<SagaConsumer, ConsumerBootError> {
    if std::env::var("KAFKA_BOOTSTRAP_SERVERS").is_err() {
        return Err(ConsumerBootError::KafkaUnconfigured);
    }
    let publisher: Arc<dyn DataPublisher> = Arc::new(KafkaPublisher::from_env(CONSUMER_GROUP)?);
    let idempotency: Arc<dyn IdempotencyStore> =
        Arc::new(PgIdempotencyStore::new(db.clone(), PROCESSED_EVENTS_TABLE));
    Ok(SagaConsumer::new(db, idempotency, publisher))
}

fn data_bus_config_from_env(service_name: &str) -> Result<DataBusConfig, ConsumerBootError> {
    let brokers = std::env::var("KAFKA_BOOTSTRAP_SERVERS")
        .map_err(|_| ConsumerBootError::KafkaUnconfigured)?;
    let service = first_non_empty_env(&["KAFKA_SASL_USERNAME", "KAFKA_CLIENT_ID"])
        .unwrap_or_else(|| service_name.to_string());
    let mut principal = match first_non_empty_env(&["KAFKA_SASL_PASSWORD"]) {
        Some(password) => ServicePrincipal::scram_sha_512(service, password),
        None => ServicePrincipal::insecure_dev(service),
    };
    if let Some(mechanism) = first_non_empty_env(&["KAFKA_SASL_MECHANISM"]) {
        principal.mechanism = mechanism;
    }
    if let Some(protocol) = first_non_empty_env(&["KAFKA_SECURITY_PROTOCOL"]) {
        principal.security_protocol = protocol;
    }
    Ok(DataBusConfig::new(brokers, principal))
}

fn first_non_empty_env(keys: &[&'static str]) -> Option<String> {
    for key in keys {
        if let Ok(value) = std::env::var(key) {
            let trimmed = value.trim();
            if !trimmed.is_empty() {
                return Some(trimmed.to_string());
            }
        }
    }
    None
}
