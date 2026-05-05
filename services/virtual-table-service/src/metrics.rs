//! Prometheus metrics exposed at `GET /metrics`.
//!
//! Counter / gauge / histogram names match the spec from D1.1.9 P1 §
//! "Métricas Prometheus":
//!   * `virtual_tables_total{provider, project}`              (gauge)
//!   * `virtual_tables_registered_total{provider, kind}`      (counter)
//!   * `virtual_table_discovery_duration_seconds{provider}`   (histogram)
//!   * `virtual_table_schema_inference_failures_total{provider}` (counter)

use std::sync::Arc;

use prometheus::{
    Encoder, HistogramVec, IntCounterVec, IntGaugeVec, Opts, Registry, TextEncoder, histogram_opts,
};

#[derive(Clone)]
pub struct Metrics {
    registry: Arc<Registry>,
    pub virtual_tables_total: IntGaugeVec,
    pub virtual_tables_registered_total: IntCounterVec,
    pub virtual_table_discovery_duration_seconds: HistogramVec,
    pub virtual_table_schema_inference_failures_total: IntCounterVec,
    /// Counter incremented every time
    /// `domain::source_validation::validate_for_virtual_tables` rejects
    /// a registration call (one of the five doc § "Limitations" rules).
    pub virtual_table_source_validation_failures_total: IntCounterVec,
    /// P4 — auto-registration scanner runs, broken down by status
    /// (`succeeded` | `failed`). Doc § "Auto-registration".
    pub virtual_table_auto_register_runs_total: IntCounterVec,
    /// P4 — wall-clock duration of one scanner tick.
    pub virtual_table_auto_register_duration_seconds: HistogramVec,
}

impl Default for Metrics {
    fn default() -> Self {
        Self::new()
    }
}

impl Metrics {
    pub fn new() -> Self {
        let registry = Arc::new(Registry::new());

        let virtual_tables_total = IntGaugeVec::new(
            Opts::new(
                "virtual_tables_total",
                "Number of virtual tables currently registered.",
            ),
            &["provider", "project"],
        )
        .expect("valid gauge");

        let virtual_tables_registered_total = IntCounterVec::new(
            Opts::new(
                "virtual_tables_registered_total",
                "Total virtual table registrations, broken down by registration kind.",
            ),
            &["provider", "kind"],
        )
        .expect("valid counter");

        let virtual_table_discovery_duration_seconds = HistogramVec::new(
            histogram_opts!(
                "virtual_table_discovery_duration_seconds",
                "Duration of remote-catalog discovery calls (seconds).",
                vec![0.005, 0.025, 0.1, 0.5, 1.0, 5.0, 30.0]
            ),
            &["provider"],
        )
        .expect("valid histogram");

        let virtual_table_schema_inference_failures_total = IntCounterVec::new(
            Opts::new(
                "virtual_table_schema_inference_failures_total",
                "Number of schema-inference attempts that returned no columns.",
            ),
            &["provider"],
        )
        .expect("valid counter");

        registry
            .register(Box::new(virtual_tables_total.clone()))
            .expect("register virtual_tables_total");
        registry
            .register(Box::new(virtual_tables_registered_total.clone()))
            .expect("register virtual_tables_registered_total");
        registry
            .register(Box::new(
                virtual_table_discovery_duration_seconds.clone(),
            ))
            .expect("register virtual_table_discovery_duration_seconds");
        registry
            .register(Box::new(
                virtual_table_schema_inference_failures_total.clone(),
            ))
            .expect("register virtual_table_schema_inference_failures_total");

        let virtual_table_source_validation_failures_total = IntCounterVec::new(
            Opts::new(
                "virtual_table_source_validation_failures_total",
                "Number of registration attempts rejected by Foundry-worker / egress enforcement.",
            ),
            &["reason"],
        )
        .expect("valid counter");
        registry
            .register(Box::new(
                virtual_table_source_validation_failures_total.clone(),
            ))
            .expect("register virtual_table_source_validation_failures_total");

        let virtual_table_auto_register_runs_total = IntCounterVec::new(
            Opts::new(
                "virtual_table_auto_register_runs_total",
                "Number of auto-registration scanner runs, by status.",
            ),
            &["source", "status"],
        )
        .expect("valid counter");
        registry
            .register(Box::new(virtual_table_auto_register_runs_total.clone()))
            .expect("register virtual_table_auto_register_runs_total");

        let virtual_table_auto_register_duration_seconds = HistogramVec::new(
            histogram_opts!(
                "virtual_table_auto_register_duration_seconds",
                "Wall-clock duration of one auto-registration scanner tick (seconds).",
                vec![0.05, 0.25, 1.0, 5.0, 30.0, 120.0, 600.0]
            ),
            &["source"],
        )
        .expect("valid histogram");
        registry
            .register(Box::new(
                virtual_table_auto_register_duration_seconds.clone(),
            ))
            .expect("register virtual_table_auto_register_duration_seconds");

        Self {
            registry,
            virtual_tables_total,
            virtual_tables_registered_total,
            virtual_table_discovery_duration_seconds,
            virtual_table_schema_inference_failures_total,
            virtual_table_source_validation_failures_total,
            virtual_table_auto_register_runs_total,
            virtual_table_auto_register_duration_seconds,
        }
    }

    pub fn record_auto_register_run(&self, source: &str, status: &str) {
        self.virtual_table_auto_register_runs_total
            .with_label_values(&[source, status])
            .inc();
    }

    pub fn observe_auto_register_duration(&self, source: &str, duration_seconds: f64) {
        self.virtual_table_auto_register_duration_seconds
            .with_label_values(&[source])
            .observe(duration_seconds);
    }

    pub fn record_source_validation_failure(&self, reason: &str) {
        self.virtual_table_source_validation_failures_total
            .with_label_values(&[reason])
            .inc();
    }

    pub fn record_registration(&self, provider: &str, kind: &str) {
        self.virtual_tables_registered_total
            .with_label_values(&[provider, kind])
            .inc();
    }

    pub fn observe_table_count(&self, provider: &str, project: &str) {
        self.virtual_tables_total
            .with_label_values(&[provider, project])
            .inc();
    }

    pub fn record_discovery(&self, provider: &str, duration_seconds: f64) {
        self.virtual_table_discovery_duration_seconds
            .with_label_values(&[provider])
            .observe(duration_seconds);
    }

    pub fn record_schema_inference_failure(&self, provider: &str) {
        self.virtual_table_schema_inference_failures_total
            .with_label_values(&[provider])
            .inc();
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
    fn registration_counter_emits_per_provider_kind() {
        let metrics = Metrics::new();
        metrics.record_registration("BIGQUERY", "manual");
        metrics.record_registration("BIGQUERY", "bulk");
        metrics.record_registration("SNOWFLAKE", "auto");

        let exposition = metrics.render().expect("render");
        assert!(exposition.contains("virtual_tables_registered_total"));
        assert!(exposition.contains("provider=\"BIGQUERY\""));
        assert!(exposition.contains("kind=\"manual\""));
        assert!(exposition.contains("kind=\"auto\""));
    }
}
