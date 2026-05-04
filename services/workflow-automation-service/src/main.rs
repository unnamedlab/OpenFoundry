mod config;
mod domain;
mod event;
mod handlers;
mod models;
mod topics;

use std::{net::SocketAddr, sync::Arc, time::Duration};

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{delete, get, patch, post},
};
use event_bus_data::{
    DataBusConfig, DataPublisher, KafkaPublisher, KafkaSubscriber, ServicePrincipal,
};
use idempotency::{IdempotencyStore, postgres::PgIdempotencyStore};
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

use crate::config::AppConfig;
use crate::domain::condition_consumer::{
    self, CONSUMER_GROUP, ConditionConsumer, PROCESSED_EVENTS_TABLE,
};
use crate::domain::effect_dispatcher::EffectDispatcher;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub http_client: reqwest::Client,
    pub nats_url: String,
    pub pipeline_service_url: String,
    /// `approvals-service` base URL. Used by
    /// [`handlers::approvals::continue_after_approval`] to forward
    /// the UI's "continue after approval" decision to the
    /// authoritative state machine in
    /// `audit_compliance.approval_requests`. Replaces the
    /// pre-FASE-7 Temporal `ApprovalsClient` signal path.
    pub approvals_service_url: String,
    /// Optional service bearer token for the approvals proxy
    /// request. Empty in dev (the in-cluster `approvals-service`
    /// accepts unauthenticated peers when this is unset).
    pub approvals_service_bearer_token: Option<String>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| {
                EnvFilter::new("workflow_automation_service=info,tower_http=info")
            }),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    let http_client = reqwest::Client::builder()
        .timeout(Duration::from_secs(30))
        .build()?;

    let state = AppState {
        db: db.clone(),
        http_client: http_client.clone(),
        nats_url: app_config.nats_url.clone(),
        pipeline_service_url: app_config.pipeline_service_url.clone(),
        approvals_service_url: app_config.approvals_service_url.clone(),
        approvals_service_bearer_token: app_config.approvals_service_bearer_token.clone(),
    };

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();

    let authenticated = Router::new()
        .route(
            "/workflows",
            get(handlers::crud::list_workflows).post(handlers::crud::create_workflow),
        )
        .route(
            "/workflows/{id}",
            get(handlers::crud::get_workflow)
                .patch(handlers::crud::update_workflow)
                .delete(handlers::crud::delete_workflow),
        )
        .route(
            "/workflows/{id}/runs",
            get(handlers::runs::list_runs).post(handlers::execute::start_manual_run),
        )
        .route(
            "/workflows/approvals/{approval_id}/continue",
            post(handlers::approvals::continue_after_approval),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ));

    let unauthenticated = Router::new()
        .route(
            "/workflows/{id}/webhook",
            post(handlers::execute::trigger_webhook),
        )
        .route(
            "/workflows/{id}/_internal/lineage",
            post(handlers::execute::start_internal_lineage_run),
        )
        .route(
            "/workflows/{id}/_internal/triggered",
            post(handlers::execute::start_internal_triggered_run),
        );

    let app = Router::new()
        .nest("/api/v1", authenticated.merge(unauthenticated))
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state.clone());

    // Legacy NATS consumer for `of.workflows.run.requested`. Kept
    // until `pipeline-schedule-service` is migrated to publish
    // directly to `automate.condition.v1` (out of scope for FASE 5).
    // The handler the consumer invokes (`execute_internal_triggered_run`)
    // already routes through the new outbox path.
    {
        let nats_state = state.clone();
        tokio::spawn(async move {
            if let Err(error) =
                domain::workflow_run_requested::consume(nats_state).await
            {
                tracing::error!("workflow run requested consumer stopped: {error}");
            }
        });
    }

    // ── Foundry-pattern Kafka condition consumer (FASE 5 / Tarea 5.3) ──
    // Boot is best-effort: if Kafka is not configured (e.g. dev
    // running without the broker) we skip the consumer task and
    // log a warning so HTTP-only flows still come up.
    match build_condition_consumer(db.clone(), http_client.clone()) {
        Ok(consumer) => {
            let subscriber = KafkaSubscriber::new(
                &data_bus_config_from_env(CONSUMER_GROUP)?,
                CONSUMER_GROUP,
            )?;
            let consumer = Arc::new(consumer);
            tokio::spawn(async move {
                if let Err(error) = condition_consumer::run(consumer, subscriber).await {
                    tracing::error!("automate.condition.v1 consumer stopped: {error}");
                }
            });
        }
        Err(error) => {
            tracing::warn!(
                error = %error,
                "skipping automate.condition.v1 consumer; HTTP API still online",
            );
        }
    }

    let addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!("workflow-automation-service listening on http://{}", addr);
    axum::serve(listener, app).await?;
    Ok(())
}

#[derive(Debug, thiserror::Error)]
enum ConsumerBootError {
    #[error("KAFKA_BOOTSTRAP_SERVERS is not set")]
    KafkaUnconfigured,
    #[error("OF_ONTOLOGY_ACTIONS_URL is not set")]
    OntologyUrlUnconfigured,
    #[error("OF_ONTOLOGY_ACTIONS_BEARER_TOKEN is not set")]
    OntologyTokenUnconfigured,
    #[error("kafka publisher init failed: {0}")]
    KafkaPublisher(#[from] event_bus_data::PublishError),
}

fn build_condition_consumer(
    db: PgPool,
    http_client: reqwest::Client,
) -> Result<ConditionConsumer, ConsumerBootError> {
    if std::env::var("KAFKA_BOOTSTRAP_SERVERS").is_err() {
        return Err(ConsumerBootError::KafkaUnconfigured);
    }
    let ontology_url = first_non_empty_env(&[
        "OF_ONTOLOGY_ACTIONS_URL",
        "ONTOLOGY_ACTIONS_SERVICE_URL",
        "ONTOLOGY_SERVICE_URL",
        "OF_ONTOLOGY_ACTIONS_GRPC_ADDR",
    ])
    .ok_or(ConsumerBootError::OntologyUrlUnconfigured)?;
    let ontology_token = first_non_empty_env(&[
        "OF_ONTOLOGY_ACTIONS_BEARER_TOKEN",
        "ONTOLOGY_ACTIONS_BEARER_TOKEN",
    ])
    .ok_or(ConsumerBootError::OntologyTokenUnconfigured)?;

    let dispatcher = EffectDispatcher::new(http_client, ontology_url, ontology_token);
    let publisher: Arc<dyn DataPublisher> =
        Arc::new(KafkaPublisher::from_env(CONSUMER_GROUP)?);
    let idempotency: Arc<dyn IdempotencyStore> =
        Arc::new(PgIdempotencyStore::new(db.clone(), PROCESSED_EVENTS_TABLE));
    Ok(ConditionConsumer::new(db, idempotency, dispatcher, publisher))
}

/// Build the `event-bus-data` config from the standard OpenFoundry
/// Kafka env vars. Same shape as `reindex-coordinator-service`.
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
