//! Prometheus metrics for the schedule plane (P5).
//!
//! Six instruments cover the doc-mandated views:
//!
//! | Metric                                          | Kind       | Labels                  |
//! |-------------------------------------------------|------------|-------------------------|
//! | `schedule_runs_total`                           | Counter    | `outcome`               |
//! | `schedule_runs_duration_seconds`                | Histogram  | —                       |
//! | `schedule_paused_total`                         | Counter    | `reason`                |
//! | `schedule_trigger_evaluation_duration_seconds`  | Histogram  | —                       |
//! | `schedules_active_count`                        | GaugeVec   | `scope_kind`            |
//! | `schedule_event_observations`                   | Gauge      | —                       |
//!
//! Counters / histograms are lazily registered on the global
//! prometheus registry via `lazy_static`; the `register_*_with_registry`
//! variant is also exposed so test harnesses can isolate state.

use prometheus::{
    Gauge, GaugeVec, Histogram, HistogramOpts, IntCounter, IntCounterVec, IntGaugeVec, Opts,
    Registry, register_gauge, register_gauge_vec, register_histogram, register_int_counter,
    register_int_counter_vec, register_int_gauge_vec,
};
use std::sync::OnceLock;

const LATENCY_BUCKETS: &[f64] = &[
    0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0,
];

const TRIGGER_EVAL_BUCKETS: &[f64] = &[
    0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0,
];

pub struct ScheduleMetrics {
    pub runs_total: IntCounterVec,
    pub runs_duration: Histogram,
    pub paused_total: IntCounterVec,
    pub trigger_eval_duration: Histogram,
    pub active_count: IntGaugeVec,
    pub event_observations: Gauge,
    pub aip_generate_total: IntCounter,
    pub aip_low_confidence_total: IntCounter,
}

static GLOBAL: OnceLock<ScheduleMetrics> = OnceLock::new();

/// Build / fetch the singleton metrics registered against the
/// process-wide default registry. Idempotent; safe to call from
/// every handler.
pub fn global() -> &'static ScheduleMetrics {
    GLOBAL.get_or_init(|| build_metrics_default().expect("metric registration"))
}

fn build_metrics_default() -> Result<ScheduleMetrics, prometheus::Error> {
    Ok(ScheduleMetrics {
        runs_total: register_int_counter_vec!(
            Opts::new(
                "schedule_runs_total",
                "Schedule run dispatch outcomes per Foundry doc § Schedules \
                 (Succeeded / Ignored / Failed)."
            ),
            &["outcome"],
        )?,
        runs_duration: register_histogram!(HistogramOpts::new(
            "schedule_runs_duration_seconds",
            "Wall-clock latency from dispatch start to schedule_runs row finalised."
        )
        .buckets(LATENCY_BUCKETS.to_vec()))?,
        paused_total: register_int_counter_vec!(
            Opts::new(
                "schedule_paused_total",
                "Pause transitions, broken down by reason (manual, auto, exempt)."
            ),
            &["reason"],
        )?,
        trigger_eval_duration: register_histogram!(HistogramOpts::new(
            "schedule_trigger_evaluation_duration_seconds",
            "Trigger engine evaluation latency per cron / event tick."
        )
        .buckets(TRIGGER_EVAL_BUCKETS.to_vec()))?,
        active_count: register_int_gauge_vec!(
            Opts::new(
                "schedules_active_count",
                "Number of non-paused schedules per scope_kind."
            ),
            &["scope_kind"],
        )?,
        event_observations: register_gauge!(Opts::new(
            "schedule_event_observations",
            "Live count of rows in schedule_event_observations \
             (event leaves still satisfied but not yet flushed)."
        ))?,
        aip_generate_total: register_int_counter!(Opts::new(
            "schedule_aip_generate_total",
            "Number of POST /v1/schedules/aip:generate calls."
        ))?,
        aip_low_confidence_total: register_int_counter!(Opts::new(
            "schedule_aip_low_confidence_total",
            "AIP outputs rejected because confidence fell below the floor."
        ))?,
    })
}

/// Test-friendly variant: register against an isolated [`Registry`].
/// Avoids duplicate-metric panics when multiple tests touch the same
/// label set in parallel.
pub fn build_metrics_with_registry(registry: &Registry) -> Result<ScheduleMetrics, prometheus::Error> {
    let runs_total = IntCounterVec::new(
        Opts::new("schedule_runs_total", "outcomes counter"),
        &["outcome"],
    )?;
    registry.register(Box::new(runs_total.clone()))?;
    let runs_duration =
        Histogram::with_opts(HistogramOpts::new("schedule_runs_duration_seconds", "duration"))?;
    registry.register(Box::new(runs_duration.clone()))?;
    let paused_total = IntCounterVec::new(
        Opts::new("schedule_paused_total", "pause counter"),
        &["reason"],
    )?;
    registry.register(Box::new(paused_total.clone()))?;
    let trigger_eval_duration = Histogram::with_opts(HistogramOpts::new(
        "schedule_trigger_evaluation_duration_seconds",
        "trigger eval duration",
    ))?;
    registry.register(Box::new(trigger_eval_duration.clone()))?;
    let active_count = IntGaugeVec::new(
        Opts::new("schedules_active_count", "active count gauge"),
        &["scope_kind"],
    )?;
    registry.register(Box::new(active_count.clone()))?;
    let event_observations =
        Gauge::with_opts(Opts::new("schedule_event_observations", "obs gauge"))?;
    registry.register(Box::new(event_observations.clone()))?;
    let aip_generate_total =
        IntCounter::with_opts(Opts::new("schedule_aip_generate_total", "aip generate"))?;
    registry.register(Box::new(aip_generate_total.clone()))?;
    let aip_low_confidence_total = IntCounter::with_opts(Opts::new(
        "schedule_aip_low_confidence_total",
        "aip low confidence",
    ))?;
    registry.register(Box::new(aip_low_confidence_total.clone()))?;
    Ok(ScheduleMetrics {
        runs_total,
        runs_duration,
        paused_total,
        trigger_eval_duration,
        active_count,
        event_observations,
        aip_generate_total,
        aip_low_confidence_total,
    })
}

#[allow(dead_code)]
fn _unused_gauge_vec(_v: GaugeVec) {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn metrics_register_against_isolated_registry_without_collision() {
        let registry = Registry::new();
        let m = build_metrics_with_registry(&registry).expect("register");
        m.runs_total.with_label_values(&["SUCCEEDED"]).inc();
        m.paused_total.with_label_values(&["manual"]).inc();
        m.active_count.with_label_values(&["USER"]).set(5);
        m.event_observations.set(7.0);
        m.runs_duration.observe(0.42);
        m.trigger_eval_duration.observe(0.001);
        m.aip_generate_total.inc();
        m.aip_low_confidence_total.inc();

        let families = registry.gather();
        let names: Vec<&str> = families.iter().map(|f| f.get_name()).collect();
        for required in [
            "schedule_runs_total",
            "schedule_runs_duration_seconds",
            "schedule_paused_total",
            "schedule_trigger_evaluation_duration_seconds",
            "schedules_active_count",
            "schedule_event_observations",
            "schedule_aip_generate_total",
            "schedule_aip_low_confidence_total",
        ] {
            assert!(names.contains(&required), "missing metric {required}");
        }
    }

    #[test]
    fn outcome_label_set_matches_doc_enum() {
        let registry = Registry::new();
        let m = build_metrics_with_registry(&registry).unwrap();
        for outcome in ["SUCCEEDED", "IGNORED", "FAILED"] {
            m.runs_total.with_label_values(&[outcome]).inc();
        }
        let families = registry.gather();
        let counter_family = families
            .iter()
            .find(|f| f.get_name() == "schedule_runs_total")
            .expect("counter present");
        assert_eq!(counter_family.get_metric().len(), 3);
    }
}
