//! P6 — Foundry-aligned `dataset_*` quality metrics.
//!
//! Mirrors the metric names listed in the closing-task spec:
//!   * `dataset_freshness_seconds{rid}`        — gauge (live).
//!   * `dataset_row_count{rid}`                — gauge.
//!   * `dataset_txn_failures_total{rid}`       — counter (24h-window).
//!
//! The 24h failure counter is reset + re-incremented on every
//! `compute_health()` call so the value reflects the current window
//! rather than the lifetime of the process. That trades counter
//! semantics for surface-level fidelity — the dashboard panels show
//! "failures in the last 24h", which lines up with the doc.

use once_cell::sync::Lazy;
use prometheus::{
    IntCounter, IntCounterVec, IntGaugeVec, Opts, register_int_counter, register_int_counter_vec,
    register_int_gauge_vec,
};

pub static DATASET_FRESHNESS_SECONDS: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "dataset_freshness_seconds",
            "Seconds since the most recent committed transaction per dataset"
        ),
        &["rid"],
    )
    .expect("register dataset_freshness_seconds")
});

pub static DATASET_ROW_COUNT: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "dataset_row_count",
            "Most-recent published row count per dataset"
        ),
        &["rid"],
    )
    .expect("register dataset_row_count")
});

pub static DATASET_TXN_FAILURES_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_txn_failures_total",
            "ABORTED dataset transactions in the last 24h, labelled by dataset RID"
        ),
        &["rid"],
    )
    .expect("register dataset_txn_failures_total")
});

pub static DATASET_QUALITY_SERVICE_INFO: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_quality_service_info",
        "Constant 1 once dataset-quality-service has registered metrics"
    )
    .expect("register dataset_quality_service_info")
});

pub fn init() {
    DATASET_QUALITY_SERVICE_INFO.inc();
}
