//! Runtime substrate for the `ai-sink` binary.
//!
//! Same shape as `audit_sink::runtime`: pin metric names + the
//! Kafka-side constants used by the (yet-to-land) consumer loop.

pub mod metrics {
    /// Histogram — gap between `event.at` (producer timestamp) and the
    /// instant the row is appended to Iceberg.
    pub const SINK_LAG_SECONDS: &str = "ai_sink_lag_seconds";

    /// Counter — total records appended (labelled by target table).
    pub const SINK_RECORDS_TOTAL: &str = "ai_sink_records_total";

    /// Histogram — number of records per Iceberg commit.
    pub const SINK_BATCH_SIZE: &str = "ai_sink_batch_size";

    /// Counter — Iceberg commits performed (labelled by table + result).
    pub const SINK_COMMITS_TOTAL: &str = "ai_sink_commits_total";
}
