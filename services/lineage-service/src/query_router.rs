//! Hot-path / historical query routing substrate (S5.2.c).
//!
//! Reads inside the lineage query API split into:
//!
//! * **Hot path** (≤ 24 h old): served from Cassandra (`lineage_runs`,
//!   `lineage_events_by_run`). Sub-100 ms P99 is the SLO.
//! * **Historical** (> 24 h): served from Trino over the Iceberg
//!   tables in `of_lineage.*`. Trino is wired in S5.6 — until then
//!   `historical_source` returns [`QuerySource::Trino`] and the caller
//!   falls back to Cassandra with a `Sec-Fetch-Lineage: degraded`
//!   header (handled by [`crate::handlers`] when the runtime PR ships).
//!
//! This module is pure logic so the routing decision is unit-testable
//! and consistent across handlers.

use std::time::Duration;

/// Threshold below which a query is "hot" — last 24 h per the plan.
pub const HOT_WINDOW: Duration = Duration::from_secs(24 * 60 * 60);

/// Where the lineage query API should send the read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QuerySource {
    /// Cassandra hot path (`lineage_runs`, `lineage_events_by_run`).
    Cassandra,
    /// Trino over Iceberg `of_lineage.*` (lands when S5.6 deploys
    /// Trino; until then handlers MUST treat this as a 503-degraded
    /// answer and may fall back to Cassandra full scan).
    Trino,
}

impl QuerySource {
    /// Stable identifier for metrics labels.
    pub const fn as_metric_label(self) -> &'static str {
        match self {
            QuerySource::Cassandra => "cassandra",
            QuerySource::Trino => "trino",
        }
    }
}

/// Decide which backend to hit given the **age of the oldest row** the
/// query needs. `now` and `oldest_needed` are caller-supplied to keep
/// the function pure (no `SystemTime::now()` inside).
pub fn route(now_unix_secs: i64, oldest_needed_unix_secs: i64) -> QuerySource {
    let age = now_unix_secs.saturating_sub(oldest_needed_unix_secs);
    if age <= HOT_WINDOW.as_secs() as i64 {
        QuerySource::Cassandra
    } else {
        QuerySource::Trino
    }
}

/// Convenience overload taking a *requested window* in hours.
pub fn route_window(window_hours: u32) -> QuerySource {
    if (window_hours as u64) * 3600 <= HOT_WINDOW.as_secs() {
        QuerySource::Cassandra
    } else {
        QuerySource::Trino
    }
}

/// Whether the Trino backend is actually reachable in this deployment.
/// Reads `LINEAGE_TRINO_ENABLED` (default `false` until S5.6 lands).
/// Handlers AND-this with `route(...)` to decide on graceful fallback.
#[must_use]
pub fn trino_enabled_from_env(env: Option<&str>) -> bool {
    matches!(env.map(str::trim), Some("1") | Some("true") | Some("TRUE") | Some("yes"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn last_hour_routes_to_cassandra() {
        assert_eq!(route(1_000_000, 1_000_000 - 3600), QuerySource::Cassandra);
    }

    #[test]
    fn over_one_day_routes_to_trino() {
        assert_eq!(
            route(1_000_000, 1_000_000 - (25 * 3600)),
            QuerySource::Trino
        );
    }

    #[test]
    fn boundary_at_24h_is_hot() {
        // exactly 24h → still Cassandra (inclusive).
        assert_eq!(
            route(1_000_000, 1_000_000 - (24 * 3600)),
            QuerySource::Cassandra
        );
    }

    #[test]
    fn window_24h_hot() {
        assert_eq!(route_window(24), QuerySource::Cassandra);
        assert_eq!(route_window(25), QuerySource::Trino);
    }

    #[test]
    fn metric_labels_stable() {
        assert_eq!(QuerySource::Cassandra.as_metric_label(), "cassandra");
        assert_eq!(QuerySource::Trino.as_metric_label(), "trino");
    }

    #[test]
    fn trino_disabled_by_default() {
        assert!(!trino_enabled_from_env(None));
        assert!(!trino_enabled_from_env(Some("")));
        assert!(!trino_enabled_from_env(Some("0")));
        assert!(trino_enabled_from_env(Some("true")));
        assert!(trino_enabled_from_env(Some("1")));
    }
}
