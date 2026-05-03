//! Hot-path / historical query routing substrate (S5.2.c).
//!
//! Reads inside the lineage query API split into:
//!
//! * **Hot path** (≤ 24 h old): served from Cassandra (`lineage_runs`,
//!   `lineage_events_by_run` plus the operational dataset/column
//!   adjacency rows derived from the same firehose). Sub-100 ms P99 is
//!   the SLO.
//! * **Historical** (> 24 h): served from Trino over the Iceberg
//!   tables in `of_lineage.*`, including historical graph traversals
//!   reconstructed from `runs/events/datasets_io`. Trino is wired in
//!   S5.6 — until then callers may degrade to the Cassandra read-model,
//!   but never to `lineage_relations` / `lineage_edges` in PostgreSQL.
//!   PostgreSQL lineage tables are archive/metadata-only and must not be
//!   treated as a serving tier.
//!
//! This module is pure logic so the routing decision is unit-testable
//! and consistent across handlers.

use std::time::Duration;

/// Threshold below which a query is "hot" — last 24 h per the plan.
pub const HOT_WINDOW: Duration = Duration::from_secs(24 * 60 * 60);
pub const HOT_WINDOW_HOURS: u32 = 24;

/// The historical Trino/Iceberg reader is not wired yet in this crate,
/// so historical requests currently degrade to the Cassandra read-model.
pub const TRINO_READER_IMPLEMENTED: bool = false;

/// Where the lineage query API should send the read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QuerySource {
    /// Cassandra hot path (`lineage_runs`, `lineage_events_by_run`,
    /// operational adjacency / dataset-io indexes).
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

/// Logical lineage read shapes exposed by the HTTP API.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum QueryKind {
    DatasetGraph,
    DatasetImpact,
    DatasetColumns,
    FullGraph,
}

impl QueryKind {
    /// Dataset-scoped reads stay on the hot path by default; the full
    /// graph defaults to historical mode because it typically exceeds the
    /// operational Cassandra working set.
    pub const fn default_window_hours(self) -> u32 {
        match self {
            QueryKind::DatasetGraph | QueryKind::DatasetImpact | QueryKind::DatasetColumns => {
                HOT_WINDOW_HOURS
            }
            QueryKind::FullGraph => HOT_WINDOW_HOURS + 1,
        }
    }

    pub const fn as_scope_label(self) -> &'static str {
        match self {
            QueryKind::DatasetGraph => "dataset_graph",
            QueryKind::DatasetImpact => "dataset_impact",
            QueryKind::DatasetColumns => "dataset_columns",
            QueryKind::FullGraph => "full_graph",
        }
    }
}

/// Concrete routing decision for a lineage API read.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct QueryPlan {
    pub kind: QueryKind,
    pub window_hours: u32,
    pub requested_source: QuerySource,
    pub selected_source: QuerySource,
    pub degraded: bool,
}

impl QueryPlan {
    pub const fn is_historical(self) -> bool {
        matches!(self.requested_source, QuerySource::Trino)
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
    matches!(
        env.map(str::trim),
        Some("1") | Some("true") | Some("TRUE") | Some("yes")
    )
}

/// Trino can only serve historical reads when it is both enabled in the
/// deployment and implemented in this crate.
#[must_use]
pub fn trino_available_from_env(env: Option<&str>) -> bool {
    trino_enabled_from_env(env) && TRINO_READER_IMPLEMENTED
}

/// Build a handler/domain-visible routing plan. When historical Trino is
/// requested but unavailable, callers must serve the Cassandra
/// read-model as a degraded fallback and never pivot to PostgreSQL
/// lineage relation tables.
#[must_use]
pub fn plan(
    kind: QueryKind,
    window_hours: Option<u32>,
    historical: bool,
    trino_available: bool,
) -> QueryPlan {
    let mut resolved_window = window_hours.unwrap_or_else(|| kind.default_window_hours());
    if historical && resolved_window <= HOT_WINDOW_HOURS {
        resolved_window = HOT_WINDOW_HOURS + 1;
    }

    let requested_source = route_window(resolved_window);
    let selected_source = match requested_source {
        QuerySource::Cassandra => QuerySource::Cassandra,
        QuerySource::Trino if trino_available => QuerySource::Trino,
        QuerySource::Trino => QuerySource::Cassandra,
    };

    QueryPlan {
        kind,
        window_hours: resolved_window,
        requested_source,
        selected_source,
        degraded: requested_source != selected_source,
    }
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

    #[test]
    fn full_graph_defaults_to_historical_and_degrades_without_trino() {
        let plan = plan(QueryKind::FullGraph, None, false, false);
        assert_eq!(plan.window_hours, 25);
        assert_eq!(plan.requested_source, QuerySource::Trino);
        assert_eq!(plan.selected_source, QuerySource::Cassandra);
        assert!(plan.degraded);
    }

    #[test]
    fn dataset_queries_default_to_hot_path() {
        let plan = plan(QueryKind::DatasetGraph, None, false, false);
        assert_eq!(plan.window_hours, 24);
        assert_eq!(plan.requested_source, QuerySource::Cassandra);
        assert_eq!(plan.selected_source, QuerySource::Cassandra);
        assert!(!plan.degraded);
    }

    #[test]
    fn historical_flag_forces_historical_window() {
        let plan = plan(QueryKind::DatasetImpact, Some(1), true, false);
        assert_eq!(plan.window_hours, 25);
        assert_eq!(plan.requested_source, QuerySource::Trino);
        assert!(plan.degraded);
    }

    #[test]
    fn trino_requires_flag_and_implementation() {
        assert!(!trino_available_from_env(Some("true")));
        assert!(!trino_available_from_env(None));
    }
}
