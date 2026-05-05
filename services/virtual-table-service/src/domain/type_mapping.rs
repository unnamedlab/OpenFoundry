//! Type mapping between Arrow and the seven Foundry-supported source
//! systems (BigQuery / Databricks / Snowflake / S3-Parquet / S3-Avro /
//! ADLS / GCS).
//!
//! Two mapping directions are exposed:
//!   1. `arrow_for(provider, source_type)` — what to render the column
//!      as inside Foundry once the virtual table has been registered.
//!      The mapping is best-effort: rare provider-specific types
//!      (BigQuery `GEOGRAPHY`, Snowflake `VARIANT`, Databricks `INTERVAL`,
//!      etc.) fall back to Arrow `Utf8` and emit a warning the
//!      handler attaches to `virtual_tables.properties.warnings`.
//!   2. `provider_for(provider, arrow_type)` — what wire type a
//!      transform should request when materialising a virtual-table
//!      output back into the source system (P6 — compute pushdown).

use serde::{Deserialize, Serialize};

use crate::domain::capability_matrix::SourceProvider;

/// Arrow logical type expressed as a small enum so we don't depend on
/// `arrow-schema` (the workspace already pins arrow-schema = 53; this
/// is a thin façade so callers without the SDK can still reason about
/// the mapping).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ArrowType {
    Boolean,
    Int32,
    Int64,
    Float32,
    Float64,
    Decimal,
    Utf8,
    Binary,
    Date32,
    Timestamp,
    List,
    Struct,
}

/// Output of the mapping: the resolved Arrow type plus an optional
/// warning when the provider type does not have a clean Arrow analogue.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Mapping {
    pub arrow: ArrowType,
    pub warning: Option<String>,
}

impl Mapping {
    fn ok(arrow: ArrowType) -> Self {
        Self {
            arrow,
            warning: None,
        }
    }

    fn fallback(arrow: ArrowType, source_type: &str) -> Self {
        Self {
            arrow,
            warning: Some(format!(
                "type '{source_type}' has no direct Arrow analogue; falling back to {arrow:?}"
            )),
        }
    }
}

/// Resolve a provider-specific source type label (e.g. `STRUCT<...>`,
/// `TIMESTAMP_NTZ`, `VARIANT`) to its Arrow equivalent. Casing is
/// normalised to upper-case before the table lookup.
pub fn arrow_for(provider: SourceProvider, source_type: &str) -> Mapping {
    let head = leading_token(source_type);
    match provider {
        SourceProvider::BigQuery => match head.as_str() {
            "BOOL" | "BOOLEAN" => Mapping::ok(ArrowType::Boolean),
            "INT64" | "INTEGER" | "INT" | "SMALLINT" | "BIGINT" | "TINYINT" | "BYTEINT" => {
                Mapping::ok(ArrowType::Int64)
            }
            "FLOAT64" | "FLOAT" => Mapping::ok(ArrowType::Float64),
            "NUMERIC" | "BIGNUMERIC" | "DECIMAL" => Mapping::ok(ArrowType::Decimal),
            "STRING" | "JSON" => Mapping::ok(ArrowType::Utf8),
            "BYTES" => Mapping::ok(ArrowType::Binary),
            "DATE" => Mapping::ok(ArrowType::Date32),
            "DATETIME" | "TIMESTAMP" | "TIME" => Mapping::ok(ArrowType::Timestamp),
            "ARRAY" => Mapping::ok(ArrowType::List),
            "STRUCT" | "RECORD" => Mapping::ok(ArrowType::Struct),
            "GEOGRAPHY" | "INTERVAL" => Mapping::fallback(ArrowType::Utf8, source_type),
            _ => Mapping::fallback(ArrowType::Utf8, source_type),
        },
        SourceProvider::Snowflake => match head.as_str() {
            "BOOLEAN" => Mapping::ok(ArrowType::Boolean),
            "NUMBER" | "DECIMAL" | "NUMERIC" => Mapping::ok(ArrowType::Decimal),
            "INT" | "INTEGER" | "BIGINT" | "SMALLINT" | "TINYINT" | "BYTEINT" => {
                Mapping::ok(ArrowType::Int64)
            }
            "FLOAT" | "FLOAT4" | "FLOAT8" | "REAL" | "DOUBLE" => Mapping::ok(ArrowType::Float64),
            "VARCHAR" | "CHAR" | "STRING" | "TEXT" => Mapping::ok(ArrowType::Utf8),
            "BINARY" | "VARBINARY" => Mapping::ok(ArrowType::Binary),
            "DATE" => Mapping::ok(ArrowType::Date32),
            "TIMESTAMP" | "TIMESTAMP_LTZ" | "TIMESTAMP_NTZ" | "TIMESTAMP_TZ" | "TIME" => {
                Mapping::ok(ArrowType::Timestamp)
            }
            "ARRAY" => Mapping::ok(ArrowType::List),
            "OBJECT" => Mapping::ok(ArrowType::Struct),
            "VARIANT" | "GEOGRAPHY" | "GEOMETRY" => {
                Mapping::fallback(ArrowType::Utf8, source_type)
            }
            _ => Mapping::fallback(ArrowType::Utf8, source_type),
        },
        SourceProvider::Databricks | SourceProvider::FoundryIceberg => match head.as_str() {
            "BOOLEAN" => Mapping::ok(ArrowType::Boolean),
            "BYTE" | "TINYINT" | "SHORT" | "SMALLINT" | "INT" | "INTEGER" => {
                Mapping::ok(ArrowType::Int32)
            }
            "LONG" | "BIGINT" => Mapping::ok(ArrowType::Int64),
            "FLOAT" => Mapping::ok(ArrowType::Float32),
            "DOUBLE" => Mapping::ok(ArrowType::Float64),
            "DECIMAL" | "NUMERIC" => Mapping::ok(ArrowType::Decimal),
            "STRING" | "VARCHAR" | "CHAR" => Mapping::ok(ArrowType::Utf8),
            "BINARY" => Mapping::ok(ArrowType::Binary),
            "DATE" => Mapping::ok(ArrowType::Date32),
            "TIMESTAMP" | "TIMESTAMP_NTZ" => Mapping::ok(ArrowType::Timestamp),
            "ARRAY" => Mapping::ok(ArrowType::List),
            "MAP" | "STRUCT" => Mapping::ok(ArrowType::Struct),
            "INTERVAL" => Mapping::fallback(ArrowType::Utf8, source_type),
            _ => Mapping::fallback(ArrowType::Utf8, source_type),
        },
        SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs => {
            // Object stores carry Parquet / Avro / CSV; we use the
            // Parquet logical-type vocabulary as the canonical surface.
            match head.as_str() {
                "BOOLEAN" => Mapping::ok(ArrowType::Boolean),
                "INT32" | "INT_8" | "INT_16" | "INT_32" | "UINT_8" | "UINT_16" | "UINT_32" => {
                    Mapping::ok(ArrowType::Int32)
                }
                "INT64" | "INT_64" | "UINT_64" => Mapping::ok(ArrowType::Int64),
                "FLOAT" => Mapping::ok(ArrowType::Float32),
                "DOUBLE" => Mapping::ok(ArrowType::Float64),
                "DECIMAL" => Mapping::ok(ArrowType::Decimal),
                "BYTE_ARRAY" | "FIXED_LEN_BYTE_ARRAY" => Mapping::ok(ArrowType::Binary),
                "UTF8" | "STRING" | "ENUM" | "JSON" => Mapping::ok(ArrowType::Utf8),
                "DATE" => Mapping::ok(ArrowType::Date32),
                "TIMESTAMP_MILLIS" | "TIMESTAMP_MICROS" | "TIMESTAMP_NANOS" => {
                    Mapping::ok(ArrowType::Timestamp)
                }
                "LIST" => Mapping::ok(ArrowType::List),
                "MAP" | "MAP_KEY_VALUE" | "STRUCT" => Mapping::ok(ArrowType::Struct),
                _ => Mapping::fallback(ArrowType::Utf8, source_type),
            }
        }
    }
}

/// Reverse direction — build the canonical wire-type label for a
/// provider given an Arrow logical type. Used when materialising a
/// pipeline output back into the source.
pub fn provider_for(provider: SourceProvider, arrow: ArrowType) -> &'static str {
    match (provider, arrow) {
        // --- BigQuery ---
        (SourceProvider::BigQuery, ArrowType::Boolean) => "BOOL",
        (SourceProvider::BigQuery, ArrowType::Int32 | ArrowType::Int64) => "INT64",
        (SourceProvider::BigQuery, ArrowType::Float32 | ArrowType::Float64) => "FLOAT64",
        (SourceProvider::BigQuery, ArrowType::Decimal) => "NUMERIC",
        (SourceProvider::BigQuery, ArrowType::Utf8) => "STRING",
        (SourceProvider::BigQuery, ArrowType::Binary) => "BYTES",
        (SourceProvider::BigQuery, ArrowType::Date32) => "DATE",
        (SourceProvider::BigQuery, ArrowType::Timestamp) => "TIMESTAMP",
        (SourceProvider::BigQuery, ArrowType::List) => "ARRAY",
        (SourceProvider::BigQuery, ArrowType::Struct) => "STRUCT",
        // --- Snowflake ---
        (SourceProvider::Snowflake, ArrowType::Boolean) => "BOOLEAN",
        (SourceProvider::Snowflake, ArrowType::Int32 | ArrowType::Int64) => "NUMBER",
        (SourceProvider::Snowflake, ArrowType::Float32 | ArrowType::Float64) => "FLOAT",
        (SourceProvider::Snowflake, ArrowType::Decimal) => "NUMBER",
        (SourceProvider::Snowflake, ArrowType::Utf8) => "VARCHAR",
        (SourceProvider::Snowflake, ArrowType::Binary) => "BINARY",
        (SourceProvider::Snowflake, ArrowType::Date32) => "DATE",
        (SourceProvider::Snowflake, ArrowType::Timestamp) => "TIMESTAMP_NTZ",
        (SourceProvider::Snowflake, ArrowType::List) => "ARRAY",
        (SourceProvider::Snowflake, ArrowType::Struct) => "OBJECT",
        // --- Databricks / Foundry Iceberg (Spark types) ---
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Boolean,
        ) => "BOOLEAN",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Int32,
        ) => "INT",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Int64,
        ) => "BIGINT",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Float32,
        ) => "FLOAT",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Float64,
        ) => "DOUBLE",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Decimal,
        ) => "DECIMAL",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Utf8,
        ) => "STRING",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Binary,
        ) => "BINARY",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Date32,
        ) => "DATE",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Timestamp,
        ) => "TIMESTAMP",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::List,
        ) => "ARRAY",
        (
            SourceProvider::Databricks | SourceProvider::FoundryIceberg,
            ArrowType::Struct,
        ) => "STRUCT",
        // --- Object stores (Parquet logical types) ---
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Boolean,
        ) => "BOOLEAN",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Int32,
        ) => "INT32",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Int64,
        ) => "INT64",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Float32,
        ) => "FLOAT",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Float64,
        ) => "DOUBLE",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Decimal,
        ) => "DECIMAL",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Utf8,
        ) => "UTF8",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Binary,
        ) => "BYTE_ARRAY",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Date32,
        ) => "DATE",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Timestamp,
        ) => "TIMESTAMP_MICROS",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::List,
        ) => "LIST",
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            ArrowType::Struct,
        ) => "STRUCT",
    }
}

fn leading_token(source_type: &str) -> String {
    let trimmed = source_type.trim();
    let head = trimmed
        .split(['<', '(', ' ', '\t'])
        .next()
        .unwrap_or(trimmed);
    head.to_ascii_uppercase()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn bigquery_geography_falls_back_with_warning() {
        let m = arrow_for(SourceProvider::BigQuery, "GEOGRAPHY");
        assert_eq!(m.arrow, ArrowType::Utf8);
        assert!(m.warning.is_some());
    }

    #[test]
    fn snowflake_variant_falls_back_with_warning() {
        let m = arrow_for(SourceProvider::Snowflake, "VARIANT");
        assert_eq!(m.arrow, ArrowType::Utf8);
        assert!(m.warning.is_some());
    }

    #[test]
    fn databricks_int_maps_to_int32() {
        let m = arrow_for(SourceProvider::Databricks, "INT");
        assert_eq!(m.arrow, ArrowType::Int32);
        assert!(m.warning.is_none());
    }

    #[test]
    fn snowflake_round_trip_for_non_numeric_types() {
        // Snowflake collapses every integer / decimal width into a
        // single `NUMBER` type, so `Int64` widens to `Decimal` after
        // a round-trip. Numeric types are covered by
        // [`snowflake_int64_widens_to_decimal_through_number`].
        for arrow in [
            ArrowType::Boolean,
            ArrowType::Float64,
            ArrowType::Utf8,
            ArrowType::Date32,
            ArrowType::Timestamp,
        ] {
            let provider_label = provider_for(SourceProvider::Snowflake, arrow);
            let back = arrow_for(SourceProvider::Snowflake, provider_label);
            assert_eq!(back.arrow, arrow, "{:?} did not round-trip", arrow);
        }
    }

    #[test]
    fn snowflake_int64_widens_to_decimal_through_number() {
        let label = provider_for(SourceProvider::Snowflake, ArrowType::Int64);
        assert_eq!(label, "NUMBER");
        assert_eq!(
            arrow_for(SourceProvider::Snowflake, label).arrow,
            ArrowType::Decimal
        );
    }

    #[test]
    fn parquet_int_64_token_strips_logical_args() {
        let m = arrow_for(SourceProvider::AmazonS3, "INT64 (TIMESTAMP_MILLIS)");
        // Leading-token parser strips at the first whitespace, so we
        // see INT64 before the logical annotation.
        assert_eq!(m.arrow, ArrowType::Int64);
    }
}
