//! Surface-level checks for the registration / discovery contract.
//!
//! These tests run without a database — DB-backed integration tests
//! arrive in P2/P3 once the testcontainers harness lands for this
//! service. They exercise the public API surface so a downstream
//! caller can compile against the crate without surprises.

use virtual_table_service::domain::capability_matrix::{
    SourceProvider, TableType, capabilities_for, iter_cells,
};
use virtual_table_service::domain::virtual_tables::RegistrationKind;
use virtual_table_service::models::virtual_table::Locator;

#[test]
fn registration_kind_labels_are_lowercase() {
    for kind in [
        RegistrationKind::Manual,
        RegistrationKind::Bulk,
        RegistrationKind::Auto,
    ] {
        assert!(kind.as_str().chars().all(|c| c.is_ascii_lowercase()));
    }
}

#[test]
fn iter_cells_visits_every_documented_combination_at_least_once() {
    // Every provider that the doc lists must appear at least once.
    let providers: std::collections::HashSet<_> =
        iter_cells().map(|(p, _, _)| p).collect();
    for required in [
        SourceProvider::AmazonS3,
        SourceProvider::AzureAbfs,
        SourceProvider::BigQuery,
        SourceProvider::Databricks,
        SourceProvider::FoundryIceberg,
        SourceProvider::Gcs,
        SourceProvider::Snowflake,
    ] {
        assert!(
            providers.contains(&required),
            "matrix is missing provider {:?}",
            required
        );
    }
}

#[test]
fn capabilities_for_falls_back_to_unknown_for_unsupported_pairs() {
    // S3 + Materialized View is not in the doc → must return the
    // permissive read-only fallback.
    let caps = capabilities_for(SourceProvider::AmazonS3, TableType::MaterializedView);
    assert!(caps.read);
    assert!(!caps.write);
    assert!(caps.compute_pushdown.is_none());
}

#[tokio::test]
async fn schema_inference_returns_columns_for_warehouse_locator() {
    let cols = virtual_table_service::domain::schema_inference::infer_for_provider(
        SourceProvider::Snowflake,
        &Locator::Tabular {
            database: "warehouse".into(),
            schema: "public".into(),
            table: "orders".into(),
        },
    )
    .await
    .expect("infer");
    assert!(!cols.is_empty());
    assert_eq!(cols[0].name, "id");
}
