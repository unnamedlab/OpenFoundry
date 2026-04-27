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
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self { state.jwt_config.clone() }
}

#[tokio::main]
async fn main() {
    observability::init_tracing("code-repository-review-service");
    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let pool = PgPoolOptions::new()
        .max_connections(15)
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
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("code-repository-review-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/code-repos/overview",
            get(handlers::get_overview),
        )
        .route(
            "/api/v1/code-repos/integrations",
            get(handlers::list_integrations).post(handlers::create_integration),
        )
        .route(
            "/api/v1/code-repos/integrations/{id}",
            get(handlers::get_integration),
        )
        .route(
            "/api/v1/code-repos/integrations/{id}/sync",
            post(handlers::sync_integration),
        )
        .route(
            "/api/v1/code-repos/merge-requests",
            get(handlers::list_merge_requests_global),
        )
        .route(
            "/api/v1/code-repos/merge-requests/{id}",
            get(handlers::get_merge_request),
        )
        .route(
            "/api/v1/code-repos/merge-requests/{id}/merge",
            post(handlers::merge_merge_request),
        )
        .route(
            "/api/v1/code-repos/repositories",
            get(handlers::list_repositories).post(handlers::create_repository),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}",
            get(handlers::get_repository).delete(handlers::delete_repository),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/commits",
            get(handlers::list_commits).post(handlers::create_commit),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/files",
            get(handlers::list_files),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/search",
            get(handlers::search_files),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/diff",
            get(handlers::get_diff),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/merge-requests",
            get(handlers::list_merge_requests).post(handlers::create_merge_request),
        )
        .route(
            "/api/v1/code-repos/merge-requests/{id}/comments",
            get(handlers::list_review_comments).post(handlers::create_review_comment),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/ci-runs",
            get(handlers::list_ci_runs).post(handlers::create_ci_run),
        )
        .route(
            "/api/v1/code-repos/repositories/{id}/ci",
            get(handlers::list_ci_runs).post(handlers::create_ci_run),
        )
        .layer(middleware::from_fn_with_state(jwt_config, auth_middleware::auth_layer));

    let app = Router::new().merge(public).merge(protected).with_state(state);
    let addr = format!("{}:{}", cfg.host, cfg.port);
    tracing::info!("starting code-repository-review-service on {addr}");
    let listener = tokio::net::TcpListener::bind(&addr).await.expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
