//! Prometheus metrics exposed by the routing facade.
//!
//! All metrics are owned by a [`Metrics`] struct that holds a private
//! [`prometheus::Registry`]. A single instance is created at startup and shared
//! across the gRPC service and the backends; tests build their own instance to
//! get isolated counters.

use std::sync::Arc;

use prometheus::{
    CounterVec, Encoder, GaugeVec, HistogramVec, IntCounterVec, IntGaugeVec, Opts, Registry,
    TextEncoder, histogram_opts,
};

use crate::router::BackendId;

/// Stable strings used for the `result` label on `events_published_total`.
pub mod publish_result {
    pub const OK: &str = "ok";
    pub const NO_ROUTE: &str = "no_route";
    pub const BACKEND_ERROR: &str = "backend_error";
    pub const BACKEND_UNAVAILABLE: &str = "backend_unavailable";
}

/// Stable strings used for the `result` label on `route_resolution_total`.
pub mod resolution_result {
    pub const MATCHED: &str = "matched";
    pub const DEFAULT: &str = "default";
    pub const MISSED: &str = "missed";
}

/// Bundle of Prometheus collectors used by the router.
#[derive(Clone)]
pub struct Metrics {
    registry: Arc<Registry>,
    pub events_published_total: IntCounterVec,
    pub events_received_total: IntCounterVec,
    pub publish_latency_seconds: HistogramVec,
    pub active_subscriptions: IntGaugeVec,
    pub route_resolution_total: IntCounterVec,
    // Bloque F1 - per-stream / per-topology observability.
    pub stream_rows_in_total: IntCounterVec,
    pub stream_rows_archived_total: IntCounterVec,
    pub stream_lag_ms: IntGaugeVec,
    pub topology_checkpoint_duration_seconds: HistogramVec,
    pub topology_checkpoint_size_bytes: IntGaugeVec,
    pub topology_backpressure_ratio: GaugeVec,
    pub topology_restarts_total: IntCounterVec,
    pub dead_letter_total: IntCounterVec,
    // Bloque P4 — stream-monitoring counters/gauges. The list is
    // closed; new monitor kinds either reuse one of these or live in
    // the in-process metrics endpoint summary.
    pub records_ingested_total: IntCounterVec,
    pub records_output_total: IntCounterVec,
    pub utilization_pct: GaugeVec,
    /// Bloque P6 — billable compute seconds per stream / topology.
    /// Increments on each checkpoint commit and is also persisted in
    /// `stream_compute_usage` so the `/usage` endpoint can surface a
    /// historical view independent of the metrics retention window.
    pub compute_seconds_total: CounterVec,
}

impl Metrics {
    pub fn new() -> Self {
        let registry = Arc::new(Registry::new());

        let events_published_total = IntCounterVec::new(
            Opts::new(
                "event_router_events_published_total",
                "Total number of events accepted by the router for publication.",
            ),
            &["backend", "topic_pattern", "result"],
        )
        .expect("valid metric definition");

        let events_received_total = IntCounterVec::new(
            Opts::new(
                "event_router_events_received_total",
                "Total number of events forwarded to subscribers.",
            ),
            &["backend", "topic_pattern"],
        )
        .expect("valid metric definition");

        let publish_latency_seconds = HistogramVec::new(
            histogram_opts!(
                "event_router_publish_latency_seconds",
                "End-to-end latency of router Publish calls (seconds).",
                vec![0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0]
            ),
            &["backend"],
        )
        .expect("valid metric definition");

        let active_subscriptions = IntGaugeVec::new(
            Opts::new(
                "event_router_active_subscriptions",
                "Number of currently active gRPC subscriptions per backend.",
            ),
            &["backend"],
        )
        .expect("valid metric definition");

        let route_resolution_total = IntCounterVec::new(
            Opts::new(
                "event_router_route_resolution_total",
                "Outcome of resolving a topic against the routing table.",
            ),
            &["result"],
        )
        .expect("valid metric definition");

        // Bloque F1 - per-stream and per-topology metrics.
        let stream_rows_in_total = IntCounterVec::new(
            Opts::new(
                "streaming_stream_rows_in_total",
                "Total number of rows accepted into a stream (after schema validation).",
            ),
            &["stream"],
        )
        .expect("valid metric definition");

        let stream_rows_archived_total = IntCounterVec::new(
            Opts::new(
                "streaming_stream_rows_archived_total",
                "Total number of stream rows committed to the cold tier (Iceberg).",
            ),
            &["stream"],
        )
        .expect("valid metric definition");

        let stream_lag_ms = IntGaugeVec::new(
            Opts::new(
                "streaming_stream_lag_ms",
                "Approximate consumer lag of a stream partition, in milliseconds.",
            ),
            &["stream", "partition"],
        )
        .expect("valid metric definition");

        let topology_checkpoint_duration_seconds = HistogramVec::new(
            histogram_opts!(
                "streaming_topology_checkpoint_duration_seconds",
                "Wall-clock duration of a topology checkpoint, in seconds.",
                vec![0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0]
            ),
            &["topology"],
        )
        .expect("valid metric definition");

        let topology_checkpoint_size_bytes = IntGaugeVec::new(
            Opts::new(
                "streaming_topology_checkpoint_size_bytes",
                "Size in bytes of the latest topology checkpoint.",
            ),
            &["topology"],
        )
        .expect("valid metric definition");

        let topology_backpressure_ratio = GaugeVec::new(
            Opts::new(
                "streaming_topology_backpressure_ratio",
                "Ratio in [0,1] of operators currently signalling backpressure.",
            ),
            &["topology"],
        )
        .expect("valid metric definition");

        let topology_restarts_total = IntCounterVec::new(
            Opts::new(
                "streaming_topology_restarts_total",
                "Total number of topology restarts.",
            ),
            &["topology", "reason"],
        )
        .expect("valid metric definition");

        let dead_letter_total = IntCounterVec::new(
            Opts::new(
                "streaming_dead_letter_total",
                "Total number of events sent to the dead-letter queue.",
            ),
            &["stream", "reason"],
        )
        .expect("valid metric definition");

        // Bloque P4 — stream monitoring metrics.
        let records_ingested_total = IntCounterVec::new(
            Opts::new(
                "streaming_records_ingested_total",
                "Total records that landed in a stream's live view.",
            ),
            &["stream"],
        )
        .expect("valid metric definition");

        let records_output_total = IntCounterVec::new(
            Opts::new(
                "streaming_records_output_total",
                "Total records emitted by a streaming pipeline (per topology).",
            ),
            &["topology"],
        )
        .expect("valid metric definition");

        let utilization_pct = GaugeVec::new(
            Opts::new(
                "streaming_utilization_pct",
                "Streaming pipeline utilization (0..1) of the configured profile capacity.",
            ),
            &["topology"],
        )
        .expect("valid metric definition");

        let compute_seconds_total = CounterVec::new(
            Opts::new(
                "streaming_compute_seconds_total",
                "Foundry-parity billable compute seconds per stream / topology.",
            ),
            &["stream", "topology"],
        )
        .expect("valid metric definition");

        registry
            .register(Box::new(events_published_total.clone()))
            .unwrap();
        registry
            .register(Box::new(events_received_total.clone()))
            .unwrap();
        registry
            .register(Box::new(publish_latency_seconds.clone()))
            .unwrap();
        registry
            .register(Box::new(active_subscriptions.clone()))
            .unwrap();
        registry
            .register(Box::new(route_resolution_total.clone()))
            .unwrap();
        registry
            .register(Box::new(stream_rows_in_total.clone()))
            .unwrap();
        registry
            .register(Box::new(stream_rows_archived_total.clone()))
            .unwrap();
        registry.register(Box::new(stream_lag_ms.clone())).unwrap();
        registry
            .register(Box::new(topology_checkpoint_duration_seconds.clone()))
            .unwrap();
        registry
            .register(Box::new(topology_checkpoint_size_bytes.clone()))
            .unwrap();
        registry
            .register(Box::new(topology_backpressure_ratio.clone()))
            .unwrap();
        registry
            .register(Box::new(topology_restarts_total.clone()))
            .unwrap();
        registry
            .register(Box::new(dead_letter_total.clone()))
            .unwrap();
        registry
            .register(Box::new(records_ingested_total.clone()))
            .unwrap();
        registry
            .register(Box::new(records_output_total.clone()))
            .unwrap();
        registry
            .register(Box::new(utilization_pct.clone()))
            .unwrap();
        registry
            .register(Box::new(compute_seconds_total.clone()))
            .unwrap();

        Self {
            registry,
            events_published_total,
            events_received_total,
            publish_latency_seconds,
            active_subscriptions,
            route_resolution_total,
            stream_rows_in_total,
            stream_rows_archived_total,
            stream_lag_ms,
            topology_checkpoint_duration_seconds,
            topology_checkpoint_size_bytes,
            topology_backpressure_ratio,
            topology_restarts_total,
            dead_letter_total,
            records_ingested_total,
            records_output_total,
            utilization_pct,
            compute_seconds_total,
        }
    }

    /// Bloque P6 — record billable compute seconds for a checkpoint
    /// commit. The same value is also persisted in
    /// `stream_compute_usage` by the engine hook so the Usage tab
    /// can serve a historical view independent of Prometheus
    /// retention.
    pub fn record_compute_seconds(&self, stream: &str, topology: &str, seconds: f64) {
        self.compute_seconds_total
            .with_label_values(&[stream, topology])
            .inc_by(seconds.max(0.0));
    }

    /// Convenience: increment `streaming_records_ingested_total`.
    pub fn record_ingest(&self, stream: &str, n: u64) {
        self.records_ingested_total
            .with_label_values(&[stream])
            .inc_by(n);
    }

    /// Convenience: increment `streaming_records_output_total`.
    pub fn record_output(&self, topology: &str, n: u64) {
        self.records_output_total
            .with_label_values(&[topology])
            .inc_by(n);
    }

    /// Convenience: set `streaming_utilization_pct` for a topology
    /// (value clamped to [0, 1]).
    pub fn set_utilization(&self, topology: &str, ratio: f64) {
        self.utilization_pct
            .with_label_values(&[topology])
            .set(ratio.clamp(0.0, 1.0));
    }

    /// Convenience: increment `streaming_stream_rows_in_total`.
    pub fn record_stream_rows_in(&self, stream: &str, n: u64) {
        self.stream_rows_in_total
            .with_label_values(&[stream])
            .inc_by(n);
    }

    /// Convenience: increment `streaming_stream_rows_archived_total`.
    pub fn record_stream_rows_archived(&self, stream: &str, n: u64) {
        self.stream_rows_archived_total
            .with_label_values(&[stream])
            .inc_by(n);
    }

    /// Convenience: increment `streaming_dead_letter_total`.
    pub fn record_dead_letter(&self, stream: &str, reason: &str) {
        self.dead_letter_total
            .with_label_values(&[stream, reason])
            .inc();
    }

    /// Convenience: observe a checkpoint duration + size.
    pub fn record_checkpoint(&self, topology: &str, duration_seconds: f64, size_bytes: i64) {
        self.topology_checkpoint_duration_seconds
            .with_label_values(&[topology])
            .observe(duration_seconds);
        self.topology_checkpoint_size_bytes
            .with_label_values(&[topology])
            .set(size_bytes);
    }

    /// Convenience: set the backpressure ratio for a topology.
    pub fn set_backpressure_ratio(&self, topology: &str, ratio: f64) {
        self.topology_backpressure_ratio
            .with_label_values(&[topology])
            .set(ratio);
    }

    /// Convenience: increment topology restart counter.
    pub fn record_topology_restart(&self, topology: &str, reason: &str) {
        self.topology_restarts_total
            .with_label_values(&[topology, reason])
            .inc();
    }

    /// Convenience: report stream lag for a partition.
    pub fn set_stream_lag_ms(&self, stream: &str, partition: &str, lag_ms: i64) {
        self.stream_lag_ms
            .with_label_values(&[stream, partition])
            .set(lag_ms);
    }

    pub fn registry(&self) -> &Registry {
        &self.registry
    }

    /// Render the registry to the standard Prometheus text exposition format.
    pub fn render(&self) -> Result<String, prometheus::Error> {
        let mut buf = Vec::new();
        let encoder = TextEncoder::new();
        encoder.encode(&self.registry.gather(), &mut buf)?;
        Ok(String::from_utf8(buf).unwrap_or_default())
    }

    /// Convenience: increment `events_published_total` with the given outcome.
    pub fn record_publish(&self, backend: BackendId, pattern: &str, result: &'static str) {
        self.events_published_total
            .with_label_values(&[backend.as_str(), pattern, result])
            .inc();
    }

    /// Convenience: record a route-resolution outcome.
    pub fn record_resolution(&self, result: &'static str) {
        self.route_resolution_total
            .with_label_values(&[result])
            .inc();
    }
}

impl Default for Metrics {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metrics_render_is_valid_prometheus_text() {
        let m = Metrics::new();
        m.record_publish(BackendId::Nats, "ctrl.*", publish_result::OK);
        m.record_resolution(resolution_result::MATCHED);
        let out = m.render().expect("render ok");
        assert!(out.contains("event_router_events_published_total"));
        assert!(out.contains("backend=\"nats\""));
        assert!(out.contains("event_router_route_resolution_total"));
    }

    #[test]
    fn registries_are_isolated_per_instance() {
        // If they shared a global registry, creating two would panic with
        // "duplicate metrics collector registration attempted".
        let _a = Metrics::new();
        let _b = Metrics::new();
    }
}
