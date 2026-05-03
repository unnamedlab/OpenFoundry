//! Prometheus metric families exported by the service.
//!
//! Pre-registered at boot so `/metrics` always surfaces the prefix even
//! before the first request lands.
//!
//! ## Foundry cost-model alignment
//!
//! The four `media_*` families below mirror the per-domain metrics
//! Foundry uses in *Media usage costs and limits* (compute seconds per
//! transformation, live-storage bytes per schema, in-flight transactions,
//! retention purges). They are the inputs operators feed into the
//! tenant cost model. Keep names and label sets stable — Grafana
//! dashboards and the Prometheus alert rules in
//! [`infra/k8s/platform/observability/prometheus-rules/media-sets.yaml`]
//! depend on them.

use once_cell::sync::Lazy;
use prometheus::{
    IntCounter, IntCounterVec, IntGauge, IntGaugeVec, register_int_counter,
    register_int_counter_vec, register_int_gauge, register_int_gauge_vec,
};

pub static MEDIA_SET_UPLOADS_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "media_set_uploads_total",
        "Total number of media items registered for upload (presigned URL issued)."
    )
    .expect("register media_set_uploads_total")
});

pub static MEDIA_SET_DOWNLOADS_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "media_set_downloads_total",
        "Total number of presigned download URLs issued."
    )
    .expect("register media_set_downloads_total")
});

pub static MEDIA_SET_STORAGE_BYTES: Lazy<IntGauge> = Lazy::new(|| {
    register_int_gauge!(
        "media_set_storage_bytes",
        "Approximate total bytes of media items currently live (sum of size_bytes \
         where deleted_at IS NULL)."
    )
    .expect("register media_set_storage_bytes")
});

/// Per-`(transformation, schema)` count of compute seconds consumed by
/// access-pattern work (image transform, OCR, transcription, …). Base
/// counter for the per-tenant cost model derived from Foundry's
/// "Media usage costs and limits". Incremented as access-pattern
/// handlers land — kept registered today so dashboards do not
/// disappear when the surface ships.
///
/// Labels:
/// * `transformation` — short identifier of the access pattern
///   (`image_transform`, `ocr`, `transcription`, …).
/// * `schema` — media-set schema (`IMAGE`, `AUDIO`, `VIDEO`, …).
pub static MEDIA_COMPUTE_SECONDS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        "media_compute_seconds_total",
        "Compute seconds consumed by media access-pattern work, per transformation and schema. \
         Base counter for the tenant cost model.",
        &["transformation", "schema"]
    )
    .expect("register media_compute_seconds_total")
});

/// Per-`(schema, virtual)` live storage footprint. Foundry's cost
/// model bills virtual sets at 0 because Foundry never holds the bytes
/// — surfaced separately so the dashboard can split the chargeable
/// vs reference storage.
pub static MEDIA_STORAGE_BYTES_LABELLED: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        "media_storage_bytes",
        "Live media bytes by media-set schema and virtual flag. \
         Sum of size_bytes where deleted_at IS NULL.",
        &["schema", "virtual"]
    )
    .expect("register media_storage_bytes")
});

/// Open transactions per media set. Surface for the operator to spot
/// stuck writers (transactions that never commit / abort), and for the
/// alert rule to page on a sustained backlog.
pub static MEDIA_ACTIVE_TRANSACTIONS: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        "media_active_transactions",
        "Currently OPEN media-set transactions, by media set RID.",
        &["media_set"]
    )
    .expect("register media_active_transactions")
});

/// Number of items soft-deleted by the retention reaper. Anomalous
/// spikes are the canonical signal for "retention policy reduced
/// accidentally" — wired to the `MediaSetsRetentionPurgeAnomaly`
/// alert in `media-sets.yaml`.
pub static MEDIA_RETENTION_PURGES_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "media_retention_purges_total",
        "Total number of media items soft-deleted by the retention reaper."
    )
    .expect("register media_retention_purges_total")
});

/// Force evaluation of every metric family so `/metrics` lists them
/// before the first request that would otherwise lazily register them.
pub fn init() {
    let _ = &*MEDIA_SET_UPLOADS_TOTAL;
    let _ = &*MEDIA_SET_DOWNLOADS_TOTAL;
    let _ = &*MEDIA_SET_STORAGE_BYTES;
    let _ = &*MEDIA_COMPUTE_SECONDS_TOTAL;
    let _ = &*MEDIA_STORAGE_BYTES_LABELLED;
    let _ = &*MEDIA_ACTIVE_TRANSACTIONS;
    let _ = &*MEDIA_RETENTION_PURGES_TOTAL;
}
