//! `virtual-table-service` binary entry point.

use std::net::SocketAddr;
use std::time::Duration;

use auth_middleware::jwt::{self, JwtConfig};
use axum::{
    Router,
    extract::{Request, State},
    http::header::AUTHORIZATION,
    middleware::{self, Next},
    response::{IntoResponse, Response},
    routing::{get, patch, post},
};
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

use virtual_table_service::{AppState, config::AppConfig, grpc, handlers, metrics};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("virtual_table_service=info,tower_http=info")),
        )
        .init();

    let app_config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&app_config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let jwt_config = JwtConfig::new(&app_config.jwt_secret).with_env_defaults();
    let metrics = metrics::Metrics::new();

    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        jwt_config: jwt_config.clone(),
        dataset_service_url: app_config.dataset_service_url.clone(),
        pipeline_service_url: app_config.pipeline_service_url.clone(),
        ontology_service_url: app_config.ontology_service_url.clone(),
        network_boundary_service_url: app_config.network_boundary_service_url.clone(),
        connector_management_service_url: app_config.connector_management_service_url.clone(),
        allow_private_network_egress: app_config.allow_private_network_egress,
        allowed_egress_hosts: app_config.allowed_egress_hosts.clone(),
        agent_stale_after: chrono::Duration::seconds(app_config.agent_stale_after_secs as i64),
        auto_register_poll_interval: Duration::from_secs(
            app_config.auto_register_poll_interval_seconds,
        ),
        update_detection_default_interval: Duration::from_secs(
            app_config.update_detection_default_interval_seconds,
        ),
        max_bulk_register_batch: app_config.max_bulk_register_batch,
        strict_source_validation: app_config.strict_source_validation,
        metrics,
    };

    spawn_auto_registration_scanner(state.clone());
    spawn_update_detection_poller(state.clone());

    let http_router = build_http_router(state.clone());
    let http_addr: SocketAddr = format!("{}:{}", app_config.host, app_config.port).parse()?;
    tracing::info!(%http_addr, "starting virtual-table-service HTTP");

    let grpc_addr: SocketAddr = format!("{}:{}", app_config.host, app_config.grpc_port).parse()?;
    tracing::info!(%grpc_addr, "starting virtual-table-service gRPC");

    let listener = tokio::net::TcpListener::bind(http_addr).await?;
    let http_handle = tokio::spawn(async move {
        if let Err(error) = axum::serve(listener, http_router).await {
            tracing::error!(?error, "axum::serve exited with error");
        }
    });

    let catalog = grpc::CatalogService::new(state.clone()).into_server();
    let grpc_handle = tokio::spawn(async move {
        if let Err(error) = tonic::transport::Server::builder()
            .add_service(catalog)
            .serve(grpc_addr)
            .await
        {
            tracing::error!(?error, "tonic::Server exited with error");
        }
    });

    tokio::select! {
        _ = http_handle => tracing::warn!("http server task ended"),
        _ = grpc_handle => tracing::warn!("grpc server task ended"),
    }

    Ok(())
}

fn build_http_router(state: AppState) -> Router {
    let virtual_tables = Router::new()
        .route(
            "/sources/{source_rid}/virtual-tables/enable",
            post(handlers::virtual_tables::enable_source)
                .delete(handlers::virtual_tables::disable_source),
        )
        .route(
            "/sources/{source_rid}/virtual-tables/discover",
            get(handlers::virtual_tables::discover),
        )
        .route(
            "/sources/{source_rid}/virtual-tables/register",
            post(handlers::virtual_tables::register),
        )
        .route(
            "/sources/{source_rid}/virtual-tables/bulk-register",
            post(handlers::virtual_tables::bulk_register),
        )
        .route(
            "/sources/{source_rid}/iceberg-catalog",
            post(handlers::virtual_tables::set_iceberg_catalog),
        )
        .route(
            "/sources/{source_rid}/auto-registration",
            post(handlers::virtual_tables::enable_auto_registration)
                .delete(handlers::virtual_tables::disable_auto_registration),
        )
        .route(
            "/sources/{source_rid}/auto-registration:scan-now",
            post(handlers::virtual_tables::auto_registration_scan_now),
        )
        // P6 — Foundry doc § "Virtual tables in Code Repositories".
        .route(
            "/sources/{source_rid}/code-imports",
            patch(handlers::virtual_tables::set_code_imports),
        )
        .route(
            "/sources/{source_rid}/export-controls",
            post(handlers::virtual_tables::set_export_controls),
        )
        .route(
            "/virtual-tables",
            get(handlers::virtual_tables::list),
        )
        .route(
            "/virtual-tables/{rid}",
            get(handlers::virtual_tables::get).delete(handlers::virtual_tables::delete),
        )
        .route(
            "/virtual-tables/{rid}/markings",
            patch(handlers::virtual_tables::update_markings),
        )
        .route(
            "/virtual-tables/{rid}/refresh-schema",
            post(handlers::virtual_tables::refresh_schema),
        )
        // P5 — update-detection (Foundry doc § "Update detection for
        // virtual table inputs").
        .route(
            "/virtual-tables/{rid}/update-detection",
            patch(handlers::virtual_tables::set_update_detection),
        )
        .route(
            "/virtual-tables/{rid}/update-detection:poll-now",
            post(handlers::virtual_tables::poll_update_detection_now),
        )
        .route(
            "/virtual-tables/{rid}/update-detection/history",
            get(handlers::virtual_tables::update_detection_history),
        );

    Router::new()
        .nest("/v1", virtual_tables)
        .layer(middleware::from_fn_with_state(
            state.jwt_config.clone(),
            optional_auth_layer,
        ))
        .route("/health", get(|| async { "ok" }))
        .route("/ready", get(readyz))
        .route("/metrics", get(metrics_handler))
        .with_state(state)
}

async fn metrics_handler(State(state): State<AppState>) -> Response {
    match state.metrics.render() {
        Ok(body) => (
            axum::http::StatusCode::OK,
            [(
                axum::http::header::CONTENT_TYPE,
                "text/plain; version=0.0.4",
            )],
            body,
        )
            .into_response(),
        Err(error) => {
            tracing::error!(?error, "failed to render prometheus metrics");
            axum::http::StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn readyz(State(state): State<AppState>) -> Response {
    match sqlx::query_scalar::<_, i32>("SELECT 1").fetch_one(&state.db).await {
        Ok(_) => (axum::http::StatusCode::OK, "ready").into_response(),
        Err(error) => {
            tracing::warn!(?error, "readyz: database probe failed");
            (axum::http::StatusCode::SERVICE_UNAVAILABLE, "not ready").into_response()
        }
    }
}

async fn optional_auth_layer(
    State(config): State<JwtConfig>,
    mut req: Request,
    next: Next,
) -> Response {
    if let Some(token) = req
        .headers()
        .get(AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
    {
        if let Ok(claims) = jwt::decode_token(&config, token) {
            req.extensions_mut().insert(claims);
        }
    }
    next.run(req).await
}

fn spawn_auto_registration_scanner(state: AppState) {
    let interval = state.auto_register_poll_interval;
    if interval.is_zero() {
        return;
    }
    tokio::spawn(async move {
        let mut tick = tokio::time::interval(interval);
        tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            tick.tick().await;
            tracing::debug!(?interval, "auto-registration scanner tick (P1 stub)");
        }
    });
}

/// P5 — update-detection poller. Calls
/// [`virtual_table_service::domain::update_detection::run_tick`] on a
/// tokio interval; each tick fetches every row whose
/// `update_detection_next_poll_at` is due, probes the source, and
/// emits the `DATA_UPDATED` outbox event when the version advanced.
fn spawn_update_detection_poller(state: AppState) {
    let interval = state.update_detection_default_interval;
    if interval.is_zero() {
        return;
    }
    tokio::spawn(async move {
        let mut tick = tokio::time::interval(interval);
        tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            tick.tick().await;
            match virtual_table_service::domain::update_detection::run_tick(&state).await {
                Ok(count) if count > 0 => {
                    tracing::info!(?interval, count, "update-detection poller tick");
                }
                Ok(_) => {}
                Err(error) => {
                    tracing::warn!(?error, "update-detection poller tick failed");
                }
            }
        }
    });
}
