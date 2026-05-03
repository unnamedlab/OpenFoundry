//! `monitoring-rules-service` library surface.
//!
//! Today this crate exposes the *contract* for the rule kinds that
//! `ontology-actions-service` emits metrics for (see [`models::ActionRuleKind`]).
//! The full alerting engine is a separate workstream; the binary in
//! `src/main.rs` will mount [`build_router`] once the dependencies it needs
//! (database connection, JWT layer, dispatcher) are wired up.

pub mod config;
pub mod evaluator;
pub mod handlers;
pub mod models;
pub mod streaming_handlers;
pub mod streaming_monitors;

use sqlx::PgPool;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
}
