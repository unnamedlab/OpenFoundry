//! Capability matrix for virtual tables, broken down by source × table type.
//!
//! Source of truth: Foundry doc § "Virtual table compatibility matrix by
//! source & table type" inside
//! `Data connectivity & integration/Core concepts/Virtual tables.md`.
//!
//! Every cell of the matrix is exhaustively reproduced in [`MATRIX`] so
//! [`capabilities_for`] can answer at registration time whether a virtual
//! table backs read / write / compute pushdown / Pipeline-Builder Spark /
//! single-node compute. The accompanying integration test
//! `tests/capability_matrix_matches_doc.rs` walks every row of the doc
//! verbatim, so silent drift between this table and the published Foundry
//! contract is impossible.

use serde::{Deserialize, Serialize};

/// Source provider, aligned 1:1 with the `provider` CHECK constraint on
/// `virtual_table_sources_link`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum SourceProvider {
    AmazonS3,
    AzureAbfs,
    BigQuery,
    Databricks,
    FoundryIceberg,
    Gcs,
    Snowflake,
}

impl SourceProvider {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::AmazonS3 => "AMAZON_S3",
            Self::AzureAbfs => "AZURE_ABFS",
            Self::BigQuery => "BIGQUERY",
            Self::Databricks => "DATABRICKS",
            Self::FoundryIceberg => "FOUNDRY_ICEBERG",
            Self::Gcs => "GCS",
            Self::Snowflake => "SNOWFLAKE",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "AMAZON_S3" => Some(Self::AmazonS3),
            "AZURE_ABFS" => Some(Self::AzureAbfs),
            "BIGQUERY" => Some(Self::BigQuery),
            "DATABRICKS" => Some(Self::Databricks),
            "FOUNDRY_ICEBERG" => Some(Self::FoundryIceberg),
            "GCS" => Some(Self::Gcs),
            "SNOWFLAKE" => Some(Self::Snowflake),
            _ => None,
        }
    }
}

/// Table type slot, aligned with the `table_type` CHECK constraint on
/// `virtual_tables`.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TableType {
    Table,
    View,
    MaterializedView,
    ExternalDelta,
    ManagedDelta,
    ManagedIceberg,
    ParquetFiles,
    AvroFiles,
    CsvFiles,
    Other,
}

impl TableType {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Table => "TABLE",
            Self::View => "VIEW",
            Self::MaterializedView => "MATERIALIZED_VIEW",
            Self::ExternalDelta => "EXTERNAL_DELTA",
            Self::ManagedDelta => "MANAGED_DELTA",
            Self::ManagedIceberg => "MANAGED_ICEBERG",
            Self::ParquetFiles => "PARQUET_FILES",
            Self::AvroFiles => "AVRO_FILES",
            Self::CsvFiles => "CSV_FILES",
            Self::Other => "OTHER",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "TABLE" => Some(Self::Table),
            "VIEW" => Some(Self::View),
            "MATERIALIZED_VIEW" => Some(Self::MaterializedView),
            "EXTERNAL_DELTA" => Some(Self::ExternalDelta),
            "MANAGED_DELTA" => Some(Self::ManagedDelta),
            "MANAGED_ICEBERG" => Some(Self::ManagedIceberg),
            "PARQUET_FILES" => Some(Self::ParquetFiles),
            "AVRO_FILES" => Some(Self::AvroFiles),
            "CSV_FILES" => Some(Self::CsvFiles),
            "OTHER" => Some(Self::Other),
            _ => None,
        }
    }
}

/// Native compute engine that the source can run on Foundry's behalf
/// when "compute pushdown" is selected. `None` means the doc lists the
/// cell as N/A or ❌.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ComputePushdownEngine {
    Ibis,
    PySpark,
    Snowpark,
}

impl ComputePushdownEngine {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Ibis => "ibis",
            Self::PySpark => "pyspark",
            Self::Snowpark => "snowpark",
        }
    }
}

/// Where Foundry can run **its** compute against the table. Mirrors the
/// "Foundry compute" column of the doc matrix.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct FoundryCompute {
    pub python_single_node: bool,
    pub python_spark: bool,
    pub pipeline_builder_single_node: bool,
    pub pipeline_builder_spark: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct Capabilities {
    pub read: bool,
    pub write: bool,
    pub incremental: bool,
    pub versioning: bool,
    pub compute_pushdown: Option<ComputePushdownEngine>,
    pub snapshot_supported: bool,
    pub append_only_supported: bool,
    pub foundry_compute: FoundryCompute,
}

/// Single row of the published Foundry compatibility matrix.
struct MatrixRow {
    provider: SourceProvider,
    table_types: &'static [TableType],
    capabilities: Capabilities,
}

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

/// Exhaustive matrix lifted verbatim from the Foundry doc. Every fact
/// asserted by the published matrix has exactly one row in this array.
/// `tests/capability_matrix_matches_doc.rs` walks both sides.
const MATRIX: &[MatrixRow] = &[
    // BigQuery — all table types share the same row.
    MatrixRow {
        provider: SourceProvider::BigQuery,
        table_types: &[
            TableType::Table,
            TableType::View,
            TableType::MaterializedView,
            TableType::Other,
        ],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: true,
            versioning: false,
            compute_pushdown: Some(ComputePushdownEngine::Ibis),
            snapshot_supported: true,
            append_only_supported: true,
            foundry_compute: ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
        },
    },
    // Databricks — External Delta and Managed Iceberg are read+write.
    MatrixRow {
        provider: SourceProvider::Databricks,
        table_types: &[TableType::ExternalDelta, TableType::ManagedIceberg],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: true,
            versioning: true,
            compute_pushdown: Some(ComputePushdownEngine::PySpark),
            snapshot_supported: true,
            append_only_supported: true,
            foundry_compute: ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
        },
    },
    // Databricks — Managed Delta is read-only.
    MatrixRow {
        provider: SourceProvider::Databricks,
        table_types: &[TableType::ManagedDelta],
        capabilities: Capabilities {
            read: true,
            write: false,
            incremental: true,
            versioning: true,
            compute_pushdown: Some(ComputePushdownEngine::PySpark),
            snapshot_supported: true,
            append_only_supported: true,
            foundry_compute: ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
        },
    },
    // Databricks — Views, Materialized Views, Other are read-only.
    MatrixRow {
        provider: SourceProvider::Databricks,
        table_types: &[
            TableType::View,
            TableType::MaterializedView,
            TableType::Other,
        ],
        capabilities: Capabilities {
            read: true,
            write: false,
            incremental: false,
            versioning: false,
            compute_pushdown: Some(ComputePushdownEngine::PySpark),
            snapshot_supported: true,
            append_only_supported: false,
            foundry_compute: ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
        },
    },
    // Snowflake — base tables are read+write with Snowpark pushdown.
    MatrixRow {
        provider: SourceProvider::Snowflake,
        table_types: &[
            TableType::Table,
            TableType::View,
            TableType::MaterializedView,
            TableType::Other,
        ],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: true,
            versioning: false,
            compute_pushdown: Some(ComputePushdownEngine::Snowpark),
            snapshot_supported: true,
            append_only_supported: true,
            foundry_compute: ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
        },
    },
    // Snowflake — Managed Iceberg is read-only and Spark-only on the
    // Foundry-compute side.
    MatrixRow {
        provider: SourceProvider::Snowflake,
        table_types: &[TableType::ManagedIceberg],
        capabilities: Capabilities {
            read: true,
            write: false,
            incremental: true,
            versioning: true,
            compute_pushdown: Some(ComputePushdownEngine::Snowpark),
            snapshot_supported: true,
            append_only_supported: true,
            foundry_compute: SPARK_ONLY,
        },
    },
    // Object stores: AWS S3, Azure ADLS, GCS — Parquet, Avro, CSV, Delta.
    MatrixRow {
        provider: SourceProvider::AmazonS3,
        table_types: &[
            TableType::ParquetFiles,
            TableType::AvroFiles,
            TableType::CsvFiles,
            TableType::ExternalDelta,
        ],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: false,
            versioning: false,
            compute_pushdown: None,
            snapshot_supported: false,
            append_only_supported: false,
            foundry_compute: NO_FOUNDRY_COMPUTE,
        },
    },
    MatrixRow {
        provider: SourceProvider::AzureAbfs,
        table_types: &[
            TableType::ParquetFiles,
            TableType::AvroFiles,
            TableType::CsvFiles,
            TableType::ExternalDelta,
        ],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: false,
            versioning: false,
            compute_pushdown: None,
            snapshot_supported: false,
            append_only_supported: false,
            foundry_compute: NO_FOUNDRY_COMPUTE,
        },
    },
    MatrixRow {
        provider: SourceProvider::Gcs,
        table_types: &[
            TableType::ParquetFiles,
            TableType::AvroFiles,
            TableType::CsvFiles,
            TableType::ExternalDelta,
        ],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: false,
            versioning: false,
            compute_pushdown: None,
            snapshot_supported: false,
            append_only_supported: false,
            foundry_compute: NO_FOUNDRY_COMPUTE,
        },
    },
    // Foundry source — managed Iceberg, beta. Treated as read+write
    // versioned, no compute pushdown (Foundry runs the engine).
    MatrixRow {
        provider: SourceProvider::FoundryIceberg,
        table_types: &[TableType::ManagedIceberg],
        capabilities: Capabilities {
            read: true,
            write: true,
            incremental: true,
            versioning: true,
            compute_pushdown: None,
            snapshot_supported: true,
            append_only_supported: true,
            foundry_compute: ALL_FOUNDRY_COMPUTE_EXCEPT_PB_SINGLE,
        },
    },
];

/// Default fallback for sources or table types not listed in the doc.
/// We default to a permissive read-only with no Foundry compute so the
/// matrix never silently grants write or pushdown capabilities to a
/// cell the doc does not bless.
pub const UNKNOWN: Capabilities = Capabilities {
    read: true,
    write: false,
    incremental: false,
    versioning: false,
    compute_pushdown: None,
    snapshot_supported: false,
    append_only_supported: false,
    foundry_compute: NO_FOUNDRY_COMPUTE,
};

/// Look up the cell of the matrix that applies to the
/// `(provider, table_type)` pair. Returns [`UNKNOWN`] for combinations
/// the Foundry doc does not enumerate (most call-sites should treat
/// that as "register, but warn the user").
pub fn capabilities_for(provider: SourceProvider, table_type: TableType) -> Capabilities {
    for row in MATRIX {
        if row.provider == provider && row.table_types.contains(&table_type) {
            return row.capabilities;
        }
    }
    UNKNOWN
}

/// Iterate every (provider, table_type, capabilities) triple that the
/// matrix asserts. Used by the doc-conformance integration test.
pub fn iter_cells() -> impl Iterator<Item = (SourceProvider, TableType, Capabilities)> {
    MATRIX
        .iter()
        .flat_map(|row| {
            row.table_types
                .iter()
                .map(move |&tt| (row.provider, tt, row.capabilities))
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bigquery_table_matches_doc_cell() {
        let caps = capabilities_for(SourceProvider::BigQuery, TableType::Table);
        assert!(caps.read && caps.write);
        assert_eq!(caps.compute_pushdown, Some(ComputePushdownEngine::Ibis));
        assert!(caps.foundry_compute.python_spark);
        assert!(!caps.foundry_compute.pipeline_builder_single_node);
    }

    #[test]
    fn databricks_managed_delta_is_read_only() {
        let caps = capabilities_for(SourceProvider::Databricks, TableType::ManagedDelta);
        assert!(caps.read);
        assert!(!caps.write, "doc lists Managed Delta as read-only");
        assert_eq!(caps.compute_pushdown, Some(ComputePushdownEngine::PySpark));
    }

    #[test]
    fn snowflake_managed_iceberg_is_spark_only() {
        let caps = capabilities_for(SourceProvider::Snowflake, TableType::ManagedIceberg);
        assert!(caps.read);
        assert!(!caps.write);
        assert!(!caps.foundry_compute.python_single_node);
        assert!(caps.foundry_compute.python_spark);
    }

    #[test]
    fn s3_parquet_has_no_pushdown_no_foundry_compute() {
        let caps = capabilities_for(SourceProvider::AmazonS3, TableType::ParquetFiles);
        assert!(caps.read && caps.write);
        assert!(caps.compute_pushdown.is_none());
        assert_eq!(caps.foundry_compute, NO_FOUNDRY_COMPUTE);
    }

    #[test]
    fn unknown_pair_defaults_to_read_only() {
        let caps = capabilities_for(SourceProvider::FoundryIceberg, TableType::CsvFiles);
        assert!(caps.read);
        assert!(!caps.write);
    }

    #[test]
    fn provider_round_trips_through_str() {
        for provider in [
            SourceProvider::AmazonS3,
            SourceProvider::AzureAbfs,
            SourceProvider::BigQuery,
            SourceProvider::Databricks,
            SourceProvider::FoundryIceberg,
            SourceProvider::Gcs,
            SourceProvider::Snowflake,
        ] {
            assert_eq!(SourceProvider::parse(provider.as_str()), Some(provider));
        }
    }
}
