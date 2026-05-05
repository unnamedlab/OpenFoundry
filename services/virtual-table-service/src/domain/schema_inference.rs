//! Schema inference for virtual tables.

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::domain::capability_matrix::SourceProvider;
use crate::models::virtual_table::Locator;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct InferredColumn {
    pub name: String,
    pub source_type: String,
    pub inferred_type: String,
    pub nullable: bool,
}

#[allow(dead_code)]
pub fn infer_from_json_sample(sample: &[Value]) -> Vec<InferredColumn> {
    let Some(first) = sample.first() else {
        return vec![];
    };
    let Value::Object(obj) = first else {
        return vec![];
    };
    obj.iter()
        .map(|(key, val)| {
            let inferred_type = match val {
                Value::Bool(_) => "boolean",
                Value::Number(n) if n.is_f64() => "float64",
                Value::Number(_) => "int64",
                Value::String(_) => "string",
                Value::Array(_) => "list",
                Value::Object(_) => "struct",
                Value::Null => "string",
            };
            InferredColumn {
                name: key.clone(),
                source_type: "json".to_string(),
                inferred_type: inferred_type.to_string(),
                nullable: sample
                    .iter()
                    .any(|row| row.get(key).is_none_or(|v| v.is_null())),
            }
        })
        .collect()
}

pub async fn infer_for_provider(
    provider: SourceProvider,
    locator: &Locator,
) -> Result<Vec<InferredColumn>, String> {
    let columns = match (provider, locator) {
        (
            SourceProvider::BigQuery
            | SourceProvider::Snowflake
            | SourceProvider::Databricks,
            Locator::Tabular { .. },
        ) => warehouse_stub_schema(),
        (
            SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs,
            Locator::File { format, .. },
        ) => file_stub_schema(format),
        (_, Locator::Iceberg { .. }) | (SourceProvider::FoundryIceberg, _) => {
            iceberg_stub_schema()
        }
        _ => vec![],
    };
    Ok(columns)
}

fn warehouse_stub_schema() -> Vec<InferredColumn> {
    vec![
        InferredColumn {
            name: "id".into(),
            source_type: "BIGINT".into(),
            inferred_type: "int64".into(),
            nullable: false,
        },
        InferredColumn {
            name: "created_at".into(),
            source_type: "TIMESTAMP".into(),
            inferred_type: "timestamp".into(),
            nullable: true,
        },
        InferredColumn {
            name: "payload".into(),
            source_type: "STRING".into(),
            inferred_type: "string".into(),
            nullable: true,
        },
    ]
}

fn iceberg_stub_schema() -> Vec<InferredColumn> {
    vec![
        InferredColumn {
            name: "_iceberg_seq".into(),
            source_type: "BIGINT".into(),
            inferred_type: "int64".into(),
            nullable: false,
        },
        InferredColumn {
            name: "data".into(),
            source_type: "STRING".into(),
            inferred_type: "string".into(),
            nullable: true,
        },
    ]
}

fn file_stub_schema(format: &str) -> Vec<InferredColumn> {
    let normalized = format.trim().to_lowercase();
    match normalized.as_str() {
        "parquet" | "avro" => vec![
            InferredColumn {
                name: "key".into(),
                source_type: normalized.to_uppercase(),
                inferred_type: "string".into(),
                nullable: false,
            },
            InferredColumn {
                name: "value".into(),
                source_type: normalized.to_uppercase(),
                inferred_type: "binary".into(),
                nullable: true,
            },
        ],
        "csv" => vec![InferredColumn {
            name: "row".into(),
            source_type: "CSV".into(),
            inferred_type: "string".into(),
            nullable: false,
        }],
        _ => vec![],
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn warehouse_stub_returns_three_columns() {
        let cols = infer_for_provider(
            SourceProvider::BigQuery,
            &Locator::Tabular {
                database: "main".into(),
                schema: "public".into(),
                table: "orders".into(),
            },
        )
        .await
        .expect("infer");
        assert_eq!(cols.len(), 3);
        assert_eq!(cols[0].name, "id");
    }

    #[tokio::test]
    async fn parquet_file_stub_has_key_value_pair() {
        let cols = infer_for_provider(
            SourceProvider::AmazonS3,
            &Locator::File {
                bucket: "b".into(),
                prefix: "p".into(),
                format: "PARQUET".into(),
            },
        )
        .await
        .expect("infer");
        assert_eq!(cols.len(), 2);
    }
}
