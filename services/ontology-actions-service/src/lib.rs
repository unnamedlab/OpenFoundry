//! `ontology-actions-service` library surface.
//!
//! The service binary in `src/main.rs` consumes this crate to build its Axum
//! router. Tests under `tests/` import [`build_router`] to drive the same
//! routes against a `tower::ServiceExt::oneshot` client without binding a
//! TCP socket.

pub mod config;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    middleware,
    routing::{delete, get, post},
};
use ontology_kernel::{
    AppState,
    handlers::actions::{
        create_action_type, create_action_what_if_branch, delete_action_type,
        delete_action_what_if_branch, execute_action, execute_action_batch, execute_inline_edit,
        get_action_type, list_action_types, list_action_what_if_branches, update_action_type,
        validate_action,
    },
};

/// Mounts the full HTTP surface of `ontology-actions-service` under
/// `/api/v1`. The `auth_middleware::layer::auth_layer` is applied to the
/// nested router so every Action types endpoint requires a valid Bearer
/// token — `GET /health` and `GET /metrics` stay open and are added by the
/// binary in `main.rs`.
pub fn build_router(state: AppState) -> Router {
    let actions = Router::new()
        .route(
            "/actions",
            get(list_action_types).post(create_action_type),
        )
        .route(
            "/actions/{id}",
            get(get_action_type)
                .put(update_action_type)
                .delete(delete_action_type),
        )
        .route("/actions/{id}/validate", post(validate_action))
        .route("/actions/{id}/execute", post(execute_action))
        .route(
            "/actions/{id}/execute-batch",
            post(execute_action_batch),
        )
        .route(
            "/actions/{id}/what-if",
            get(list_action_what_if_branches).post(create_action_what_if_branch),
        )
        .route(
            "/actions/{id}/what-if/{branch_id}",
            delete(delete_action_what_if_branch),
        )
        .route(
            "/types/{type_id}/properties/{property_id}/objects/{obj_id}/inline-edit",
            post(execute_inline_edit),
        );

    Router::new()
        .nest("/api/v1/ontology", actions)
        .layer(middleware::from_fn_with_state(
            state.jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ))
        .with_state(state)
}

/// Convenience constructor used by both the binary and integration tests.
pub fn jwt_config_from_secret(secret: &str) -> JwtConfig {
    JwtConfig::new(secret).with_env_defaults()
}
