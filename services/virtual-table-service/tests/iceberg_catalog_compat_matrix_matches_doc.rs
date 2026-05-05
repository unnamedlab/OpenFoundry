//! Doc-conformance test for the Iceberg catalog × source compatibility
//! matrix.
//!
//! Source of truth: Foundry doc § "Iceberg catalogs" inside
//! `Data connectivity & integration/Core concepts/Virtual tables.md`.
//! Each cell is reproduced inline in [`expected`] so a future doc
//! revision that flips a cell breaks this test until
//! `iceberg_catalogs::compatibility` is updated to match.

use virtual_table_service::domain::capability_matrix::SourceProvider;
use virtual_table_service::domain::iceberg_catalogs::{
    CatalogKind, CompatibilityStatus, compatibility,
};

const fn ga() -> CompatibilityStatus {
    CompatibilityStatus::GenerallyAvailable
}
const fn legacy() -> CompatibilityStatus {
    CompatibilityStatus::Legacy
}
const fn na() -> CompatibilityStatus {
    CompatibilityStatus::NotAvailable
}

fn expected() -> Vec<(SourceProvider, CatalogKind, CompatibilityStatus)> {
    use CatalogKind::*;
    use SourceProvider::*;
    vec![
        // Amazon S3 row.
        (AmazonS3, AwsGlue, ga()),
        (AmazonS3, Horizon, na()),
        (AmazonS3, ObjectStorage, ga()),
        (AmazonS3, Polaris, ga()),
        (AmazonS3, UnityCatalog, legacy()),
        // Databricks row — only Unity GA.
        (Databricks, AwsGlue, na()),
        (Databricks, Horizon, na()),
        (Databricks, ObjectStorage, na()),
        (Databricks, Polaris, na()),
        (Databricks, UnityCatalog, ga()),
        // GCS row — only Object Storage.
        (Gcs, AwsGlue, na()),
        (Gcs, Horizon, na()),
        (Gcs, ObjectStorage, ga()),
        (Gcs, Polaris, na()),
        (Gcs, UnityCatalog, na()),
        // Azure ADLS row.
        (AzureAbfs, AwsGlue, na()),
        (AzureAbfs, Horizon, na()),
        (AzureAbfs, ObjectStorage, ga()),
        (AzureAbfs, Polaris, ga()),
        (AzureAbfs, UnityCatalog, legacy()),
        // Snowflake row.
        (Snowflake, AwsGlue, na()),
        (Snowflake, Horizon, ga()),
        (Snowflake, ObjectStorage, na()),
        (Snowflake, Polaris, na()),
        (Snowflake, UnityCatalog, na()),
    ]
}

#[test]
fn matrix_matches_published_foundry_doc() {
    for (provider, catalog, status) in expected() {
        let observed = compatibility(provider, catalog);
        assert_eq!(
            observed, status,
            "{:?} × {:?} drifted from doc — expected {:?}, got {:?}",
            provider, catalog, status, observed
        );
    }
}

#[test]
fn unity_is_legacy_for_s3_and_adls_per_doc_note() {
    assert_eq!(
        compatibility(SourceProvider::AmazonS3, CatalogKind::UnityCatalog),
        CompatibilityStatus::Legacy
    );
    assert_eq!(
        compatibility(SourceProvider::AzureAbfs, CatalogKind::UnityCatalog),
        CompatibilityStatus::Legacy
    );
}

#[test]
fn bigquery_does_not_support_any_iceberg_catalog() {
    for catalog in [
        CatalogKind::AwsGlue,
        CatalogKind::Horizon,
        CatalogKind::ObjectStorage,
        CatalogKind::Polaris,
        CatalogKind::UnityCatalog,
    ] {
        assert_eq!(
            compatibility(SourceProvider::BigQuery, catalog),
            CompatibilityStatus::NotAvailable
        );
    }
}

#[test]
fn snowflake_horizon_is_generally_available() {
    assert_eq!(
        compatibility(SourceProvider::Snowflake, CatalogKind::Horizon),
        CompatibilityStatus::GenerallyAvailable
    );
}
