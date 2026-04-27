mod config;
mod domain;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{delete, get, patch, post, put},
};
use core_models::{health::HealthStatus, observability};
use sqlx::postgres::PgPoolOptions;

#[derive(Clone)]
pub struct AppState {
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
}

#[tokio::main]
async fn main() {
    observability::init_tracing("authorization-policy-service");

    let cfg = config::AppConfig::from_env().expect("failed to load config");

    let pool = PgPoolOptions::new()
        .max_connections(20)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let state = AppState {
        db: pool,
        jwt_config: jwt_config.clone(),
    };

    let public = Router::new().route(
        "/health",
        get(|| async { axum::Json(HealthStatus::ok("authorization-policy-service")) }),
    );

    let protected = Router::new()
        .route(
            "/api/v1/users/{id}/roles",
            post(handlers::role_mgmt::assign_role),
        )
        .route(
            "/api/v2/admin/users/{id}/roles",
            post(handlers::role_mgmt::assign_role),
        )
        .route(
            "/api/v1/users/{id}/roles/{role_id}",
            delete(handlers::role_mgmt::remove_role),
        )
        .route(
            "/api/v2/admin/users/{id}/roles/{role_id}",
            delete(handlers::role_mgmt::remove_role),
        )
        .route(
            "/api/v1/users/{id}/groups",
            post(handlers::group_mgmt::add_user_to_group),
        )
        .route(
            "/api/v2/admin/users/{id}/groups",
            post(handlers::group_mgmt::add_user_to_group),
        )
        .route(
            "/api/v1/users/{id}/groups/{group_id}",
            delete(handlers::group_mgmt::remove_user_from_group),
        )
        .route(
            "/api/v2/admin/users/{id}/groups/{group_id}",
            delete(handlers::group_mgmt::remove_user_from_group),
        )
        .route(
            "/api/v1/roles",
            get(handlers::role_mgmt::list_roles).post(handlers::role_mgmt::create_role),
        )
        .route(
            "/api/v2/admin/roles",
            get(handlers::role_mgmt::list_roles).post(handlers::role_mgmt::create_role),
        )
        .route("/api/v1/roles/{id}", put(handlers::role_mgmt::update_role))
        .route(
            "/api/v2/admin/roles/{id}",
            put(handlers::role_mgmt::update_role),
        )
        .route(
            "/api/v1/permissions",
            get(handlers::permission_mgmt::list_permissions)
                .post(handlers::permission_mgmt::create_permission),
        )
        .route(
            "/api/v2/admin/permissions",
            get(handlers::permission_mgmt::list_permissions)
                .post(handlers::permission_mgmt::create_permission),
        )
        .route(
            "/api/v1/groups",
            get(handlers::group_mgmt::list_groups).post(handlers::group_mgmt::create_group),
        )
        .route(
            "/api/v2/admin/groups",
            get(handlers::group_mgmt::list_groups).post(handlers::group_mgmt::create_group),
        )
        .route(
            "/api/v1/groups/{id}",
            put(handlers::group_mgmt::update_group),
        )
        .route(
            "/api/v2/admin/groups/{id}",
            put(handlers::group_mgmt::update_group),
        )
        .route(
            "/api/v1/policies",
            get(handlers::policy_mgmt::list_policies).post(handlers::policy_mgmt::create_policy),
        )
        .route(
            "/api/v2/admin/policies",
            get(handlers::policy_mgmt::list_policies).post(handlers::policy_mgmt::create_policy),
        )
        .route(
            "/api/v1/policies/evaluate",
            post(handlers::policy_mgmt::evaluate_policy),
        )
        .route(
            "/api/v2/admin/policies/evaluate",
            post(handlers::policy_mgmt::evaluate_policy),
        )
        .route(
            "/api/v1/policies/{id}",
            put(handlers::policy_mgmt::update_policy).delete(handlers::policy_mgmt::delete_policy),
        )
        .route(
            "/api/v2/admin/policies/{id}",
            patch(handlers::policy_mgmt::update_policy)
                .delete(handlers::policy_mgmt::delete_policy),
        )
        .route(
            "/api/v1/restricted-views",
            get(handlers::restricted_views::list_restricted_views)
                .post(handlers::restricted_views::create_restricted_view),
        )
        .route(
            "/api/v2/admin/restricted-views",
            get(handlers::restricted_views::list_restricted_views)
                .post(handlers::restricted_views::create_restricted_view),
        )
        .route(
            "/api/v1/restricted-views/{id}",
            put(handlers::restricted_views::update_restricted_view)
                .delete(handlers::restricted_views::delete_restricted_view),
        )
        .route(
            "/api/v2/admin/restricted-views/{id}",
            patch(handlers::restricted_views::update_restricted_view)
                .delete(handlers::restricted_views::delete_restricted_view),
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
    tracing::info!("starting authorization-policy-service on {addr}");

    let listener = tokio::net::TcpListener::bind(&addr)
        .await
        .expect("failed to bind");

    axum::serve(listener, app).await.expect("server error");
}
