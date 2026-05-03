//! Prometheus metrics for the Builds lifecycle.
//!
//! Mirrors the Foundry "Builds.md § Build lifecycle" stages so the
//! dashboards line up with the doc one-to-one:
//!
//!   * `build_state_total{state}` — counter, ticked once each time a
//!     build *enters* a state.
//!   * `jobs_in_state{state}` — gauge, sampled on every job
//!     transition.
//!   * `build_resolution_duration_seconds` — histogram around
//!     [`build_resolution::resolve_build`].
//!   * `build_lock_acquisition_duration_seconds` — histogram around
//!     [`build_resolution::acquire_locks`].

use once_cell::sync::Lazy;
use prometheus::{
    Histogram, HistogramOpts, HistogramVec, IntCounter, IntCounterVec, IntGauge, IntGaugeVec, Opts,
    register_histogram, register_histogram_vec, register_int_counter,
    register_int_counter_vec, register_int_gauge, register_int_gauge_vec,
};

use crate::domain::logs::LogLevel;
use crate::models::build::{AbortPolicy, BuildState};
use crate::models::job::JobState;

pub static BUILD_STATE_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "build_state_total",
            "Total times a build entered the given BuildState",
        ),
        &["state"],
    )
    .expect("register build_state_total")
});

pub static JOBS_IN_STATE: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "jobs_in_state",
            "Number of jobs currently sitting in each JobState",
        ),
        &["state"],
    )
    .expect("register jobs_in_state")
});

pub static BUILD_RESOLUTION_DURATION_SECONDS: Lazy<Histogram> = Lazy::new(|| {
    register_histogram!(
        HistogramOpts::new(
            "build_resolution_duration_seconds",
            "Wall-clock seconds spent in the Build resolution step (specs + cycles + inputs + locks)",
        )
        .buckets(vec![
            0.005, 0.025, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0,
        ])
    )
    .expect("register build_resolution_duration_seconds")
});

pub static BUILD_LOCK_ACQUISITION_DURATION_SECONDS: Lazy<Histogram> = Lazy::new(|| {
    register_histogram!(
        HistogramOpts::new(
            "build_lock_acquisition_duration_seconds",
            "Wall-clock seconds spent opening output transactions and persisting build_input_locks",
        )
        .buckets(vec![
            0.001, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0,
        ])
    )
    .expect("register build_lock_acquisition_duration_seconds")
});

/// Foundry "Build branch guarantees" outcome counter. Incremented
/// once per `compile_build_graph + resolve_input/output` cycle so
/// SREs can spot the difference between cycle / missing-spec /
/// incompatible-ancestry failures vs. healthy builds.
///
/// Label values:
///   * `ok` — graph compiled, all inputs resolved, locks acquired.
///   * `missing_spec` — no JobSpec found for an output along the
///     fallback chain.
///   * `incompatible_ancestry` — chain crossed a parent boundary that
///     doesn't exist on the dataset.
///   * `cycle` — JobSpec graph is cyclic.
pub static BUILD_RESOLUTIONS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "build_resolutions_total",
            "Total Foundry build-resolution attempts, labelled by outcome",
        ),
        &["outcome"],
    )
    .expect("register build_resolutions_total")
});

/// Stable label set used by [`record_build_resolution`]. Kept here so
/// every emitter agrees on the vocabulary and `init()` can pre-touch
/// each variant.
pub const BUILD_RESOLUTION_OUTCOMES: &[&str] = &[
    "ok",
    "missing_spec",
    "incompatible_ancestry",
    "cycle",
];

pub fn record_build_resolution(outcome: &str) {
    BUILD_RESOLUTIONS_TOTAL
        .with_label_values(&[outcome])
        .inc();
}

/// Pre-touch every label combination so dashboards render even before
/// the first build is submitted.
pub fn init() {
    for state in BuildState::ALL {
        let _ = BUILD_STATE_TOTAL.with_label_values(&[state.as_str()]);
    }
    for state in JobState::ALL {
        let _ = JOBS_IN_STATE.with_label_values(&[state.as_str()]);
    }
    let _ = BUILD_RESOLUTION_DURATION_SECONDS.get_sample_count();
    let _ = BUILD_LOCK_ACQUISITION_DURATION_SECONDS.get_sample_count();
    for outcome in BUILD_RESOLUTION_OUTCOMES {
        let _ = BUILD_RESOLUTIONS_TOTAL.with_label_values(&[outcome]);
    }
    init_p2();
    init_p4();
}

pub fn record_build_state(state: BuildState) {
    BUILD_STATE_TOTAL
        .with_label_values(&[state.as_str()])
        .inc();
}

pub fn set_jobs_in_state(state: JobState, value: i64) {
    JOBS_IN_STATE
        .with_label_values(&[state.as_str()])
        .set(value);
}

// ---------------------------------------------------------------------------
// P2 — Job-execution metrics
// ---------------------------------------------------------------------------

/// Jobs that the staleness check skipped (Foundry doc § Staleness:
/// "If an output dataset is fresh, it will not be recomputed in
/// subsequent builds."). Incremented once per skipped job.
pub static BUILD_JOBS_SKIPPED_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "build_jobs_skipped_total",
        "Jobs short-circuited by the staleness check (stale_skipped = TRUE)",
    )
    .expect("register build_jobs_skipped_total")
});

/// Failure cascades, labelled by the abort policy that drove them.
/// Tracks both the trigger (`policy`) and the magnitude (`scope`).
///
/// Labels:
///   * `policy` — `DEPENDENT_ONLY` | `ALL_NON_DEPENDENT`.
///   * `scope`  — `dependent` (jobs aborted because they transitively
///                  depend on the failed job) or `independent`
///                  (jobs aborted as a side-effect of
///                  `ALL_NON_DEPENDENT`).
pub static BUILD_FAILURE_CASCADES_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "build_failure_cascades_total",
            "Jobs aborted as part of a failure cascade, labelled by policy + scope",
        ),
        &["policy", "scope"],
    )
    .expect("register build_failure_cascades_total")
});

/// Wall-clock seconds spent inside the parallel orchestrator
/// (`build_executor::execute_build`). Distinct from
/// `build_resolution_duration_seconds` which only covers the
/// pre-execute resolution step.
pub static BUILD_EXECUTION_DURATION_SECONDS: Lazy<Histogram> = Lazy::new(|| {
    register_histogram!(
        HistogramOpts::new(
            "build_execution_duration_seconds",
            "Wall-clock seconds spent driving jobs to terminal state",
        )
        .buckets(vec![
            0.05, 0.25, 1.0, 5.0, 15.0, 60.0, 300.0, 900.0, 3600.0,
        ])
    )
    .expect("register build_execution_duration_seconds")
});

pub fn record_job_skipped() {
    BUILD_JOBS_SKIPPED_TOTAL.inc();
}

pub fn record_failure_cascade(policy: AbortPolicy, scope: &str) {
    BUILD_FAILURE_CASCADES_TOTAL
        .with_label_values(&[policy.as_str(), scope])
        .inc();
}

/// Pre-touch every label combination introduced by P2 so dashboards
/// stay populated. Called from [`init`].
pub fn init_p2() {
    let _ = BUILD_JOBS_SKIPPED_TOTAL.get();
    for policy in AbortPolicy::ALL {
        for scope in &["dependent", "independent"] {
            let _ = BUILD_FAILURE_CASCADES_TOTAL.with_label_values(&[policy.as_str(), scope]);
        }
    }
    let _ = BUILD_EXECUTION_DURATION_SECONDS.get_sample_count();
}

// ---------------------------------------------------------------------------
// P4 — Live logs + outbox metrics
// ---------------------------------------------------------------------------

/// Total wall-clock duration of a build, sampled when the build
/// reaches a terminal state. Labelled by `BuildState`.
pub static BUILD_DURATION_SECONDS: Lazy<HistogramVec> = Lazy::new(|| {
    register_histogram_vec!(
        HistogramOpts::new(
            "build_duration_seconds",
            "Wall-clock seconds from queued_at to finished_at, by terminal state",
        )
        .buckets(vec![1.0, 5.0, 30.0, 120.0, 600.0, 1800.0, 3600.0, 7200.0]),
        &["state"]
    )
    .expect("register build_duration_seconds")
});

/// Job throughput labelled by terminal state and logic kind.
pub static BUILD_JOBS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "build_jobs_total",
            "Total jobs that reached a terminal state, labelled by state and logic_kind",
        ),
        &["state", "kind"]
    )
    .expect("register build_jobs_total")
});

/// Per-job duration histogram, labelled by logic kind so the
/// dashboards can break Sync vs Transform vs Health-check timing.
pub static BUILD_JOB_DURATION_SECONDS: Lazy<HistogramVec> = Lazy::new(|| {
    register_histogram_vec!(
        HistogramOpts::new(
            "build_job_duration_seconds",
            "Wall-clock seconds from RUN_PENDING to terminal state per job, by logic_kind",
        )
        .buckets(vec![0.1, 0.5, 1.0, 5.0, 15.0, 60.0, 300.0, 900.0]),
        &["kind"]
    )
    .expect("register build_job_duration_seconds")
});

/// Total log entries emitted, labelled by [`crate::domain::logs::LogLevel`].
pub static BUILD_LOGS_EMITTED_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "build_logs_emitted_total",
            "Total live-log entries persisted, by level",
        ),
        &["level"]
    )
    .expect("register build_logs_emitted_total")
});

/// Active SSE / WebSocket subscribers across all jobs.
pub static LIVE_LOG_SUBSCRIBERS: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "live_log_subscribers",
            "Live-log subscribers per job_rid (sum across all jobs available via Prometheus aggregation)",
        ),
        &["job_rid"]
    )
    .expect("register live_log_subscribers")
});

/// Builds currently sitting in BUILD_QUEUED. Sampled by the queue
/// poller; refreshed on every transition into / out of QUEUED.
pub static BUILD_QUEUE_DEPTH: Lazy<IntGauge> = Lazy::new(|| {
    register_int_gauge!(
        "build_queue_depth",
        "Number of builds currently in BUILD_QUEUED",
    )
    .expect("register build_queue_depth")
});

pub fn record_build_duration(state: BuildState, seconds: f64) {
    BUILD_DURATION_SECONDS
        .with_label_values(&[state.as_str()])
        .observe(seconds);
}

pub fn record_job_terminal(state: JobState, kind: &str) {
    BUILD_JOBS_TOTAL
        .with_label_values(&[state.as_str(), kind])
        .inc();
}

pub fn record_job_duration(kind: &str, seconds: f64) {
    BUILD_JOB_DURATION_SECONDS
        .with_label_values(&[kind])
        .observe(seconds);
}

pub fn record_log_emitted(level: LogLevel) {
    BUILD_LOGS_EMITTED_TOTAL
        .with_label_values(&[level.as_str()])
        .inc();
}

pub fn set_live_log_subscribers_for(job_rid: &str, count: i64) {
    LIVE_LOG_SUBSCRIBERS
        .with_label_values(&[job_rid])
        .set(count);
}

pub fn set_build_queue_depth(depth: i64) {
    BUILD_QUEUE_DEPTH.set(depth);
}

/// Pre-touch P4 series so dashboards render before the first event.
pub fn init_p4() {
    for level in LogLevel::ALL {
        let _ = BUILD_LOGS_EMITTED_TOTAL.with_label_values(&[level.as_str()]);
    }
    for state in BuildState::ALL {
        let _ = BUILD_DURATION_SECONDS.with_label_values(&[state.as_str()]);
    }
    let _ = BUILD_QUEUE_DEPTH.get();
}
