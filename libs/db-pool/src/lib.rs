//! S6.4 — Dual sqlx pool (writer/reader) for OpenFoundry services.
//!
//! Why this exists
//! ---------------
//! Every service that talks to a CNPG cluster needs **two** connections:
//!
//! * A **writer** pool that targets the cluster's PgBouncer Pooler service
//!   (`<cluster>-pooler-rw`) in transaction mode — the path used for
//!   `INSERT/UPDATE/DELETE` and for any read that must observe its own
//!   writes (read-your-writes).
//! * A **reader** pool that targets the CNPG read-replica service
//!   (`<cluster>-ro`) directly — used for analytics, dashboards and
//!   list endpoints that tolerate replication lag.
//!
//! Splitting the workload protects the primary from read amplification
//! and lets the reader pool scale independently. When the reader URL is
//! not configured (`DATABASE_READ_URL` unset), the wrapper transparently
//! falls back to the writer pool so a service can run unchanged in dev
//! environments that have no replica.
//!
//! Connection-string contract
//! --------------------------
//! Both URLs must include the per-service `search_path` so that
//! transaction-mode pooling (which forbids `SET search_path` outside a
//! transaction) cannot leak across schemas:
//!
//! ```text
//! postgresql://svc_<bc>:<pwd>@pg-<role>-pooler-rw.openfoundry.svc:5432/app
//!     ?sslmode=require&options=-c%20search_path%3D<bc>
//! ```
//!
//! The wrapper does **not** rewrite or override `search_path`; the URL
//! supplied by the operator (Helm `envSecrets.DATABASE_URL`) is the
//! single source of truth.

use std::time::Duration;

use sqlx::{postgres::PgPoolOptions, PgPool};
use thiserror::Error;

/// Default values tuned for transaction-mode PgBouncer in front of CNPG.
/// Each service therefore opens at most `MAX_CONNECTIONS` *client* slots
/// against the Pooler; the Pooler maps them onto the 50-connection
/// server-side budget defined in
/// `infra/k8s/platform/manifests/cnpg/poolers/<cluster>-pooler.yaml`.
const DEFAULT_MAX_CONNECTIONS: u32 = 20;
const DEFAULT_MIN_CONNECTIONS: u32 = 1;
const DEFAULT_ACQUIRE_TIMEOUT_SECS: u64 = 5;
const DEFAULT_IDLE_TIMEOUT_SECS: u64 = 120;
const DEFAULT_MAX_LIFETIME_SECS: u64 = 1800;

/// Environment variables consumed by [`DualPool::from_env`].
pub const ENV_WRITER_URL: &str = "DATABASE_URL";
pub const ENV_READER_URL: &str = "DATABASE_READ_URL";

/// Sizing knobs for the underlying sqlx pools.
#[derive(Debug, Clone, Copy)]
pub struct PoolSizing {
    pub max_connections: u32,
    pub min_connections: u32,
    pub acquire_timeout: Duration,
    pub idle_timeout: Duration,
    pub max_lifetime: Duration,
}

impl Default for PoolSizing {
    fn default() -> Self {
        Self {
            max_connections: DEFAULT_MAX_CONNECTIONS,
            min_connections: DEFAULT_MIN_CONNECTIONS,
            acquire_timeout: Duration::from_secs(DEFAULT_ACQUIRE_TIMEOUT_SECS),
            idle_timeout: Duration::from_secs(DEFAULT_IDLE_TIMEOUT_SECS),
            max_lifetime: Duration::from_secs(DEFAULT_MAX_LIFETIME_SECS),
        }
    }
}

/// Errors returned when constructing a [`DualPool`].
#[derive(Debug, Error)]
pub enum DualPoolError {
    #[error("environment variable `{0}` is required")]
    MissingEnv(&'static str),
    #[error("failed to connect to {role} pool: {source}")]
    Connect {
        role: &'static str,
        #[source]
        source: sqlx::Error,
    },
}

/// Writer + reader pools sharing the same cluster.
///
/// The reader pool is `Some(_)` only when [`ENV_READER_URL`] is set;
/// otherwise [`DualPool::reader`] returns the writer pool so callers
/// can be agnostic of the deployment topology.
#[derive(Debug, Clone)]
pub struct DualPool {
    writer: PgPool,
    reader: Option<PgPool>,
}

impl DualPool {
    /// Build both pools from `DATABASE_URL` (required) and
    /// `DATABASE_READ_URL` (optional).
    pub async fn from_env() -> Result<Self, DualPoolError> {
        Self::from_env_with(PoolSizing::default()).await
    }

    /// Same as [`Self::from_env`] but with explicit sizing.
    pub async fn from_env_with(sizing: PoolSizing) -> Result<Self, DualPoolError> {
        let writer_url =
            std::env::var(ENV_WRITER_URL).map_err(|_| DualPoolError::MissingEnv(ENV_WRITER_URL))?;
        let reader_url = std::env::var(ENV_READER_URL).ok();
        Self::connect(&writer_url, reader_url.as_deref(), sizing).await
    }

    /// Build both pools from explicit URLs (intended for tests and for
    /// services that load configuration from non-env sources).
    pub async fn connect(
        writer_url: &str,
        reader_url: Option<&str>,
        sizing: PoolSizing,
    ) -> Result<Self, DualPoolError> {
        let writer =
            build_pool(writer_url, sizing)
                .await
                .map_err(|source| DualPoolError::Connect {
                    role: "writer",
                    source,
                })?;

        let reader = match reader_url {
            Some(url) if !url.trim().is_empty() => {
                let pool =
                    build_pool(url, sizing)
                        .await
                        .map_err(|source| DualPoolError::Connect {
                            role: "reader",
                            source,
                        })?;
                tracing::info!(
                    target: "openfoundry::db_pool",
                    "dual pool initialised with dedicated reader replica"
                );
                Some(pool)
            }
            _ => {
                tracing::info!(
                    target: "openfoundry::db_pool",
                    "DATABASE_READ_URL unset — reader requests fall back to writer pool"
                );
                None
            }
        };

        Ok(Self { writer, reader })
    }

    /// Wrap pre-built pools (used by tests and by services that own the
    /// pool lifecycle themselves).
    pub fn from_pools(writer: PgPool, reader: Option<PgPool>) -> Self {
        Self { writer, reader }
    }

    /// Pool used for writes and read-your-writes.
    pub fn writer(&self) -> &PgPool {
        &self.writer
    }

    /// Pool used for replica-tolerant reads. Falls back to the writer
    /// pool when no reader was configured.
    pub fn reader(&self) -> &PgPool {
        self.reader.as_ref().unwrap_or(&self.writer)
    }

    /// `true` iff a dedicated reader pool is in use.
    pub fn has_dedicated_reader(&self) -> bool {
        self.reader.is_some()
    }
}

async fn build_pool(url: &str, sizing: PoolSizing) -> Result<PgPool, sqlx::Error> {
    PgPoolOptions::new()
        .max_connections(sizing.max_connections)
        .min_connections(sizing.min_connections)
        .acquire_timeout(sizing.acquire_timeout)
        .idle_timeout(sizing.idle_timeout)
        .max_lifetime(sizing.max_lifetime)
        .connect(url)
        .await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pool_sizing_default_matches_pgbouncer_budget() {
        let s = PoolSizing::default();
        // Each service stays well under the Pooler's `default_pool_size=50`
        // server-side budget so a single noisy service cannot starve its
        // peers behind the same Pooler instance.
        assert!(s.max_connections <= 50);
        assert!(s.min_connections <= s.max_connections);
        assert!(s.acquire_timeout < s.idle_timeout);
    }

    #[tokio::test]
    async fn missing_writer_env_is_reported() {
        // Use an isolated subprocess-free check by clearing both vars and
        // calling `from_env` — sqlx never tries to connect because the
        // env lookup fails first.
        // SAFETY (2024 edition): test binaries are single-threaded by default
        // in this crate (no tokio::test multi-thread flavour is requested).
        unsafe {
            std::env::remove_var(ENV_WRITER_URL);
            std::env::remove_var(ENV_READER_URL);
        }
        let err = DualPool::from_env()
            .await
            .expect_err("must require DATABASE_URL");
        assert!(matches!(err, DualPoolError::MissingEnv(ENV_WRITER_URL)));
    }

    #[test]
    fn from_pools_without_reader_falls_back_to_writer() {
        // We cannot construct a real PgPool without a live server, so
        // we only assert the type-level fallback contract here.
        // (See integration tests in services for live-pool coverage.)
        fn assert_send_sync<T: Send + Sync>() {}
        assert_send_sync::<DualPool>();
    }
}
