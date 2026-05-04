//! Per-input view filter — Foundry "InputSpecs specify a subset of
//! data to read from a dataset in terms of its views".
//!
//! Persisted on each [`crate::domain::build_resolution::InputSpec`] as
//! `view_filter`. The build resolver converts the (potentially
//! relative) selectors into a concrete view id / transaction window
//! per dataset, persists the resolution into
//! `jobs.input_view_resolutions`, and the runner replays from that.

use serde::{Deserialize, Serialize};

/// Selector against a dataset's view timeline.
///
/// `RANGE` and `INCREMENTAL_SINCE_LAST_BUILD` materialise into a
/// `(from_transaction, to_transaction)` window that the runner reads
/// as a delta; the other two materialise into a single point-in-time
/// view id.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ViewFilter {
    /// View at a wall-clock timestamp (RFC3339).
    AtTimestamp { value: String },
    /// View pinned to a specific committed transaction.
    AtTransaction { transaction_rid: String },
    /// Window from `from_transaction` (exclusive) to `to_transaction`
    /// (inclusive). Both must be committed transactions on the
    /// resolved branch.
    Range {
        from_transaction: String,
        to_transaction: String,
    },
    /// Incremental window since the most recent COMPLETED job for
    /// this `(pipeline_rid, build_branch, output_dataset_rids)`. The
    /// resolver looks up the previous job's `range_to_transaction_rid`
    /// for the same input dataset and uses it as the lower bound.
    IncrementalSinceLastBuild,
}

/// Resolved counterpart persisted in `jobs.input_view_resolutions`.
/// Includes the original filter for traceability and the concrete
/// outputs the runner needs.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResolvedViewFilter {
    pub dataset_rid: String,
    pub branch: String,
    pub filter: ViewFilter,
    /// Set when the filter materialises to a single-view selector
    /// (`AT_TIMESTAMP`, `AT_TRANSACTION`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub resolved_view_id: Option<String>,
    /// Mirrors `resolved_view_id`: the transaction whose HEAD the
    /// runner will read.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub resolved_transaction_rid: Option<String>,
    /// Set for windowed selectors (`RANGE`,
    /// `INCREMENTAL_SINCE_LAST_BUILD`).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub range_from_transaction_rid: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub range_to_transaction_rid: Option<String>,
    /// Optional explanation for tracing — the resolver records *why*
    /// it landed on this resolution (e.g. "no prior build, falling
    /// back to current view").
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub note: Option<String>,
}

impl ResolvedViewFilter {
    /// True when the resolution carries a usable target — either a
    /// single view id or both ends of a range.
    pub fn is_concrete(&self) -> bool {
        self.resolved_view_id.is_some()
            || self.resolved_transaction_rid.is_some()
            || (self.range_from_transaction_rid.is_some()
                && self.range_to_transaction_rid.is_some())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn view_filter_round_trips_through_serde() {
        let cases = vec![
            ViewFilter::AtTimestamp {
                value: "2026-04-01T00:00:00Z".into(),
            },
            ViewFilter::AtTransaction {
                transaction_rid: "ri.txn.1".into(),
            },
            ViewFilter::Range {
                from_transaction: "ri.txn.0".into(),
                to_transaction: "ri.txn.5".into(),
            },
            ViewFilter::IncrementalSinceLastBuild,
        ];
        for f in cases {
            let raw = serde_json::to_value(&f).unwrap();
            // The `kind` tag must use SCREAMING_SNAKE_CASE.
            assert!(raw.get("kind").and_then(|v| v.as_str()).is_some());
            let decoded: ViewFilter = serde_json::from_value(raw).unwrap();
            assert_eq!(decoded, f);
        }
    }

    #[test]
    fn is_concrete_reflects_resolution_state() {
        let mut r = ResolvedViewFilter {
            dataset_rid: "d".into(),
            branch: "master".into(),
            filter: ViewFilter::IncrementalSinceLastBuild,
            resolved_view_id: None,
            resolved_transaction_rid: None,
            range_from_transaction_rid: None,
            range_to_transaction_rid: None,
            note: None,
        };
        assert!(!r.is_concrete());
        r.range_from_transaction_rid = Some("a".into());
        r.range_to_transaction_rid = Some("b".into());
        assert!(r.is_concrete());
    }
}
