//! Iceberg schema substrate for `of_lineage.*` (S5.2.a).
//!
//! Pins the column set, partition transform and sort order for the
//! three tables behind the OpenLineage materialisation pipeline. The
//! writer is in [`crate::kafka_to_iceberg`] (lands in the runtime PR).
//!
//! Table layout follows OpenLineage 1.x event conventions
//! (<https://openlineage.io/spec/>) trimmed to what the Foundry-parity
//! UI actually queries.

pub mod runs {
    use super::common;

    pub const TABLE: &str = "runs";

    pub mod fields {
        pub const RUN_ID: &str = "run_id"; // uuid, OpenLineage run id
        pub const JOB_NAMESPACE: &str = "job_namespace";
        pub const JOB_NAME: &str = "job_name";
        pub const STARTED_AT: &str = "started_at"; // partition source
        pub const COMPLETED_AT: &str = "completed_at";
        pub const STATE: &str = "state"; // RUNNING|COMPLETE|FAIL|ABORT
        pub const FACETS: &str = "facets"; // serialized JSON
    }

    pub const PARTITION_SOURCE_FIELD: &str = fields::STARTED_AT;
    pub const PARTITION_TRANSFORM: &str = common::DAY;
    pub const SORT_FIELD: &str = fields::STARTED_AT;
    pub const SORT_DIRECTION: &str = common::ASC;
    pub const REQUIRED: &[&str] = &[
        fields::RUN_ID,
        fields::JOB_NAMESPACE,
        fields::JOB_NAME,
        fields::STARTED_AT,
        fields::STATE,
    ];
}

pub mod events {
    use super::common;

    pub const TABLE: &str = "events";

    pub mod fields {
        pub const EVENT_ID: &str = "event_id"; // uuid, dedup
        pub const RUN_ID: &str = "run_id"; // uuid, FK→runs.run_id
        pub const EVENT_TIME: &str = "event_time"; // partition source
        pub const EVENT_TYPE: &str = "event_type"; // START|RUNNING|COMPLETE|FAIL|ABORT
        pub const PRODUCER: &str = "producer"; // OL producer URI
        pub const SCHEMA_URL: &str = "schema_url";
        pub const PAYLOAD: &str = "payload"; // serialized JSON
    }

    pub const PARTITION_SOURCE_FIELD: &str = fields::EVENT_TIME;
    pub const PARTITION_TRANSFORM: &str = common::DAY;
    pub const SORT_FIELD: &str = fields::EVENT_TIME;
    pub const SORT_DIRECTION: &str = common::ASC;
    pub const REQUIRED: &[&str] = &[
        fields::EVENT_ID,
        fields::RUN_ID,
        fields::EVENT_TIME,
        fields::EVENT_TYPE,
    ];
}

pub mod datasets_io {
    use super::common;

    pub const TABLE: &str = "datasets_io";

    pub mod fields {
        pub const RUN_ID: &str = "run_id";
        pub const EVENT_TIME: &str = "event_time"; // partition source
        pub const SIDE: &str = "side"; // "input" | "output"
        pub const DATASET_NAMESPACE: &str = "dataset_namespace";
        pub const DATASET_NAME: &str = "dataset_name";
        pub const FACETS: &str = "facets"; // JSON
    }

    pub const PARTITION_SOURCE_FIELD: &str = fields::EVENT_TIME;
    pub const PARTITION_TRANSFORM: &str = common::DAY;
    pub const SORT_FIELD: &str = fields::EVENT_TIME;
    pub const SORT_DIRECTION: &str = common::ASC;
    pub const REQUIRED: &[&str] = &[
        fields::RUN_ID,
        fields::EVENT_TIME,
        fields::SIDE,
        fields::DATASET_NAMESPACE,
        fields::DATASET_NAME,
    ];
    pub const SIDE_INPUT: &str = "input";
    pub const SIDE_OUTPUT: &str = "output";
}

pub mod common {
    pub const DAY: &str = "day";
    pub const ASC: &str = "asc";
    pub const NULLS_LAST: &str = "nulls-last";

    /// Lineage tables are smaller than audit and we *do* allow snapshot
    /// expiration after 90 days — lineage history is regenerable from
    /// Cassandra hot path, unlike audit. This contrasts with
    /// `audit_sink::iceberg_schema::retention` which forbids expiry.
    pub const TABLE_PROPERTIES: &[(&str, &str)] = &[
        ("write.format.default", "parquet"),
        ("write.parquet.compression-codec", "zstd"),
        ("history.expire.max-snapshot-age-ms", "7776000000"), // 90d
        ("history.expire.min-snapshots-to-keep", "10"),
    ];
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::kafka_to_iceberg::iceberg_target;

    #[test]
    fn table_names_match_target_constants() {
        assert_eq!(runs::TABLE, iceberg_target::TABLE_RUNS);
        assert_eq!(events::TABLE, iceberg_target::TABLE_EVENTS);
        assert_eq!(datasets_io::TABLE, iceberg_target::TABLE_DATASETS_IO);
    }

    #[test]
    fn partition_transform_consistent_with_target() {
        // events.event_time is the canonical partition source quoted in
        // the target const `"day(event_time)"`.
        assert_eq!(
            format!("{}({})", events::PARTITION_TRANSFORM, events::PARTITION_SOURCE_FIELD),
            iceberg_target::PARTITION_TRANSFORM
        );
    }

    #[test]
    fn datasets_io_side_values_documented() {
        assert_eq!(datasets_io::SIDE_INPUT, "input");
        assert_eq!(datasets_io::SIDE_OUTPUT, "output");
    }
}
