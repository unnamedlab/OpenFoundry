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
    pub audit_db: sqlx::PgPool,
    pub policy_db: sqlx::PgPool,
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
        .expect("failed to connect to governance database");
    let audit_db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.audit_database_url)
        .await
        .expect("failed to connect to audit database");
    let policy_db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&cfg.policy_database_url)
        .await
        .expect("failed to connect to policy database");

    sqlx::migrate!("./migrations")
        .run(&db)
        .await
        .expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let state = AppState {
        db,
        audit_db,
        policy_db,
        jwt_config: jwt_config.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("security-governance-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/security-governance/project-constraints",
            get(handlers::governance::list_project_constraints)
                .post(handlers::governance::create_project_constraint),
        )
        .route(
            "/api/v1/security-governance/project-constraints/{id}",
            axum::routing::patch(handlers::governance::update_project_constraint),
        )
        .route(
            "/api/v1/security-governance/structural-rules",
            get(handlers::governance::list_structural_security_rules)
                .post(handlers::governance::create_structural_security_rule),
        )
        .route(
            "/api/v1/security-governance/structural-rules/{id}",
            axum::routing::patch(handlers::governance::update_structural_security_rule),
        )
        .route(
            "/api/v1/security-governance/integrity/validate",
            axum::routing::post(handlers::governance::validate_integrity),
        )
        .route(
            "/api/v1/security-governance/templates",
            get(handlers::governance::list_governance_templates),
        )
        .route(
            "/api/v1/security-governance/template-applications",
            get(handlers::governance::list_governance_template_applications),
        )
        .route(
            "/api/v1/security-governance/templates/{slug}/apply",
            axum::routing::post(handlers::governance::apply_governance_template),
        )
        .route(
            "/api/v1/security-governance/compliance/posture",
            get(handlers::governance::get_compliance_posture),
        )
        .route(
            "/api/v1/audit/classifications",
            get(handlers::governance::list_classifications),
        )
        .route(
            "/api/v1/audit/governance/templates",
            get(handlers::governance::list_governance_templates),
        )
        .route(
            "/api/v1/audit/governance/applications",
            get(handlers::governance::list_governance_template_applications),
        )
        .route(
            "/api/v1/audit/governance/templates/{slug}/apply",
            axum::routing::post(handlers::governance::apply_governance_template),
        )
        .route(
            "/api/v1/audit/compliance/posture",
            get(handlers::governance::get_compliance_posture),
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
    tracing::info!("starting security-governance-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}
