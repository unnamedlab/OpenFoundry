//! Prometheus metrics exposed by `connector-management-service`.
//!
//! Tarea 9 — observabilidad: every connector test_connection / sync_run
//! call increments these counters and observes the duration histogram.
//! The metrics are owned by [`Metrics`] which carries its own
//! [`prometheus::Registry`] so the `/metrics` endpoint is not polluted by
//! unrelated process collectors.
//!
//! Labels:
//! * `connector` — connector kind (`postgresql`, `kafka`, `s3`, …).
//! * `result`    — `success` | `failure`.
//!
//! The counter and histogram names match the spec from Tarea 9:
//! * `connector_test_total{connector,result}`
//! * `connector_sync_rows_total{connector}`
//! * `connector_sync_duration_seconds{connector}`

use std::sync::Arc;

use prometheus::{
    Encoder, HistogramVec, IntCounterVec, Opts, Registry, TextEncoder, histogram_opts,
};

pub mod result {
    pub const SUCCESS: &str = "success";
    pub const FAILURE: &str = "failure";
}

#[derive(Clone)]
pub struct Metrics {
    registry: Arc<Registry>,
    pub connector_test_total: IntCounterVec,
    pub connector_sync_rows_total: IntCounterVec,
    pub connector_sync_duration_seconds: HistogramVec,
}

impl Default for Metrics {
    fn default() -> Self {
        Self::new()
    }
}

impl Metrics {
    pub fn new() -> Self {
        let registry = Arc::new(Registry::new());

        let connector_test_total = IntCounterVec::new(
            Opts::new(
                "connector_test_total",
                "Total number of connector test_connection invocations.",
            ),
            &["connector", "result"],
        )
        .expect("valid metric definition");

        let connector_sync_rows_total = IntCounterVec::new(
            Opts::new(
                "connector_sync_rows_total",
                "Total rows registered through connector syncs (sum of sync_run row counts).",
            ),
            &["connector"],
        )
        .expect("valid metric definition");

        let connector_sync_duration_seconds = HistogramVec::new(
            histogram_opts!(
                "connector_sync_duration_seconds",
                "Wall-clock duration of connector sync_run invocations (seconds).",
                vec![0.005, 0.025, 0.1, 0.5, 1.0, 5.0, 30.0, 120.0, 600.0]
            ),
            &["connector", "result"],
        )
        .expect("valid metric definition");

        registry
            .register(Box::new(connector_test_total.clone()))
            .expect("register connector_test_total");
        registry
            .register(Box::new(connector_sync_rows_total.clone()))
            .expect("register connector_sync_rows_total");
        registry
            .register(Box::new(connector_sync_duration_seconds.clone()))
            .expect("register connector_sync_duration_seconds");

        Self {
            registry,
            connector_test_total,
            connector_sync_rows_total,
            connector_sync_duration_seconds,
        }
    }

    pub fn record_test(&self, connector: &str, success: bool) {
        let label = if success { result::SUCCESS } else { result::FAILURE };
        self.connector_test_total
            .with_label_values(&[connector, label])
            .inc();
    }

    pub fn record_sync(&self, connector: &str, rows: i64, duration_seconds: f64, success: bool) {
        if rows > 0 {
            self.connector_sync_rows_total
                .with_label_values(&[connector])
                .inc_by(rows as u64);
        }
        let label = if success { result::SUCCESS } else { result::FAILURE };
        self.connector_sync_duration_seconds
            .with_label_values(&[connector, label])
            .observe(duration_seconds);
    }

    pub fn render(&self) -> Result<String, prometheus::Error> {
        let mut buf = Vec::new();
        TextEncoder::new().encode(&self.registry.gather(), &mut buf)?;
        Ok(String::from_utf8(buf).unwrap_or_default())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn record_test_emits_per_connector_counter() {
        let metrics = Metrics::new();
        metrics.record_test("postgresql", true);
        metrics.record_test("postgresql", true);
        metrics.record_test("postgresql", false);
        metrics.record_test("kafka", true);

        let exposition = metrics.render().expect("render");
        assert!(exposition.contains("connector_test_total"));
        assert!(exposition.contains("connector=\"postgresql\""));
        assert!(exposition.contains("connector=\"kafka\""));
        assert!(exposition.contains("result=\"success\""));
        assert!(exposition.contains("result=\"failure\""));
    }

    #[test]
    fn record_sync_advances_rows_and_duration_histograms() {
        let metrics = Metrics::new();
        metrics.record_sync("s3", 1500, 0.42, true);
        metrics.record_sync("s3", 0, 1.5, false);

        let exposition = metrics.render().expect("render");
        assert!(exposition.contains("connector_sync_rows_total"));
        assert!(exposition.contains("connector_sync_duration_seconds_bucket"));
        // Only the success path should advance the row counter (rows=0 in the failure case).
        assert!(exposition
            .lines()
            .any(|line| line.starts_with("connector_sync_rows_total{connector=\"s3\"} 1500")));
    }
}
