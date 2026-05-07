//! Prometheus metric registry for the catalog service.
//!
//! T8.1 — Foundry-aligned `dataset_*` family covering:
//!
//!   * `dataset_marking_enforcement_denials_total` — counter bumped
//!     every time a request is denied because the caller does not
//!     hold the necessary marking clearance.
//!   * `dataset_retention_files_deleted_total` — counter incremented
//!     by retention sweeps (today driven by `lineage-deletion-service`
//!     reporting back via the `/v1/datasets/{rid}/retention/deleted`
//!     internal hook; the counter is registered here because the
//!     catalog is the canonical owner of the dataset surface).
//!   * `dataset_retention_bytes_freed_total` — counter, paired with
//!     the deleted-files counter.
//!   * `dataset_requests_total{op}` — pre-existing generic counter
//!     for HTTP ops (kept for backward compatibility).
//!   * `dataset_catalog_service_info` — bootstrap heartbeat ensuring
//!     the `dataset_` family is always present in `/metrics`.

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

/// Times a request was denied because the caller's clearances did not
/// satisfy the dataset's effective markings.
pub static DATASET_MARKING_ENFORCEMENT_DENIALS_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_marking_enforcement_denials_total",
        "Total request denials caused by marking enforcement",
    )
    .expect("register dataset_marking_enforcement_denials_total")
});

/// Files removed by a retention policy sweep.
pub static DATASET_RETENTION_FILES_DELETED_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_retention_files_deleted_total",
        "Total physical files removed by dataset retention sweeps",
    )
    .expect("register dataset_retention_files_deleted_total")
});

/// Bytes reclaimed by retention sweeps. Paired with
/// [`DATASET_RETENTION_FILES_DELETED_TOTAL`].
pub static DATASET_RETENTION_BYTES_FREED_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_retention_bytes_freed_total",
        "Total bytes freed by dataset retention sweeps",
    )
    .expect("register dataset_retention_bytes_freed_total")
});

/// Touch the lazy statics so that calling `prometheus::gather()`
/// returns the `dataset_*` metric families immediately.
pub fn init() {
    DATASET_SERVICE_INFO.inc();
    let _ = DATASET_REQUESTS_TOTAL.with_label_values(&["bootstrap"]);
    let _ = DATASET_MARKING_ENFORCEMENT_DENIALS_TOTAL.get();
    let _ = DATASET_RETENTION_FILES_DELETED_TOTAL.get();
    let _ = DATASET_RETENTION_BYTES_FREED_TOTAL.get();
}
