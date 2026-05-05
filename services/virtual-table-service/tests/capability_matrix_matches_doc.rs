//! Doc-conformance test for the capability matrix.
//!
//! Re-implements the cells of the Foundry doc § "Virtual table
//! compatibility matrix by source & table type" inline, then asserts
//! that `capabilities_for(provider, table_type)` returns exactly the
//! same triple. If a Foundry doc revision changes a cell, this test
//! fails until `capability_matrix.rs` is updated to match.

use virtual_table_service::domain::capability_matrix::{
    Capabilities, ComputePushdownEngine, FoundryCompute, SourceProvider, TableType,
    capabilities_for,
};

const ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE: FoundryCompute = FoundryCompute {
    python_single_node: true,
    python_spark: true,
    pipeline_builder_single_node: false,
    pipeline_builder_spark: true,
};

const SPARK_ONLY: FoundryCompute = FoundryCompute {
    python_single_node: false,
    python_spark: true,
    pipeline_builder_single_node: false,
    pipeline_builder_spark: true,
};

const NO_FOUNDRY_COMPUTE: FoundryCompute = FoundryCompute {
    python_single_node: false,
    python_spark: false,
    pipeline_builder_single_node: false,
    pipeline_builder_spark: false,
};

fn cell(
    read: bool,
    write: bool,
    incremental: bool,
    versioning: bool,
    compute_pushdown: Option<ComputePushdownEngine>,
    snapshot_supported: bool,
    append_only_supported: bool,
    foundry_compute: FoundryCompute,
) -> Capabilities {
    Capabilities {
        read,
        write,
        incremental,
        versioning,
        compute_pushdown,
        snapshot_supported,
        append_only_supported,
        foundry_compute,
    }
}

#[test]
fn bigquery_row() {
    let expected = cell(
        true,
        true,
        true,
        false,
        Some(ComputePushdownEngine::Ibis),
        true,
        true,
        ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
    );
    for tt in [
        TableType::Table,
        TableType::View,
        TableType::MaterializedView,
        TableType::Other,
    ] {
        assert_eq!(
            capabilities_for(SourceProvider::BigQuery, tt),
            expected,
            "BigQuery × {:?} drifted from doc",
            tt
        );
    }
}

#[test]
fn databricks_external_delta_and_managed_iceberg_row() {
    let expected = cell(
        true,
        true,
        true,
        true,
        Some(ComputePushdownEngine::PySpark),
        true,
        true,
        ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
    );
    for tt in [TableType::ExternalDelta, TableType::ManagedIceberg] {
        assert_eq!(
            capabilities_for(SourceProvider::Databricks, tt),
            expected,
            "Databricks × {:?} drifted from doc",
            tt
        );
    }
}

#[test]
fn databricks_managed_delta_row() {
    let caps = capabilities_for(SourceProvider::Databricks, TableType::ManagedDelta);
    assert!(caps.read);
    assert!(!caps.write, "Managed Delta is read-only per Foundry doc");
    assert_eq!(caps.compute_pushdown, Some(ComputePushdownEngine::PySpark));
    assert_eq!(caps.foundry_compute, ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE);
}

#[test]
fn databricks_views_and_other_row() {
    for tt in [
        TableType::View,
        TableType::MaterializedView,
        TableType::Other,
    ] {
        let caps = capabilities_for(SourceProvider::Databricks, tt);
        assert!(caps.read, "{:?} should be readable", tt);
        assert!(!caps.write, "{:?} should be read-only", tt);
        assert_eq!(caps.compute_pushdown, Some(ComputePushdownEngine::PySpark));
        assert_eq!(caps.foundry_compute, ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE);
    }
}

#[test]
fn snowflake_base_row() {
    let expected = cell(
        true,
        true,
        true,
        false,
        Some(ComputePushdownEngine::Snowpark),
        true,
        true,
        ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
    );
    for tt in [
        TableType::Table,
        TableType::View,
        TableType::MaterializedView,
        TableType::Other,
    ] {
        assert_eq!(
            capabilities_for(SourceProvider::Snowflake, tt),
            expected,
            "Snowflake × {:?} drifted from doc",
            tt
        );
    }
}

#[test]
fn snowflake_managed_iceberg_is_spark_only() {
    let caps = capabilities_for(SourceProvider::Snowflake, TableType::ManagedIceberg);
    assert!(caps.read);
    assert!(!caps.write, "Managed Iceberg is read-only per Foundry doc");
    assert_eq!(caps.compute_pushdown, Some(ComputePushdownEngine::Snowpark));
    assert_eq!(caps.foundry_compute, SPARK_ONLY);
}

#[test]
fn object_stores_have_no_foundry_compute_or_pushdown() {
    for provider in [
        SourceProvider::AmazonS3,
        SourceProvider::AzureAbfs,
        SourceProvider::Gcs,
    ] {
        for tt in [
            TableType::ParquetFiles,
            TableType::AvroFiles,
            TableType::CsvFiles,
            TableType::ExternalDelta,
        ] {
            let caps = capabilities_for(provider, tt);
            assert!(caps.read);
            assert!(caps.write);
            assert_eq!(caps.compute_pushdown, None);
            assert_eq!(caps.foundry_compute, NO_FOUNDRY_COMPUTE);
        }
    }
}
