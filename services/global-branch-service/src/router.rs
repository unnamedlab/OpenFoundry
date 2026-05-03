//! Axum router + AppState for `global-branch-service`.

use axum::{
    Router,
    routing::{get, post},
};
use sqlx::PgPool;

use crate::global::handlers as global_handlers;

/// Trimmed app state — no JWT layer is wired here yet because the
/// service runs inside the Foundry edge gateway, which already
/// forwards an authenticated principal. The `actor` field is the
/// header-derived caller surfacing through audit events; in
/// production it's set from the JWT, in tests from
/// [`AppState::for_tests`].
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub actor: String,
}

impl AppState {
    pub fn new(db: PgPool, actor: impl Into<String>) -> Self {
        Self {
            db,
            actor: actor.into(),
        }
    }

    /// Convenience for the integration tests.
    pub fn for_tests(db: PgPool) -> Self {
        Self {
            db,
            actor: "test-actor".to_string(),
        }
    }
}

pub fn build_router(state: AppState) -> Router {
    let api = Router::new()
        .route(
            "/v1/global-branches",
            get(global_handlers::list_global_branches)
                .post(global_handlers::create_global_branch),
        )
        .route(
            "/v1/global-branches/{id}",
            get(global_handlers::get_global_branch),
        )
        .route(
            "/v1/global-branches/{id}/links",
            post(global_handlers::add_link),
        )
        .route(
            "/v1/global-branches/{id}/resources",
            get(global_handlers::list_resources),
        )
        .route(
            "/v1/global-branches/{id}/promote",
            post(global_handlers::promote),
        );

    Router::new()
        .merge(api)
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state)
}
