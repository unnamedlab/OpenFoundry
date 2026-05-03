//! Kafka → Iceberg materialisation substrate for the lineage service.
//!
//! `lineage-service` consumes Kafka `lineage.events.v1` and
//! materialises into Iceberg `of.lineage.*` (S5.2).

pub const SOURCE_TOPIC: &str = "lineage.events.v1";

pub const CONSUMER_GROUP: &str = "lineage-service";

/// Iceberg target — catalog / namespace / tables.
pub mod iceberg_target {
    pub const CATALOG: &str = "lakekeeper";
    pub const NAMESPACE: &str = "of_lineage";

    /// One row per OpenLineage *run* (start + complete).
    pub const TABLE_RUNS: &str = "runs";

    /// One row per OpenLineage *event* (per state transition).
    pub const TABLE_EVENTS: &str = "events";

    /// Dataset I/O edges per run (input / output side of a job).
    pub const TABLE_DATASETS_IO: &str = "datasets_io";

    /// Partition transform on `events.event_time` and `runs.started_at`.
    pub const PARTITION_TRANSFORM: &str = "day(event_time)";
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn pins_match_plan() {
        assert_eq!(SOURCE_TOPIC, "lineage.events.v1");
        assert_eq!(iceberg_target::NAMESPACE, "of_lineage");
        assert_eq!(iceberg_target::TABLE_RUNS, "runs");
        assert_eq!(iceberg_target::TABLE_EVENTS, "events");
        assert_eq!(iceberg_target::TABLE_DATASETS_IO, "datasets_io");
    }
}
