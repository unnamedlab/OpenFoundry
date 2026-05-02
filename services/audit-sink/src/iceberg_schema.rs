//! Iceberg schema substrate for `of_audit.events` (S5.1.a + S5.1.c).
//!
//! This module pins **only** the schema, partition spec, sort order and
//! maintenance policy that the future Iceberg writer (S5.1.b) must use.
//! No Iceberg client is touched here so the constants are testable and
//! diffable in isolation. The writer lands in [`crate::runtime`] once
//! `event-bus-data` is wired end-to-end.
//!
//! The audit table is **append-only** and **WORM**: snapshot retention
//! is infinite, `expire_snapshots` is disabled forever per ADR-0028 §6
//! ("Audit log is the SoR for compliance — never collapse history").

/// Field names — single source of truth, also used by the producer
/// envelope at `crate::AuditEnvelope`.
pub mod fields {
    pub const EVENT_ID: &str = "event_id"; // uuid v5, dedup key
    pub const AT: &str = "at"; // microseconds since unix epoch — partition source
    pub const CORRELATION_ID: &str = "correlation_id";
    pub const KIND: &str = "kind"; // e.g. "auth.login.ok"
    pub const PAYLOAD: &str = "payload"; // serialized JSON (string column)
}

/// Iceberg type literals as they appear in the catalog REST `create_table`
/// payload. Kept as `&'static str` instead of importing the `iceberg`
/// crate so this module compiles without the `runtime` feature.
pub mod types {
    pub const UUID: &str = "uuid";
    pub const TIMESTAMPTZ: &str = "timestamptz"; // microsecond precision
    pub const STRING: &str = "string";
}

/// Field id assignments. Iceberg requires stable field ids — once
/// shipped these MUST NOT be reused or remapped.
pub mod field_ids {
    pub const EVENT_ID: i32 = 1;
    pub const AT: i32 = 2;
    pub const CORRELATION_ID: i32 = 3;
    pub const KIND: i32 = 4;
    pub const PAYLOAD: i32 = 5;
}

/// Initial schema id. Bumped by every additive evolution; never reused.
pub const INITIAL_SCHEMA_ID: i32 = 0;

/// Partition spec id. Bumped when the partition transform changes.
pub const INITIAL_PARTITION_SPEC_ID: i32 = 0;

/// Sort order id. Bumped when the sort order changes.
pub const INITIAL_SORT_ORDER_ID: i32 = 1;

/// Partition transform — `day(at)` per S5.1.a. Stored as the literal the
/// Iceberg REST catalog accepts.
pub const PARTITION_TRANSFORM: &str = "day";
pub const PARTITION_SOURCE_FIELD: &str = fields::AT;

/// Sort order — events arrive almost-monotonic by `at`, so a single
/// ascending sort key keeps file pruning effective.
pub const SORT_FIELD: &str = fields::AT;
pub const SORT_DIRECTION: &str = "asc";
pub const SORT_NULL_ORDER: &str = "nulls-last";

/// Required (non-null) fields — every audit record carries these.
pub const REQUIRED_FIELDS: &[&str] = &[fields::EVENT_ID, fields::AT, fields::KIND, fields::PAYLOAD];

/// Snapshot / maintenance policy (S5.1.c).
pub mod retention {
    /// Snapshot retention is infinite — every snapshot is preserved.
    pub const SNAPSHOT_RETENTION: &str = "infinite";

    /// Manifest list compaction is allowed (it does NOT drop rows or
    /// snapshot history) — Spark `rewrite_data_files` is fine.
    pub const REWRITE_DATA_FILES_ENABLED: bool = true;

    /// Snapshot expiration is **disabled forever** for the audit table.
    /// Any operator who runs `expire_snapshots` against `of_audit.events`
    /// must treat it as a P1 incident.
    pub const EXPIRE_SNAPSHOTS_ENABLED: bool = false;

    /// Iceberg table properties to apply at create time. The writer in
    /// S5.1.b passes these verbatim to the catalog.
    pub const TABLE_PROPERTIES: &[(&str, &str)] = &[
        ("write.format.default", "parquet"),
        ("write.parquet.compression-codec", "zstd"),
        ("write.metadata.delete-after-commit.enabled", "false"),
        ("write.metadata.previous-versions-max", "999999"),
        ("history.expire.max-snapshot-age-ms", "9223372036854775807"),
        ("history.expire.min-snapshots-to-keep", "2147483647"),
        ("commit.manifest.target-size-bytes", "8388608"),
    ];
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::iceberg_target;

    #[test]
    fn partition_matches_target_constant() {
        // `iceberg_target::PARTITION_TRANSFORM` is `"day(at)"`; the
        // schema module decomposes that into `(transform, source)`.
        assert_eq!(
            format!("{}({})", PARTITION_TRANSFORM, PARTITION_SOURCE_FIELD),
            iceberg_target::PARTITION_TRANSFORM
        );
    }

    #[test]
    fn sort_matches_target_constant() {
        // `iceberg_target::SORT_ORDER` is `"at ASC"`.
        let upper = SORT_DIRECTION.to_ascii_uppercase();
        assert_eq!(format!("{} {}", SORT_FIELD, upper), iceberg_target::SORT_ORDER);
    }

    #[test]
    fn required_fields_cover_envelope_minimum() {
        for f in [fields::EVENT_ID, fields::AT, fields::KIND, fields::PAYLOAD] {
            assert!(REQUIRED_FIELDS.contains(&f), "{f} must be required");
        }
    }

    #[test]
    fn worm_policy_disables_snapshot_expiration() {
        assert!(!retention::EXPIRE_SNAPSHOTS_ENABLED);
        assert_eq!(retention::SNAPSHOT_RETENTION, "infinite");
        // Property table must encode the same fact for the catalog.
        assert!(retention::TABLE_PROPERTIES
            .iter()
            .any(|(k, v)| *k == "history.expire.min-snapshots-to-keep" && *v == "2147483647"));
    }

    #[test]
    fn field_ids_are_stable_and_unique() {
        let ids = [
            field_ids::EVENT_ID,
            field_ids::AT,
            field_ids::CORRELATION_ID,
            field_ids::KIND,
            field_ids::PAYLOAD,
        ];
        let mut sorted = ids.to_vec();
        sorted.sort_unstable();
        sorted.dedup();
        assert_eq!(sorted.len(), ids.len(), "field ids must be unique");
        // First five ids per Iceberg convention.
        assert_eq!(ids, [1, 2, 3, 4, 5]);
    }
}
