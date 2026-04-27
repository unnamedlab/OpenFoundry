mod config;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, extract::FromRef, middleware,
    routing::get,
};
use core_models::{health::HealthStatus, observability};

#[derive(Clone)]
pub struct AppState {
    pub jwt_config: JwtConfig,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("widget-registry-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();

    let state = AppState {
        jwt_config: jwt_config.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("widget-registry-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/widgets/catalog",
            get(handlers::widgets::list_widget_catalog),
        )
        .layer(middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::auth_layer,
        ));

    let app = Router::new()
        .merge(public)
        .merge(protected)
        .with_state(state);

    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting widget-registry-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
