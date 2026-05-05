use axum::{Router, routing::get};
use sqlx::PgPool;

pub mod handlers;
pub mod models;
pub mod schema_registry;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
}

pub fn routes() -> Router<AppState> {
    let stream_routes = Router::new()
        .route(
            "/streams",
            get(handlers::list_streams).post(handlers::register_stream),
        )
        .route("/streams/:id", get(handlers::get_stream))
        .route(
            "/streams/:id/checkpoint",
            get(handlers::get_checkpoint).post(handlers::record_checkpoint),
        )
        .route(
            "/streams/:id/resolution",
            get(handlers::get_resolution).put(handlers::update_resolution),
        );

    Router::new()
        .merge(schema_registry::routes())
        .nest("/cdc", stream_routes.clone())
        .merge(stream_routes)
}
