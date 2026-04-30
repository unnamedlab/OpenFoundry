//! Prometheus metrics exposed by the routing facade.
//!
//! All metrics are owned by a [`Metrics`] struct that holds a private
//! [`prometheus::Registry`]. A single instance is created at startup and shared
//! across the gRPC service and the backends; tests build their own instance to
//! get isolated counters.

use std::sync::Arc;

use prometheus::{
    Encoder, HistogramVec, IntCounterVec, IntGaugeVec, Opts, Registry, TextEncoder,
    histogram_opts,
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

        registry.register(Box::new(events_published_total.clone())).unwrap();
        registry.register(Box::new(events_received_total.clone())).unwrap();
        registry.register(Box::new(publish_latency_seconds.clone())).unwrap();
        registry.register(Box::new(active_subscriptions.clone())).unwrap();
        registry.register(Box::new(route_resolution_total.clone())).unwrap();

        Self {
            registry,
            events_published_total,
            events_received_total,
            publish_latency_seconds,
            active_subscriptions,
            route_resolution_total,
        }
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
        self.route_resolution_total.with_label_values(&[result]).inc();
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
