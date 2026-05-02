//! Runtime wiring for `audit-sink` (Kafka → Iceberg writer).
//!
//! Pinned constants only. The Iceberg client + Kafka loop land in a
//! follow-up PR (S5.1.b).

/// Prometheus metric names — pinned so dashboards and alert rules
/// (`infra/k8s/observability/prometheus-rules-audit-sink.yaml`)
/// reference them as constants.
pub mod metrics {
    /// Histogram (seconds): gap between `event.at` and the moment
    /// the snapshot containing that record was committed in Iceberg.
    /// SLO P99 < 90s under steady load.
    pub const SINK_LAG_SECONDS: &str = "audit_sink_lag_seconds";

    /// Counter: records persisted to Iceberg.
    pub const SINK_RECORDS_TOTAL: &str = "audit_sink_records_total";

    /// Histogram: number of records per Iceberg snapshot.
    pub const SINK_BATCH_SIZE: &str = "audit_sink_batch_size_records";

    /// Counter: snapshot commits, labelled `outcome={ok,fail}`.
    pub const SINK_COMMITS_TOTAL: &str = "audit_sink_commits_total";
}
