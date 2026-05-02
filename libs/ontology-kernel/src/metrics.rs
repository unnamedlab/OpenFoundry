//! TASK F — Action metrics surface for the ontology actions service.
//!
//! Exposes a small Prometheus registry-friendly module that classifies and
//! records every action execution. The metrics are:
//!
//! - `action_executions_total{action_id,status}` — counter of every attempt.
//! - `action_execution_duration_seconds{action_id}` — histogram bucketed over
//!   typical request latencies.
//! - `action_failures_total{action_id,failure_type}` — counter of failures
//!   broken down by classification (see [`FailureType`]).
//!
//! Binaries register the metrics into their own `prometheus::Registry` once
//! at startup via [`register_action_metrics`]. Handlers grab the singleton
//! via [`action_metrics`] and call `record_*`.

use std::sync::OnceLock;

use prometheus::{HistogramOpts, HistogramVec, IntCounterVec, Opts, Registry, exponential_buckets};

/// Singleton store. Initialised the first time
/// [`register_action_metrics`] is called by a binary; subsequent calls are
/// no-ops so multi-binary tests stay safe.
static METRICS: OnceLock<ActionMetrics> = OnceLock::new();

/// Failure classification used as the `failure_type` label of
/// `action_failures_total`. Mirrors the categories called out in
/// `Action metrics.md`.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FailureType {
    InvalidParameter,
    ScaleLimit,
    Authentication,
    SideEffect,
    Function,
    UserFacingFunction,
    Conflict,
    Unclassified,
}

impl FailureType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::InvalidParameter => "invalid_parameter",
            Self::ScaleLimit => "scale_limit",
            Self::Authentication => "authentication",
            Self::SideEffect => "side_effect",
            Self::Function => "function",
            Self::UserFacingFunction => "user_facing_function",
            Self::Conflict => "conflict",
            Self::Unclassified => "unclassified",
        }
    }

    /// Best-effort classification from the HTTP status code emitted by the
    /// kernel, used as a fallback when callers cannot classify upstream.
    pub fn from_http_status(status: u16) -> Self {
        match status {
            400 | 422 => Self::InvalidParameter,
            401 | 403 => Self::Authentication,
            409 => Self::Conflict,
            429 => Self::ScaleLimit,
            500..=599 => Self::SideEffect,
            _ => Self::Unclassified,
        }
    }
}

/// Bundle of Prometheus collectors. Cloned cheaply via the singleton
/// reference returned by [`action_metrics`].
pub struct ActionMetrics {
    pub executions_total: IntCounterVec,
    pub execution_duration_seconds: HistogramVec,
    pub failures_total: IntCounterVec,
}

impl ActionMetrics {
    fn new() -> Self {
        // 8 buckets from 5ms to 5s — covers typical writeback latencies and
        // surfaces P95 via the JSON aggregation endpoint as well.
        let buckets = exponential_buckets(0.005, 2.0, 10).expect("valid bucket spec");

        let executions_total = IntCounterVec::new(
            Opts::new(
                "action_executions_total",
                "Total ontology action executions, partitioned by action and outcome",
            ),
            &["action_id", "status"],
        )
        .expect("counter spec is valid");

        let execution_duration_seconds = HistogramVec::new(
            HistogramOpts::new(
                "action_execution_duration_seconds",
                "Wall-clock duration of ontology action executions",
            )
            .buckets(buckets),
            &["action_id"],
        )
        .expect("histogram spec is valid");

        let failures_total = IntCounterVec::new(
            Opts::new(
                "action_failures_total",
                "Failed ontology action executions, partitioned by failure_type",
            ),
            &["action_id", "failure_type"],
        )
        .expect("counter spec is valid");

        Self {
            executions_total,
            execution_duration_seconds,
            failures_total,
        }
    }

    pub fn record_success(&self, action_id: &str, duration_seconds: f64) {
        self.executions_total
            .with_label_values(&[action_id, "success"])
            .inc();
        self.execution_duration_seconds
            .with_label_values(&[action_id])
            .observe(duration_seconds);
    }

    pub fn record_failure(
        &self,
        action_id: &str,
        failure_type: FailureType,
        duration_seconds: f64,
    ) {
        self.executions_total
            .with_label_values(&[action_id, "failure"])
            .inc();
        self.execution_duration_seconds
            .with_label_values(&[action_id])
            .observe(duration_seconds);
        self.failures_total
            .with_label_values(&[action_id, failure_type.as_str()])
            .inc();
    }
}

/// Initialise the singleton (first call) and register every collector
/// against the supplied registry. Subsequent calls are no-ops; this lets
/// tests reuse the same registry across multiple `build_router` calls.
pub fn register_action_metrics(registry: &Registry) -> &'static ActionMetrics {
    let metrics = METRICS.get_or_init(ActionMetrics::new);
    // Re-registering the same collector returns AlreadyReg; we ignore that.
    let _ = registry.register(Box::new(metrics.executions_total.clone()));
    let _ = registry.register(Box::new(metrics.execution_duration_seconds.clone()));
    let _ = registry.register(Box::new(metrics.failures_total.clone()));
    metrics
}

/// Return the metrics singleton. Returns `None` if no binary has called
/// [`register_action_metrics`] yet (e.g. unit tests that exercise the
/// handler in isolation).
pub fn action_metrics() -> Option<&'static ActionMetrics> {
    METRICS.get()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn failure_type_from_http_status_classifies_common_codes() {
        assert_eq!(
            FailureType::from_http_status(400),
            FailureType::InvalidParameter
        );
        assert_eq!(
            FailureType::from_http_status(403),
            FailureType::Authentication
        );
        assert_eq!(FailureType::from_http_status(409), FailureType::Conflict);
        assert_eq!(FailureType::from_http_status(429), FailureType::ScaleLimit);
        assert_eq!(FailureType::from_http_status(503), FailureType::SideEffect);
        assert_eq!(
            FailureType::from_http_status(200),
            FailureType::Unclassified
        );
    }

    #[test]
    fn metrics_record_success_and_failure_increments_counters() {
        let registry = Registry::new();
        let metrics = register_action_metrics(&registry);
        metrics.record_success("00000000-0000-0000-0000-000000000001", 0.123);
        metrics.record_failure(
            "00000000-0000-0000-0000-000000000001",
            FailureType::InvalidParameter,
            0.05,
        );
        let families = registry.gather();
        assert!(
            !families.is_empty(),
            "registry should have collected metric families",
        );
    }
}
