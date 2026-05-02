//! Prometheus metric registry for the catalog service.
//!
//! All counters live here so the `/metrics` endpoint and the
//! integration smoke test can rely on a stable, `dataset_`-prefixed
//! surface.

use once_cell::sync::Lazy;
use prometheus::{IntCounter, IntCounterVec, Opts, register_int_counter, register_int_counter_vec};

/// Total number of dataset CRUD operations served. Bumped from
/// handlers when they reach a terminal status.
pub static DATASET_REQUESTS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_requests_total",
            "Catalog service requests served, labelled by op",
        ),
        &["op"],
    )
    .expect("register dataset_requests_total")
});

/// Service-wide bootstrap counter; ensures the `dataset_` family is
/// always present in `/metrics` even before any HTTP traffic.
pub static DATASET_SERVICE_INFO: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_catalog_service_info",
        "Constant 1 once the catalog service has registered metrics",
    )
    .expect("register dataset_catalog_service_info")
});

/// Touch the lazy statics so that calling `prometheus::gather()`
/// returns the `dataset_*` metric families immediately.
pub fn init() {
    DATASET_SERVICE_INFO.inc();
    let _ = DATASET_REQUESTS_TOTAL.with_label_values(&["bootstrap"]);
}
