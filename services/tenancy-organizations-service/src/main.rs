mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{Router, extract::FromRef, middleware, routing::get};
use core_models::health::HealthStatus;
use sqlx::postgres::PgPoolOptions;
use tracing_subscriber::EnvFilter;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub nexus_db: sqlx::PgPool,
    pub ontology_db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
}

impl FromRef<AppState> for JwtConfig {
    fn from_ref(state: &AppState) -> Self {
        state.jwt_config.clone()
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .init();

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let db = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to tenancy database");
    let nexus_db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.nexus_database_url)
        .await
        .expect("failed to connect to nexus database");
    let ontology_db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.ontology_database_url)
        .await
        .expect("failed to connect to ontology database");

    sqlx::migrate!("./migrations")
        .run(&db)
        .await
        .expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let state = AppState {
        db,
        nexus_db,
        ontology_db,
        jwt_config: jwt_config.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("tenancy-organizations-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/tenancy/resolve",
            get(handlers::tenant_resolution::resolve_tenant),
        )
        .route(
            "/api/v1/organizations",
            get(handlers::organizations::list_organizations)
                .post(handlers::organizations::create_organization),
        )
        .route(
            "/api/v1/organizations/{id}",
            get(handlers::organizations::get_organization)
                .patch(handlers::organizations::update_organization),
        )
        .route(
            "/api/v1/enrollments",
            get(handlers::enrollments::list_enrollments)
                .post(handlers::enrollments::create_enrollment),
        )
        .route(
            "/api/v1/enrollments/{id}",
            get(handlers::enrollments::get_enrollment)
                .patch(handlers::enrollments::update_enrollment),
        )
        .route(
            "/api/v1/spaces",
            get(handlers::spaces::list_spaces).post(handlers::spaces::create_space),
        )
        .route(
            "/api/v1/spaces/{id}",
            axum::routing::patch(handlers::spaces::update_space),
        )
        .route(
            "/api/v1/nexus/spaces",
            get(handlers::spaces::list_spaces).post(handlers::spaces::create_space),
        )
        .route(
            "/api/v1/nexus/spaces/{id}",
            axum::routing::patch(handlers::spaces::update_space),
        )
        .route(
            "/api/v1/projects",
            get(handlers::projects::list_projects).post(handlers::projects::create_project),
        )
        .route(
            "/api/v1/projects/{id}",
            get(handlers::projects::get_project)
                .patch(handlers::projects::update_project)
                .delete(handlers::projects::delete_project),
        )
        .route(
            "/api/v1/projects/{project_id}/memberships",
            get(handlers::projects::list_project_memberships)
                .post(handlers::projects::upsert_project_membership),
        )
        .route(
            "/api/v1/projects/{project_id}/memberships/{user_id}",
            axum::routing::delete(handlers::projects::delete_project_membership),
        )
        .route(
            "/api/v1/projects/{project_id}/resources",
            get(handlers::projects::list_project_resources)
                .post(handlers::projects::bind_project_resource),
        )
        .route(
            "/api/v1/projects/{project_id}/resources/{resource_kind}/{resource_id}",
            axum::routing::delete(handlers::projects::unbind_project_resource),
        )
        .route(
            "/api/v1/ontology/projects",
            get(handlers::projects::list_projects).post(handlers::projects::create_project),
        )
        .route(
            "/api/v1/ontology/projects/{id}",
            get(handlers::projects::get_project)
                .patch(handlers::projects::update_project)
                .delete(handlers::projects::delete_project),
        )
        .route(
            "/api/v1/ontology/projects/{project_id}/memberships",
            get(handlers::projects::list_project_memberships)
                .post(handlers::projects::upsert_project_membership),
        )
        .route(
            "/api/v1/ontology/projects/{project_id}/memberships/{user_id}",
            axum::routing::delete(handlers::projects::delete_project_membership),
        )
        .route(
            "/api/v1/ontology/projects/{project_id}/resources",
            get(handlers::projects::list_project_resources)
                .post(handlers::projects::bind_project_resource),
        )
        .route(
            "/api/v1/ontology/projects/{project_id}/resources/{resource_kind}/{resource_id}",
            axum::routing::delete(handlers::projects::unbind_project_resource),
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
    tracing::info!("starting tenancy-organizations-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
