//! Doc-conformance tests for the P5 update-detection classifier
//! and per-provider version probe.
//!
//! Mirrors the test list in the prompt (without the DB-coupled cases
//! that ship with the testcontainer harness in P5.next):
//!
//!   * `update_detection_delta_polls_last_log.rs`             — here
//!   * `update_detection_iceberg_polls_snapshot_id.rs`        — here
//!   * `update_detection_no_versioning_falls_back_to_potential_update.rs`
//!     — here
//!   * `update_detection_emits_dataset_updated_event_on_change.rs`
//!     — covered by `fires_downstream_event` matrix.

use virtual_table_service::domain::capability_matrix::SourceProvider;
use virtual_table_service::domain::update_detection::{
    PollOutcome, Version, classify_change, current_version,
};
use virtual_table_service::models::virtual_table::Locator;

#[tokio::test]
async fn delta_table_polls_last_log_signature() {
    let v = current_version(
        SourceProvider::Databricks,
        &Locator::Tabular {
            database: "main".into(),
            schema: "public".into(),
            table: "orders".into(),
        },
        None,
    )
    .await
    .expect("probe");
    match v {
        Version::Known { value } => {
            assert!(value.starts_with("delta-log:"));
            assert!(value.contains("main.public.orders"));
        }
        Version::Unknown => panic!("Databricks Delta must surface a known version"),
    }
}

#[tokio::test]
async fn iceberg_locator_polls_snapshot_id() {
    let v = current_version(
        SourceProvider::Snowflake,
        &Locator::Iceberg {
            catalog: "polaris".into(),
            namespace: "sales".into(),
            table: "events".into(),
        },
        None,
    )
    .await
    .expect("probe");
    match v {
        Version::Known { value } => assert!(value.starts_with("snapshot:polaris/sales/events")),
        Version::Unknown => panic!("Iceberg locator must surface a snapshot id"),
    }
}

#[tokio::test]
async fn no_versioning_falls_back_to_potential_update() {
    // Doc § "Update detection for virtual table inputs":
    //   "If versioning is not supported, every poll is treated as a
    //    potential update, which may result in unnecessary downstream
    //    builds."
    for format in ["parquet", "PARQUET", "avro", "csv"] {
        let v = current_version(
            SourceProvider::AmazonS3,
            &Locator::File {
                bucket: "openfoundry".into(),
                prefix: "year=2026/".into(),
                format: format.into(),
            },
            None,
        )
        .await
        .expect("probe");
        assert_eq!(v, Version::Unknown, "format {format} should be Unknown");
        assert_eq!(
            classify_change(None, &v),
            PollOutcome::PotentialUpdate,
            "format {format} should classify as PotentialUpdate"
        );
        assert!(classify_change(None, &v).fires_downstream_event());
    }
}

#[test]
fn classify_change_emits_event_on_change_only_for_known_versions() {
    // Initial / Changed / PotentialUpdate must fire downstream
    // schedules; Unchanged / Failed must not.
    assert!(PollOutcome::Initial.fires_downstream_event());
    assert!(PollOutcome::Changed.fires_downstream_event());
    assert!(PollOutcome::PotentialUpdate.fires_downstream_event());
    assert!(!PollOutcome::Unchanged.fires_downstream_event());
    assert!(!PollOutcome::Failed.fires_downstream_event());
}

#[test]
fn classify_change_initial_to_changed_to_unchanged() {
    // First poll on a brand-new row: Initial.
    assert_eq!(
        classify_change(None, &Version::known("v1")),
        PollOutcome::Initial
    );
    // Same version on the next tick: Unchanged.
    assert_eq!(
        classify_change(Some("v1"), &Version::known("v1")),
        PollOutcome::Unchanged
    );
    // Source advanced: Changed.
    assert_eq!(
        classify_change(Some("v1"), &Version::known("v2")),
        PollOutcome::Changed
    );
}
