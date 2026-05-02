//! Postgres pool builder for the consolidated `pg-schemas` cluster.
//!
//! The connection URL is supplied by the operator and points at the
//! `pg-schemas` cluster (S1.6.a). The schema `search_path` is applied
//! through sqlx connect-options so callers do not need to fully-qualify
//! every identifier — the kernel handlers can keep saying
//! `SELECT * FROM object_types` and the planner resolves it inside
//! `ontology_schema` (S1.6.b / S1.6.d).

use std::str::FromStr;

use sqlx::ConnectOptions;
use sqlx::postgres::{PgConnectOptions, PgPool, PgPoolOptions};

#[derive(Debug, thiserror::Error)]
pub enum BuildPoolError {
    #[error("invalid DATABASE_URL: {0}")]
    InvalidUrl(#[from] sqlx::Error),
}

/// Build a [`PgPool`] with `search_path` set to the configured schema.
///
/// The schema name is interpolated literally into the connect option;
/// it MUST come from configuration, never from user input — Postgres
/// has no parameter binding for `SET`. Callers should pin it to a
/// known constant such as [`crate::config::DEFAULT_PG_SCHEMA`].
pub async fn build_pool(database_url: &str, schema: &str) -> Result<PgPool, BuildPoolError> {
    let mut options = PgConnectOptions::from_str(database_url)?;

    // S1.6.d — apply the schema search_path at the connection level.
    // sqlx forwards this as a libpq `options=-c search_path=…` start-up
    // packet so it survives reconnects and pool resets.
    options = options.options([("search_path", schema)]);

    // Quieter logs for hot CRUD; operators can dial it up via env.
    options = options.log_statements(tracing::log::LevelFilter::Debug);

    let pool = PgPoolOptions::new()
        .max_connections(16)
        .connect_with(options)
        .await?;

    Ok(pool)
}
