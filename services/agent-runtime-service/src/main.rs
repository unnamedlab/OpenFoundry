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
    pub tool_registry_service_url: String,
    pub conversation_state_service_url: String,
    pub llm_catalog_service_url: String,
    pub prompt_workflow_service_url: String,
    pub retrieval_context_service_url: String,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self { state.jwt_config.clone() }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("agent-runtime-service");
    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");
    sqlx::migrate!("./migrations").run(&pool).await.expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(60))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
        tool_registry_service_url: cfg.tool_registry_service_url.clone(),
        conversation_state_service_url: cfg.conversation_state_service_url.clone(),
        llm_catalog_service_url: cfg.llm_catalog_service_url.clone(),
        prompt_workflow_service_url: cfg.prompt_workflow_service_url.clone(),
        retrieval_context_service_url: cfg.retrieval_context_service_url.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("agent-runtime-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/ai/agents",
            get(handlers::list_agents).post(handlers::create_agent),
        )
        .route(
            "/api/v1/ai/agents/{id}",
            get(handlers::get_agent).patch(handlers::update_agent),
        )
        .route(
            "/api/v1/ai/agents/{id}/runs",
            get(handlers::list_runs).post(handlers::start_run),
        )
        .route(
            "/api/v1/ai/agents/{id}/runs/{run_id}/steps",
            post(handlers::record_step),
        )
        .route(
            "/api/v1/ai/agents/{id}/runs/{run_id}/human-approval",
            post(handlers::submit_human_approval),
        )
        .route(
            "/api/v1/ai/chat/completions",
            post(handlers::create_chat_completion),
        )
        .route(
            "/api/v1/ai/copilot/ask",
            post(handlers::ask_copilot),
        )
        .layer(middleware::from_fn_with_state(jwt_config, auth_middleware::auth_layer));

    let app = Router::new().merge(public).merge(protected).with_state(state);
    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting agent-runtime-service on {addr}");
    let listener = tokio::net::TcpListener::bind(&addr).await.expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
