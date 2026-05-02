//! Prometheus metric registry for the dataset-versioning service.
//!
//! T8.1 — Foundry-aligned `dataset_*` family covering:
//!
//!   * `dataset_transactions_open` — gauge per `(dataset_rid, branch)`,
//!     tracks the unique OPEN transaction each branch may carry at a
//!     time. Inc on `start_transaction` success; dec on commit/abort.
//!   * `dataset_transactions_committed_total{type}` — counter labelled
//!     by `SNAPSHOT|APPEND|UPDATE|DELETE`.
//!   * `dataset_transactions_aborted_total` — counter.
//!   * `dataset_view_compute_duration_seconds` — histogram around the
//!     `compute_view_at` algorithm (the hot path that materialises a
//!     branch view from its committed transactions).
//!   * `dataset_branch_fallback_resolutions_total{outcome=hit|miss}` —
//!     counter incremented by the (forthcoming) fallback resolver.
//!     Both label values are pre-registered at boot so the dashboard
//!     panels render immediately even before the first resolution.

use once_cell::sync::Lazy;
use prometheus::{
    Histogram, HistogramOpts, IntCounter, IntCounterVec, IntGauge, IntGaugeVec, Opts,
    register_histogram, register_int_counter, register_int_counter_vec, register_int_gauge_vec,
};

/// Legacy generic counter retained from Bloque 7 for the smoke test.
pub static DATASET_TX_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_versioning_transactions_total",
            "Versioning transactions handled, labelled by op (open|commit|abort)",
        ),
        &["op"],
    )
    .expect("register dataset_versioning_transactions_total")
});

pub static DATASET_VERSIONING_INFO: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_versioning_service_info",
        "Constant 1 once the versioning service has registered metrics",
    )
    .expect("register dataset_versioning_service_info")
});

/// Number of currently-OPEN transactions, keyed by `(dataset_rid,
/// branch)`. Foundry guarantees at most one OPEN per branch — the
/// gauge therefore peaks at the number of branches with work in flight.
pub static DATASET_TRANSACTIONS_OPEN: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "dataset_transactions_open",
            "Currently OPEN dataset transactions per (dataset_rid, branch)",
        ),
        &["dataset_rid", "branch"],
    )
    .expect("register dataset_transactions_open")
});

/// Total committed transactions, labelled by Foundry `tx_type`.
pub static DATASET_TRANSACTIONS_COMMITTED_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_transactions_committed_total",
            "Total committed dataset transactions, labelled by Foundry transaction type",
        ),
        &["type"],
    )
    .expect("register dataset_transactions_committed_total")
});

/// Total aborted transactions (any branch / type).
pub static DATASET_TRANSACTIONS_ABORTED_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_transactions_aborted_total",
        "Total aborted dataset transactions",
    )
    .expect("register dataset_transactions_aborted_total")
});

/// End-to-end duration of the `compute_view_at` algorithm. Buckets are
/// chosen for read paths that range from instantaneous (cached, single
/// SNAPSHOT) to multi-second (long branch with many APPENDs).
pub static DATASET_VIEW_COMPUTE_DURATION_SECONDS: Lazy<Histogram> = Lazy::new(|| {
    register_histogram!(HistogramOpts::new(
        "dataset_view_compute_duration_seconds",
        "Wall-clock seconds spent assembling a branch view from its committed transactions",
    )
    .buckets(vec![
        0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
    ]))
    .expect("register dataset_view_compute_duration_seconds")
});

/// Branch fallback resolution outcomes. `hit` = a fallback branch
/// satisfied a read; `miss` = no fallback configured / none usable.
pub static DATASET_BRANCH_FALLBACK_RESOLUTIONS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_branch_fallback_resolutions_total",
            "Branch fallback resolutions, labelled by outcome (hit|miss)",
        ),
        &["outcome"],
    )
    .expect("register dataset_branch_fallback_resolutions_total")
});

/// T9.2 — RBAC denials at dataset-versioning mutation endpoints,
/// labelled by the high-level operation that was rejected
/// (branch.create, branch.delete, transaction.open, …). Distinct from
/// marking-enforcement denials; surfaces missing scopes vs. clearance
/// gaps.
pub static DATASET_RBAC_DENIALS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_rbac_denials_total",
            "Dataset mutation requests rejected by the in-process RBAC gate",
        ),
        &["operation"],
    )
    .expect("register dataset_rbac_denials_total")
});

/// Sugar — `metrics::open_gauge(rid, branch).inc()` is more readable
/// than threading `with_label_values` through every handler.
pub fn open_gauge(dataset_rid: &str, branch: &str) -> IntGauge {
    DATASET_TRANSACTIONS_OPEN.with_label_values(&[dataset_rid, branch])
}

/// Touch every lazy static so `prometheus::gather()` returns the full
/// `dataset_*` family even before the first request.
pub fn init() {
    DATASET_VERSIONING_INFO.inc();
    let _ = DATASET_TX_TOTAL.with_label_values(&["bootstrap"]);

    for ty in ["SNAPSHOT", "APPEND", "UPDATE", "DELETE"] {
        let _ = DATASET_TRANSACTIONS_COMMITTED_TOTAL.with_label_values(&[ty]);
    }
    let _ = DATASET_TRANSACTIONS_ABORTED_TOTAL.get();
    let _ = DATASET_VIEW_COMPUTE_DURATION_SECONDS.get_sample_count();
    for outcome in ["hit", "miss"] {
        let _ = DATASET_BRANCH_FALLBACK_RESOLUTIONS_TOTAL.with_label_values(&[outcome]);
    }
    for op in [
        "branch.create",
        "branch.delete",
        "branch.reparent",
        "branch.fallbacks.update",
        "transaction.open",
        "transaction.commit",
        "transaction.abort",
    ] {
        let _ = DATASET_RBAC_DENIALS_TOTAL.with_label_values(&[op]);
    }
}
