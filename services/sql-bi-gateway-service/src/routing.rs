//! Backend routing for the edge SQL gateway.
//!
//! Every SQL statement that arrives over Flight SQL is classified into one
//! of the [`Backend`]s defined in [ADR-0014]. The classification is purely
//! syntactic (cheap, deterministic, no external metadata fetch) and is
//! based on the **catalog prefix** of the first table reference in the
//! statement:
//!
//! ```text
//!   SELECT * FROM iceberg.sales.orders     -> Backend::Iceberg
//!   SELECT * FROM vespa.documents          -> Backend::Vespa
//!   SELECT * FROM postgres.public.users    -> Backend::Postgres
//!   SELECT * FROM trino.of_lineage.runs    -> Backend::Trino
//!   SELECT 1                               -> Backend::Iceberg (default / DataFusion local)
//! ```
//!
//! The heuristic is intentionally conservative: anything that does not
//! start with a recognised catalog prefix falls back to the local
//! DataFusion `SessionContext`, which is the path used by
//! `SELECT 1`-style probes that Tableau / Superset send during
//! connection bring-up.
//!
//! [ADR-0014]: ../../../../docs/architecture/adr/ADR-0014-retire-trino-flight-sql-only.md

use std::fmt;

use crate::config::AppConfig;

/// Logical backend that owns the data referenced by a SQL statement.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Backend {
    /// Iceberg lakehouse, served by the local DataFusion session or by
    /// `sql-warehousing-service` over Flight SQL when configured.
    Iceberg,
    /// Search / hybrid retrieval (Vespa).
    Vespa,
    /// OLTP reference data (PostgreSQL via CloudNativePG).
    Postgres,
    /// Iceberg analytical engine (Trino, ADR-0029, S5.6). Used for
    /// multi-namespace joins and time-windowed aggregates that the
    /// local DataFusion plan cannot serve efficiently.
    Trino,
}

impl Backend {
    /// Stable string id used in audit logs and the `information_schema`
    /// catalog tree returned to BI clients.
    pub fn as_str(self) -> &'static str {
        match self {
            Backend::Iceberg => "iceberg",
            Backend::Vespa => "vespa",
            Backend::Postgres => "postgres",
            Backend::Trino => "trino",
        }
    }

    /// All backends, in the order they are advertised to BI clients.
    pub fn all() -> &'static [Backend] {
        &[
            Backend::Iceberg,
            Backend::Vespa,
            Backend::Postgres,
            Backend::Trino,
        ]
    }
}

impl fmt::Display for Backend {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// A routing decision: which [`Backend`] owns the statement and, when the
/// backend is fronted by a remote Flight SQL endpoint, the URL to delegate
/// to.
#[derive(Debug, Clone)]
pub struct RoutingDecision {
    pub backend: Backend,
    /// `None` ⇒ execute against the local DataFusion `SessionContext`.
    /// `Some(url)` ⇒ delegate the SQL to a remote Flight SQL endpoint.
    pub remote_endpoint: Option<String>,
}

/// Decides which [`Backend`] to use for a given SQL statement based on the
/// configured endpoints in [`AppConfig`].
#[derive(Debug, Clone)]
pub struct BackendRouter {
    warehousing_flight_sql_url: Option<String>,
    vespa_flight_sql_url: Option<String>,
    postgres_flight_sql_url: Option<String>,
    trino_flight_sql_url: Option<String>,
}

/// Routing failure. Returned when a statement targets a backend that has
/// not been configured.
#[derive(Debug, thiserror::Error)]
pub enum RoutingError {
    #[error(
        "backend `{0}` is not configured on this gateway; \
         set the corresponding `*_FLIGHT_SQL_URL` environment variable \
         (see services/sql-bi-gateway-service/k8s/README.md)"
    )]
    BackendUnavailable(Backend),
}

impl BackendRouter {
    pub fn from_config(config: &AppConfig) -> Self {
        Self {
            warehousing_flight_sql_url: normalize(config.warehousing_flight_sql_url.as_deref()),
            vespa_flight_sql_url: normalize(config.vespa_flight_sql_url.as_deref()),
            postgres_flight_sql_url: normalize(config.postgres_flight_sql_url.as_deref()),
            trino_flight_sql_url: normalize(config.trino_flight_sql_url.as_deref()),
        }
    }

    /// Classify a statement and return the routing decision. Returns
    /// [`RoutingError::BackendUnavailable`] when the chosen backend is one
    /// of the federated ones but no endpoint was configured.
    pub fn route(&self, sql: &str) -> Result<RoutingDecision, RoutingError> {
        let backend = classify(sql);
        let remote_endpoint = match backend {
            Backend::Iceberg => self.warehousing_flight_sql_url.clone(),
            Backend::Vespa => Some(
                self.vespa_flight_sql_url
                    .clone()
                    .ok_or(RoutingError::BackendUnavailable(Backend::Vespa))?,
            ),
            Backend::Postgres => Some(
                self.postgres_flight_sql_url
                    .clone()
                    .ok_or(RoutingError::BackendUnavailable(Backend::Postgres))?,
            ),
            Backend::Trino => Some(
                self.trino_flight_sql_url
                    .clone()
                    .ok_or(RoutingError::BackendUnavailable(Backend::Trino))?,
            ),
        };
        Ok(RoutingDecision {
            backend,
            remote_endpoint,
        })
    }
}

fn normalize(url: Option<&str>) -> Option<String> {
    url.map(str::trim)
        .filter(|s| !s.is_empty())
        .map(str::to_string)
}

/// Classify a SQL statement by inspecting the first identifier that
/// follows the first `FROM` (or `INTO`/`UPDATE`/`JOIN`) keyword. Anything
/// without a recognised catalog prefix is treated as
/// [`Backend::Iceberg`], which routes to the local DataFusion session by
/// default — exactly what `SELECT 1` and similar BI client probes need.
pub fn classify(sql: &str) -> Backend {
    let lowered = sql.to_ascii_lowercase();
    let tokens = lowered.split(|c: char| !c.is_ascii_alphanumeric() && c != '_' && c != '.');
    let mut next_is_table = false;
    for tok in tokens {
        if tok.is_empty() {
            continue;
        }
        if next_is_table {
            if let Some(catalog) = tok.split('.').next() {
                return match catalog {
                    "vespa" => Backend::Vespa,
                    "postgres" | "postgresql" => Backend::Postgres,
                    "trino" => Backend::Trino,
                    _ => Backend::Iceberg,
                };
            }
        }
        next_is_table = matches!(tok, "from" | "into" | "update" | "join");
    }
    Backend::Iceberg
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg(warehousing: Option<&str>, vespa: Option<&str>, postgres: Option<&str>) -> AppConfig {
        cfg_with_trino(warehousing, vespa, postgres, None)
    }

    fn cfg_with_trino(
        warehousing: Option<&str>,
        vespa: Option<&str>,
        postgres: Option<&str>,
        trino: Option<&str>,
    ) -> AppConfig {
        AppConfig {
            host: "127.0.0.1".to_string(),
            port: 0,
            healthz_port: 0,
            database_url: "postgres://test".to_string(),
            jwt_secret: "test".to_string(),
            warehousing_flight_sql_url: warehousing.map(str::to_string),
            vespa_flight_sql_url: vespa.map(str::to_string),
            postgres_flight_sql_url: postgres.map(str::to_string),
            trino_flight_sql_url: trino.map(str::to_string),
            allow_anonymous: false,
        }
    }

    #[test]
    fn classifies_by_first_catalog_prefix() {
        assert_eq!(classify("SELECT 1"), Backend::Iceberg);
        assert_eq!(
            classify("SELECT * FROM iceberg.sales.orders"),
            Backend::Iceberg
        );
        assert_eq!(classify("SELECT * FROM vespa.documents"), Backend::Vespa);
        assert_eq!(
            classify("SELECT * FROM postgres.public.users"),
            Backend::Postgres
        );
        assert_eq!(
            classify("SELECT * FROM postgresql.public.users"),
            Backend::Postgres
        );
        assert_eq!(
            classify("SELECT * FROM trino.of_lineage.runs"),
            Backend::Trino
        );
    }

    #[test]
    fn trino_routes_to_configured_endpoint() {
        let router = BackendRouter::from_config(&cfg_with_trino(
            None,
            None,
            None,
            Some("http://trino-flight-sql-proxy.trino:50133"),
        ));
        let decision = router
            .route("SELECT * FROM trino.of_metrics_long.service_metrics_daily")
            .expect("trino route should succeed when configured");
        assert_eq!(decision.backend, Backend::Trino);
        assert_eq!(
            decision.remote_endpoint.as_deref(),
            Some("http://trino-flight-sql-proxy.trino:50133")
        );
    }

    #[test]
    fn missing_trino_endpoint_is_an_explicit_error() {
        let router = BackendRouter::from_config(&cfg(None, None, None));
        let err = router
            .route("SELECT * FROM trino.of_lineage.runs")
            .expect_err("trino backend must be configured");
        match err {
            RoutingError::BackendUnavailable(Backend::Trino) => {}
            other => panic!("unexpected error: {other:?}"),
        }
    }

    #[test]
    fn backend_all_includes_trino() {
        assert!(Backend::all().contains(&Backend::Trino));
        assert_eq!(Backend::Trino.as_str(), "trino");
    }

    #[test]
    fn join_and_update_are_recognised_as_table_anchors() {
        assert_eq!(
            classify("SELECT * FROM iceberg.t1 JOIN vespa.t2 USING(id)"),
            Backend::Iceberg,
            "first FROM target wins (cross-backend joins must be planned in DF)"
        );
        assert_eq!(
            classify("UPDATE postgres.public.users SET active=true"),
            Backend::Postgres
        );
    }

    #[test]
    fn local_iceberg_routes_to_local_datafusion_when_warehousing_unconfigured() {
        let router = BackendRouter::from_config(&cfg(None, None, None));
        let decision = router.route("SELECT 1").expect("should route");
        assert_eq!(decision.backend, Backend::Iceberg);
        assert!(decision.remote_endpoint.is_none());
    }

    #[test]
    fn iceberg_delegates_to_warehousing_when_configured() {
        let router = BackendRouter::from_config(&cfg(
            Some("http://sql-warehousing-service:50123"),
            None,
            None,
        ));
        let decision = router
            .route("SELECT * FROM iceberg.sales.orders")
            .expect("should route");
        assert_eq!(decision.backend, Backend::Iceberg);
        assert_eq!(
            decision.remote_endpoint.as_deref(),
            Some("http://sql-warehousing-service:50123")
        );
    }

    #[test]
    fn empty_endpoint_string_is_treated_as_unconfigured() {
        let router = BackendRouter::from_config(&cfg(Some("   "), None, None));
        let decision = router.route("SELECT 1").expect("should route");
        assert!(decision.remote_endpoint.is_none());
    }
}
