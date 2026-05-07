//! Axum router + AppState for `code-repository-review-service`.

use axum::{
    Router,
    routing::{get, post},
};
use sqlx::PgPool;

use crate::global_branch::handlers as global_branch_handlers;

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

    pub fn for_tests(db: PgPool) -> Self {
        Self {
            db,
            actor: "test-actor".to_string(),
        }
    }
}

pub fn build_router(state: AppState) -> Router {
    let global_branch_api = Router::new()
        .route(
            "/v1/global-branches",
            get(global_branch_handlers::list_global_branches)
                .post(global_branch_handlers::create_global_branch),
        )
        .route(
            "/v1/global-branches/{id}",
            get(global_branch_handlers::get_global_branch),
        )
        .route(
            "/v1/global-branches/{id}/links",
            post(global_branch_handlers::add_link),
        )
        .route(
            "/v1/global-branches/{id}/resources",
            get(global_branch_handlers::list_resources),
        )
        .route(
            "/v1/global-branches/{id}/promote",
            post(global_branch_handlers::promote),
        );

    Router::new()
        .merge(global_branch_api)
        .route("/healthz", get(|| async { "ok" }))
        .with_state(state)
}
