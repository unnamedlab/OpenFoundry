mod config;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{Router, extract::FromRef, middleware, routing::{get, post}};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
    pub ontology_query_service_url: String,
    pub ontology_actions_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self { state.jwt_config.clone() }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("ontology-exploratory-analysis-service");
    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let pool = PgPoolOptions::new()
        .max_connections(15)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");
    sqlx::migrate!("./migrations").run(&pool).await.expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        ontology_query_service_url: cfg.ontology_query_service_url.clone(),
        ontology_actions_service_url: cfg.ontology_actions_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("ontology-exploratory-analysis-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ontology-exploration/views",
            get(handlers::list_views).post(handlers::create_view),
        )
        .route(
            "/api/v1/ontology-exploration/views/{id}",
            get(handlers::get_view),
        )
        .route(
            "/api/v1/ontology-exploration/maps",
            get(handlers::list_maps).post(handlers::create_map),
        )
        .route(
            "/api/v1/ontology-exploration/writeback",
            post(handlers::propose_writeback),
        )
        .layer(middleware::from_fn_with_state(jwt_config, auth_middleware::auth_layer));

    let app = Router::new().merge(public).merge(protected).with_state(state);
    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting ontology-exploratory-analysis-service on {addr}");
    let listener = tokio::net::TcpListener::bind(&addr).await.expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
