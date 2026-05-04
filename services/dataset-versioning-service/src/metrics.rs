//! Prometheus metric registry for the dataset-versioning service.
//!
//! T8.1 — Foundry-aligned `dataset_*` family covering:
//!
//!   * `dataset_transactions_open` — gauge per `(dataset_rid, branch)`,
//!     tracks the unique OPEN transaction each branch may carry at a
//!     time. Inc on `start_transaction` success; dec on commit/abort.
//!   * `dataset_transactions_committed_total{type}` — counter labelled
//!     by `SNAPSHOT|APPEND|UPDATE|DELETE`.
//!   * `dataset_transactions_aborted_total` — counter.
//!   * `dataset_view_compute_duration_seconds` — histogram around the
//!     `compute_view_at` algorithm (the hot path that materialises a
//!     branch view from its committed transactions).
//!   * `dataset_branch_fallback_resolutions_total{outcome=hit|miss}` —
//!     counter incremented by the (forthcoming) fallback resolver.
//!     Both label values are pre-registered at boot so the dashboard
//!     panels render immediately even before the first resolution.

use once_cell::sync::Lazy;
use prometheus::{
    Histogram, HistogramOpts, IntCounter, IntCounterVec, IntGauge, IntGaugeVec, Opts,
    register_histogram, register_int_counter, register_int_counter_vec, register_int_gauge,
    register_int_gauge_vec,
};

/// Legacy generic counter retained from Bloque 7 for the smoke test.
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

/// Number of currently-OPEN transactions, keyed by `(dataset_rid,
/// branch)`. Foundry guarantees at most one OPEN per branch — the
/// gauge therefore peaks at the number of branches with work in flight.
pub static DATASET_TRANSACTIONS_OPEN: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "dataset_transactions_open",
            "Currently OPEN dataset transactions per (dataset_rid, branch)",
        ),
        &["dataset_rid", "branch"],
    )
    .expect("register dataset_transactions_open")
});

/// Total committed transactions, labelled by Foundry `tx_type`.
pub static DATASET_TRANSACTIONS_COMMITTED_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_transactions_committed_total",
            "Total committed dataset transactions, labelled by Foundry transaction type",
        ),
        &["type"],
    )
    .expect("register dataset_transactions_committed_total")
});

/// Total aborted transactions (any branch / type).
pub static DATASET_TRANSACTIONS_ABORTED_TOTAL: Lazy<IntCounter> = Lazy::new(|| {
    register_int_counter!(
        "dataset_transactions_aborted_total",
        "Total aborted dataset transactions",
    )
    .expect("register dataset_transactions_aborted_total")
});

/// End-to-end duration of the `compute_view_at` algorithm. Buckets are
/// chosen for read paths that range from instantaneous (cached, single
/// SNAPSHOT) to multi-second (long branch with many APPENDs).
pub static DATASET_VIEW_COMPUTE_DURATION_SECONDS: Lazy<Histogram> = Lazy::new(|| {
    register_histogram!(
        HistogramOpts::new(
            "dataset_view_compute_duration_seconds",
            "Wall-clock seconds spent assembling a branch view from its committed transactions",
        )
        .buckets(vec![
            0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
        ])
    )
    .expect("register dataset_view_compute_duration_seconds")
});

/// Total branches created, labelled by source-kind (`root` |
/// `child_from_branch` | `child_from_transaction`). Mirrors
/// `BranchSourceKind::metric_label` in `handlers::foundry`.
pub static DATASET_BRANCHES_CREATED_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_branches_created_total",
            "Total branches minted, labelled by P1 source-kind"
        ),
        &["kind"],
    )
    .expect("register dataset_branches_created_total")
});

/// Sampled gauge — number of active branches that currently have an
/// OPEN transaction. Refreshed on every transaction lifecycle event
/// (`start_transaction` / commit / abort) and on demand via
/// `metrics::refresh_branch_gauges`.
pub static DATASET_BRANCHES_WITH_OPEN_TX: Lazy<IntGauge> = Lazy::new(|| {
    register_int_gauge!(
        "dataset_branches_with_open_tx",
        "Active dataset branches that currently carry an OPEN transaction"
    )
    .expect("register dataset_branches_with_open_tx")
});

/// Sampled gauge — total active branches, labelled by `is_root`.
/// Pre-registered with both label values at boot so dashboard panels
/// render immediately even before the first sample.
pub static DATASET_BRANCHES_TOTAL: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "dataset_branches_total",
            "Active dataset branches, labelled by is_root"
        ),
        &["is_root"],
    )
    .expect("register dataset_branches_total")
});

/// P4 — branches archived by the retention worker. `reason` is
/// `ttl` for automatic eligibility runs, `manual` when the archive
/// was forced via the admin endpoint.
pub static DATASET_BRANCHES_ARCHIVED_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_branches_archived_total",
            "Branches archived, labelled by reason (ttl|manual)",
        ),
        &["reason"],
    )
    .expect("register dataset_branches_archived_total")
});

/// P4 — branches that *would* be archived on the next worker tick.
/// Sampled at the end of every `retention_worker::run_once`.
pub static DATASET_BRANCHES_ARCHIVE_ELIGIBLE: Lazy<IntGauge> = Lazy::new(|| {
    register_int_gauge!(
        "dataset_branches_archive_eligible",
        "Branches that match the retention worker's archive criteria right now",
    )
    .expect("register dataset_branches_archive_eligible")
});

/// Branch fallback resolution outcomes. `hit` = a fallback branch
/// satisfied a read; `miss` = no fallback configured / none usable.
pub static DATASET_BRANCH_FALLBACK_RESOLUTIONS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_branch_fallback_resolutions_total",
            "Branch fallback resolutions, labelled by outcome (hit|miss)",
        ),
        &["outcome"],
    )
    .expect("register dataset_branch_fallback_resolutions_total")
});

/// T9.2 — RBAC denials at dataset-versioning mutation endpoints,
/// labelled by the high-level operation that was rejected
/// (branch.create, branch.delete, transaction.open, …). Distinct from
/// marking-enforcement denials; surfaces missing scopes vs. clearance
/// gaps.
pub static DATASET_RBAC_DENIALS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "dataset_rbac_denials_total",
            "Dataset mutation requests rejected by the in-process RBAC gate",
        ),
        &["operation"],
    )
    .expect("register dataset_rbac_denials_total")
});

/// Sugar — `metrics::open_gauge(rid, branch).inc()` is more readable
/// than threading `with_label_values` through every handler.
pub fn open_gauge(dataset_rid: &str, branch: &str) -> IntGauge {
    DATASET_TRANSACTIONS_OPEN.with_label_values(&[dataset_rid, branch])
}

/// Touch every lazy static so `prometheus::gather()` returns the full
/// `dataset_*` family even before the first request.
pub fn init() {
    DATASET_VERSIONING_INFO.inc();
    let _ = DATASET_TX_TOTAL.with_label_values(&["bootstrap"]);

    for ty in ["SNAPSHOT", "APPEND", "UPDATE", "DELETE"] {
        let _ = DATASET_TRANSACTIONS_COMMITTED_TOTAL.with_label_values(&[ty]);
    }
    let _ = DATASET_TRANSACTIONS_ABORTED_TOTAL.get();
    let _ = DATASET_VIEW_COMPUTE_DURATION_SECONDS.get_sample_count();
    for outcome in ["hit", "miss"] {
        let _ = DATASET_BRANCH_FALLBACK_RESOLUTIONS_TOTAL.with_label_values(&[outcome]);
    }
    for op in [
        "branch.create",
        "branch.delete",
        "branch.reparent",
        "branch.fallbacks.update",
        "transaction.open",
        "transaction.commit",
        "transaction.abort",
    ] {
        let _ = DATASET_RBAC_DENIALS_TOTAL.with_label_values(&[op]);
    }
    for kind in ["root", "child_from_branch", "child_from_transaction"] {
        let _ = DATASET_BRANCHES_CREATED_TOTAL.with_label_values(&[kind]);
    }
    let _ = DATASET_BRANCHES_WITH_OPEN_TX.get();
    for is_root in ["true", "false"] {
        let _ = DATASET_BRANCHES_TOTAL.with_label_values(&[is_root]);
    }
    for reason in ["ttl", "manual"] {
        let _ = DATASET_BRANCHES_ARCHIVED_TOTAL.with_label_values(&[reason]);
    }
    let _ = DATASET_BRANCHES_ARCHIVE_ELIGIBLE.get();
}

/// Sample DB-derived gauges. Cheap query, but not free — call from
/// branch / transaction lifecycle hooks rather than every request.
pub async fn refresh_branch_gauges(pool: &sqlx::PgPool) -> Result<(), sqlx::Error> {
    let with_open_tx: i64 = sqlx::query_scalar(
        r#"SELECT COUNT(DISTINCT t.branch_id)
             FROM dataset_transactions t
             JOIN dataset_branches b ON b.id = t.branch_id
            WHERE t.status = 'OPEN' AND b.deleted_at IS NULL"#,
    )
    .fetch_one(pool)
    .await?;
    DATASET_BRANCHES_WITH_OPEN_TX.set(with_open_tx);

    let counts: (i64, i64) = sqlx::query_as(
        r#"SELECT
              COUNT(*) FILTER (WHERE parent_branch_id IS NULL) AS roots,
              COUNT(*) FILTER (WHERE parent_branch_id IS NOT NULL) AS children
            FROM dataset_branches WHERE deleted_at IS NULL"#,
    )
    .fetch_one(pool)
    .await?;
    DATASET_BRANCHES_TOTAL
        .with_label_values(&["true"])
        .set(counts.0);
    DATASET_BRANCHES_TOTAL
        .with_label_values(&["false"])
        .set(counts.1);
    Ok(())
}
