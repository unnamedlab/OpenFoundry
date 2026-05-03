use std::{path::PathBuf, str::from_utf8};

use arrow::util::display::array_value_to_string;
use datafusion::prelude::NdJsonReadOptions;
use query_engine::context::QueryContext;
use serde_json::{Value, json};
use tokio::fs;
use uuid::Uuid;

use crate::{
    AppState,
    models::{
        branch::DatasetBranch, dataset::Dataset, schema::SchemaField, version::DatasetVersion,
    },
};

const DATASET_BRANCH_BY_NAME_PROJECTION_SQL: &str =
    "SELECT * FROM dataset_branches WHERE dataset_id = $1 AND name = $2";
const DATASET_VERSION_BY_NUMBER_PROJECTION_SQL: &str =
    "SELECT * FROM dataset_versions WHERE dataset_id = $1 AND version = $2";
const DATASET_VERSIONS_PROJECTION_SQL: &str =
    "SELECT * FROM dataset_versions WHERE dataset_id = $1 ORDER BY version DESC";
const DATASET_BRANCHES_PROJECTION_SQL: &str =
    "SELECT * FROM dataset_branches WHERE dataset_id = $1 ORDER BY is_default DESC, name ASC";

pub struct PreparedDatasetQuery {
    pub ctx: QueryContext,
    pub path: PathBuf,
}

#[derive(Debug, Clone)]
pub struct ResolvedDatasetSource {
    pub dataset: Dataset,
    pub branch: Option<String>,
    pub version: i32,
    pub size_bytes: i64,
    pub storage_path: String,
}

pub enum DatasetSourceError {
    Invalid(String),
    Database(String),
}

pub async fn list_dataset_versions(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<DatasetVersion>, DatasetSourceError> {
    sqlx::query_as::<_, DatasetVersion>(DATASET_VERSIONS_PROJECTION_SQL)
        .bind(dataset_id)
        .fetch_all(&state.db)
        .await
        .map_err(|error| DatasetSourceError::Database(error.to_string()))
}

pub async fn list_dataset_branches(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<DatasetBranch>, DatasetSourceError> {
    sqlx::query_as::<_, DatasetBranch>(DATASET_BRANCHES_PROJECTION_SQL)
        .bind(dataset_id)
        .fetch_all(&state.db)
        .await
        .map_err(|error| DatasetSourceError::Database(error.to_string()))
}

pub fn versioning_runtime_write_conflict(dataset_id: Uuid) -> Value {
    json!({
        "error": "dataset runtime writes moved to dataset-versioning-service",
        "dataset_id": dataset_id,
        "owner": "dataset-versioning-service",
        "details": {
            "dataset_versions": "owned by dataset-versioning-service",
            "dataset_branches": "owned by dataset-versioning-service",
            "dataset_transactions": "owned by dataset-versioning-service",
            "snapshots_and_data_state": "Iceberg is mandatory; Postgres remains declarative metadata only"
        }
    })
}

pub async fn resolve_dataset_source(
    state: &AppState,
    dataset_id: Uuid,
    branch: Option<&str>,
    version: Option<i32>,
) -> Result<Option<ResolvedDatasetSource>, DatasetSourceError> {
    let dataset = sqlx::query_as::<_, Dataset>("SELECT * FROM datasets WHERE id = $1")
        .bind(dataset_id)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| DatasetSourceError::Database(error.to_string()))?;

    let Some(dataset) = dataset else {
        return Ok(None);
    };

    if let Some(version) = version
        && version < 1
    {
        return Err(DatasetSourceError::Invalid(
            "version must be greater than zero".to_string(),
        ));
    }

    let branch = branch.map(str::trim).filter(|value| !value.is_empty());

    // Read-only projection bridge: branch/version resolution still consults
    // the catalog copy, but runtime ownership lives in
    // `dataset-versioning-service`.
    let branch_record = if let Some(branch_name) = branch {
        load_branch_projection(state, dataset_id, branch_name).await?
    } else {
        None
    };

    if branch.is_some() && branch_record.is_none() {
        return Ok(None);
    }

    let version = version
        .or_else(|| branch_record.as_ref().map(|record| record.version))
        .unwrap_or(dataset.current_version);

    let version_record = load_version_projection(state, dataset_id, version).await?;

    let (storage_path, size_bytes) = if version == dataset.current_version {
        (
            format!("{}/v{}", dataset.storage_path, dataset.current_version),
            dataset.size_bytes,
        )
    } else if let Some(version_record) = version_record {
        (version_record.storage_path, version_record.size_bytes)
    } else {
        return Ok(None);
    };

    Ok(Some(ResolvedDatasetSource {
        dataset,
        branch: branch_record.map(|record| record.name),
        version,
        size_bytes,
        storage_path,
    }))
}

async fn load_branch_projection(
    state: &AppState,
    dataset_id: Uuid,
    branch_name: &str,
) -> Result<Option<DatasetBranch>, DatasetSourceError> {
    sqlx::query_as::<_, DatasetBranch>(DATASET_BRANCH_BY_NAME_PROJECTION_SQL)
        .bind(dataset_id)
        .bind(branch_name)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| DatasetSourceError::Database(error.to_string()))
}

async fn load_version_projection(
    state: &AppState,
    dataset_id: Uuid,
    version: i32,
) -> Result<Option<DatasetVersion>, DatasetSourceError> {
    sqlx::query_as::<_, DatasetVersion>(DATASET_VERSION_BY_NUMBER_PROJECTION_SQL)
        .bind(dataset_id)
        .bind(version)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| DatasetSourceError::Database(error.to_string()))
}

pub async fn prepare_query_context(
    format: &str,
    data: &[u8],
) -> Result<PreparedDatasetQuery, String> {
    let extension = match format {
        "csv" => "csv",
        "json" => "json",
        _ => "parquet",
    };
    let path = std::env::temp_dir().join(format!(
        "openfoundry-runtime-{}.{}",
        Uuid::now_v7(),
        extension
    ));
    let bytes = if format == "json" {
        normalize_json_bytes(data)?
    } else {
        data.to_vec()
    };

    fs::write(&path, bytes)
        .await
        .map_err(|error| error.to_string())?;

    let ctx = QueryContext::new();
    let file_path = path.to_string_lossy().to_string();
    match format {
        "csv" => ctx
            .register_csv("dataset", &file_path)
            .await
            .map_err(|error| error.to_string())?,
        "json" => ctx
            .inner()
            .register_json("dataset", &file_path, NdJsonReadOptions::default())
            .await
            .map_err(|error| error.to_string())?,
        _ => ctx
            .register_parquet("dataset", &file_path)
            .await
            .map_err(|error| error.to_string())?,
    }

    Ok(PreparedDatasetQuery { ctx, path })
}

pub async fn load_schema_fields(ctx: &QueryContext) -> Result<Vec<SchemaField>, String> {
    let dataframe = ctx
        .sql("SELECT * FROM dataset LIMIT 1")
        .await
        .map_err(|error| error.to_string())?;

    Ok(dataframe
        .schema()
        .fields()
        .iter()
        .map(|field| SchemaField {
            name: field.name().to_string(),
            field_type: field.data_type().to_string(),
            nullable: field.is_nullable(),
        })
        .collect())
}

pub async fn load_schema_fields_for_query(
    ctx: &QueryContext,
    sql: &str,
) -> Result<Vec<SchemaField>, String> {
    let dataframe = ctx.sql(sql).await.map_err(|error| error.to_string())?;
    Ok(dataframe
        .schema()
        .fields()
        .iter()
        .map(|field| SchemaField {
            name: field.name().to_string(),
            field_type: field.data_type().to_string(),
            nullable: field.is_nullable(),
        })
        .collect())
}

pub async fn collect_object_rows(ctx: &QueryContext, sql: &str) -> Result<Vec<Value>, String> {
    let batches = ctx
        .execute_sql(sql)
        .await
        .map_err(|error| error.to_string())?;
    let mut rows = Vec::new();

    for batch in batches {
        let field_names = batch
            .schema()
            .fields()
            .iter()
            .map(|field| field.name().to_string())
            .collect::<Vec<_>>();
        for row_index in 0..batch.num_rows() {
            let mut row = serde_json::Map::new();
            for (column_index, field_name) in field_names.iter().enumerate() {
                let raw = array_value_to_string(batch.column(column_index), row_index)
                    .unwrap_or_else(|_| "null".to_string());
                row.insert(field_name.clone(), json_scalar_or_string(&raw));
            }
            rows.push(Value::Object(row));
        }
    }

    Ok(rows)
}

pub async fn fetch_scalar_i64(ctx: &QueryContext, sql: &str) -> Result<i64, String> {
    let rows = collect_object_rows(ctx, sql).await?;
    Ok(rows
        .first()
        .and_then(|row| row.as_object())
        .and_then(|row| row.values().next())
        .and_then(|value| {
            value
                .as_i64()
                .or_else(|| value.as_str()?.parse::<i64>().ok())
        })
        .unwrap_or(0))
}

pub async fn cleanup_temp_path(path: PathBuf) {
    let _ = fs::remove_file(path).await;
}

pub fn normalize_json_bytes(data: &[u8]) -> Result<Vec<u8>, String> {
    let text = from_utf8(data).map_err(|error| error.to_string())?;
    let trimmed = text.trim();
    if trimmed.is_empty() {
        return Ok(Vec::new());
    }

    if trimmed.starts_with('[') || trimmed.starts_with('{') {
        let parsed: Value = serde_json::from_slice(data).map_err(|error| error.to_string())?;
        let mut lines = String::new();
        match parsed {
            Value::Array(rows) => {
                for row in rows {
                    lines
                        .push_str(&serde_json::to_string(&row).map_err(|error| error.to_string())?);
                    lines.push('\n');
                }
            }
            Value::Object(_) => {
                lines.push_str(&serde_json::to_string(&parsed).map_err(|error| error.to_string())?);
                lines.push('\n');
            }
            _ => return Err("JSON uploads must contain objects or arrays of objects".to_string()),
        }
        return Ok(lines.into_bytes());
    }

    Ok(data.to_vec())
}

pub fn json_scalar_or_string(raw: &str) -> Value {
    if raw == "null" {
        Value::Null
    } else {
        serde_json::from_str(raw).unwrap_or_else(|_| Value::String(raw.to_string()))
    }
}

pub fn wrap_query(sql: &str) -> String {
    format!("SELECT * FROM ({sql}) AS dataset_view")
}

pub fn count_query(sql: &str) -> String {
    format!("SELECT COUNT(*) AS value FROM ({sql}) AS dataset_view")
}

pub fn paged_query(sql: &str, limit: i64, offset: i64) -> String {
    format!("SELECT * FROM ({sql}) AS dataset_view LIMIT {limit} OFFSET {offset}")
}

pub fn json_bytes(rows: &[Value]) -> Result<Vec<u8>, String> {
    serde_json::to_vec(rows).map_err(|error| error.to_string())
}

pub fn schema_to_value(fields: &[SchemaField]) -> Result<Value, String> {
    serde_json::to_value(fields).map_err(|error| error.to_string())
}

pub fn preview_payload(
    dataset_id: Uuid,
    branch: Option<String>,
    version: i32,
    format: &str,
    size_bytes: i64,
    storage_path: String,
    limit: i64,
    offset: i64,
    total_rows: i64,
    columns: Vec<SchemaField>,
    rows: Vec<Value>,
    warnings: Vec<String>,
    errors: Vec<String>,
) -> Value {
    json!({
        "dataset_id": dataset_id,
        "branch": branch,
        "version": version,
        "format": format,
        "size_bytes": size_bytes,
        "storage_path": storage_path,
        "limit": limit,
        "offset": offset,
        "total_rows": total_rows,
        "row_count": rows.len(),
        "columns": columns,
        "rows": rows,
        "warnings": warnings,
        "errors": errors,
    })
}

#[cfg(test)]
mod tests {
    use serde_json::json;
    use uuid::Uuid;

    use super::{
        DATASET_BRANCH_BY_NAME_PROJECTION_SQL, DATASET_BRANCHES_PROJECTION_SQL,
        DATASET_VERSION_BY_NUMBER_PROJECTION_SQL, DATASET_VERSIONS_PROJECTION_SQL, count_query,
        json_scalar_or_string, paged_query, versioning_runtime_write_conflict, wrap_query,
    };

    #[test]
    fn parses_json_scalars_when_possible() {
        assert_eq!(json_scalar_or_string("12"), json!(12));
        assert_eq!(json_scalar_or_string("true"), json!(true));
        assert_eq!(json_scalar_or_string("ready"), json!("ready"));
        assert_eq!(json_scalar_or_string("null"), json!(null));
    }

    #[test]
    fn wraps_queries_for_views() {
        let sql = "SELECT * FROM dataset WHERE amount > 10";
        assert_eq!(
            wrap_query(sql),
            "SELECT * FROM (SELECT * FROM dataset WHERE amount > 10) AS dataset_view"
        );
        assert_eq!(
            count_query(sql),
            "SELECT COUNT(*) AS value FROM (SELECT * FROM dataset WHERE amount > 10) AS dataset_view"
        );
        assert_eq!(
            paged_query(sql, 25, 10),
            "SELECT * FROM (SELECT * FROM dataset WHERE amount > 10) AS dataset_view LIMIT 25 OFFSET 10"
        );
    }

    #[test]
    fn versioning_projection_queries_are_read_only() {
        for sql in [
            DATASET_BRANCH_BY_NAME_PROJECTION_SQL,
            DATASET_VERSION_BY_NUMBER_PROJECTION_SQL,
            DATASET_VERSIONS_PROJECTION_SQL,
            DATASET_BRANCHES_PROJECTION_SQL,
        ] {
            let upper = sql.to_ascii_uppercase();
            assert!(upper.starts_with("SELECT "));
            assert!(!upper.contains(" INSERT "));
            assert!(!upper.contains(" UPDATE "));
            assert!(!upper.contains(" DELETE "));
        }
    }

    #[test]
    fn runtime_write_conflict_names_versioning_owner() {
        let payload = versioning_runtime_write_conflict(Uuid::nil());

        assert_eq!(
            payload["error"],
            json!("dataset runtime writes moved to dataset-versioning-service")
        );
        assert_eq!(payload["owner"], json!("dataset-versioning-service"));
        assert_eq!(
            payload["details"]["dataset_transactions"],
            json!("owned by dataset-versioning-service")
        );
    }
}
