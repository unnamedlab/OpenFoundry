//! Prometheus registry for `retention-policy-service`.
//!
//! P4 — covers policy CRUD activity and the synthetic preview path so
//! dashboards can spot configuration churn or an over-eager runner.
//! Mirrors the `dataset_*` family in `dataset-versioning-service` so
//! both services share the same `*_total` / `*_seconds` shape.

use once_cell::sync::Lazy;
use prometheus::{IntCounter, IntCounterVec, Opts, register_int_counter, register_int_counter_vec};

/// Sentinel counter touched at boot so `/metrics` always returns the
/// `retention_*` prefix even before the first request.
pub static RETENTION_SERVICE_INFO: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "retention_service_info",
        "Constant 1 once retention-policy-service has registered metrics"
    )
    .expect("register retention_service_info")
});

/// Total `applicable-policies` resolutions, labelled by inheritance
/// outcome (`hit_org` | `hit_space` | `hit_project` | `hit_dataset` |
/// `none`). The UI uses this to surface whether a dataset is governed
/// by an inherited policy or solely by an explicit one.
pub static RETENTION_APPLICABLE_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "retention_applicable_resolutions_total",
            "applicable-policies resolutions, labelled by inheritance origin"
        ),
        &["origin"],
    )
    .expect("register retention_applicable_resolutions_total")
});

/// Total preview calls, labelled by `as_of_days` bucket
/// (`now` | `lt_30` | `lt_90` | `gt_90`). Lets ops see whether the UI
/// is mostly used to look at "today's" purge or for forecast scenarios.
pub static RETENTION_PREVIEW_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "retention_preview_total",
            "retention-preview invocations, bucketed by as_of_days horizon"
        ),
        &["bucket"],
    )
    .expect("register retention_preview_total")
});

pub fn init() {
    RETENTION_SERVICE_INFO.inc();
    for origin in ["hit_org", "hit_space", "hit_project", "hit_dataset", "none"] {
        let _ = RETENTION_APPLICABLE_TOTAL.with_label_values(&[origin]);
    }
    for bucket in ["now", "lt_30", "lt_90", "gt_90"] {
        let _ = RETENTION_PREVIEW_TOTAL.with_label_values(&[bucket]);
    }
}

/// `as_of_days` → bucket label. Keeps preview cardinality bounded.
pub fn preview_bucket(days: i64) -> &'static str {
    if days <= 0 {
        "now"
    } else if days < 30 {
        "lt_30"
    } else if days < 90 {
        "lt_90"
    } else {
        "gt_90"
    }
}
