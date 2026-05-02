//! Prometheus metric registry for the dataset-versioning service.
//!
//! Provides a stable `dataset_*` family that the smoke test asserts
//! against on `GET /metrics`.

use once_cell::sync::Lazy;
use prometheus::{IntCounter, IntCounterVec, Opts, register_int_counter, register_int_counter_vec};

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

pub fn init() {
    DATASET_VERSIONING_INFO.inc();
    let _ = DATASET_TX_TOTAL.with_label_values(&["bootstrap"]);
}
