//! T6.3 — POST /v1/datasets/{rid}/schema:validate
//!
//! Loads the current view (head version on the active branch) of a
//! dataset, infers the actual column types from the underlying file,
//! and compares them against the user-supplied [`Schema`] proposal.
//! Errors are reported per file (today: one entry, since the catalog
//! materialises one object per version).

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use core_models::dataset::{FieldType, Schema, SchemaField as ProposedField};
use serde::{Deserialize, Serialize};
use serde_json::json;
use uuid::Uuid;

use crate::{AppState, domain::runtime, models::schema::SchemaField as ActualField};

/// Body of `POST /v1/datasets/{rid}/schema:validate`.
#[derive(Debug, Deserialize)]
pub struct ValidateRequest {
    pub schema: Schema,
}

#[derive(Debug, Serialize)]
pub struct FileValidationReport {
    pub path: String,
    pub format: String,
    pub size_bytes: i64,
    pub conforms: bool,
    pub errors: Vec<FileSchemaError>,
}

#[derive(Debug, Serialize)]
pub struct FileSchemaError {
    pub field: String,
    pub kind: String,
    pub message: String,
}

#[derive(Debug, Serialize)]
pub struct ValidateResponse {
    pub conforms: bool,
    pub files: Vec<FileValidationReport>,
    /// Errors raised by validating the proposal itself before
    /// touching the storage (e.g. illegal DECIMAL precision).
    pub schema_errors: Vec<String>,
}

/// `POST /v1/datasets/:id/schema:validate`
pub async fn validate_schema(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<ValidateRequest>,
) -> impl IntoResponse {
    // 1. Validate the proposal itself.
    if let Err(error) = body.schema.validate() {
        return (
            StatusCode::BAD_REQUEST,
            Json(ValidateResponse {
                conforms: false,
                files: vec![],
                schema_errors: vec![error.to_string()],
            }),
        )
            .into_response();
    }

    // 2. Resolve the head version on the active branch.
    let source = match runtime::resolve_dataset_source(&state, dataset_id, None, None).await {
        Ok(Some(source)) => source,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(runtime::DatasetSourceError::Invalid(message)) => {
            return (StatusCode::BAD_REQUEST, Json(json!({ "error": message }))).into_response();
        }
        Err(runtime::DatasetSourceError::Database(error)) => {
            tracing::error!("schema validate lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    // 3. Read the underlying object and infer its schema.
    let bytes = match state.storage.get(&source.storage_path).await {
        Ok(bytes) => bytes,
        Err(error) => {
            return (
                StatusCode::OK,
                Json(ValidateResponse {
                    conforms: false,
                    files: vec![FileValidationReport {
                        path: source.storage_path.clone(),
                        format: source.dataset.format.clone(),
                        size_bytes: source.size_bytes,
                        conforms: false,
                        errors: vec![FileSchemaError {
                            field: String::new(),
                            kind: "storage_unreadable".into(),
                            message: error.to_string(),
                        }],
                    }],
                    schema_errors: vec![],
                }),
            )
                .into_response();
        }
    };

    let actual_fields = match runtime::prepare_query_context(&source.dataset.format, &bytes).await {
        Ok(prepared) => match runtime::load_schema_fields(&prepared.ctx).await {
            Ok(fields) => {
                runtime::cleanup_temp_path(prepared.path).await;
                fields
            }
            Err(error) => {
                runtime::cleanup_temp_path(prepared.path).await;
                return (
                    StatusCode::OK,
                    Json(ValidateResponse {
                        conforms: false,
                        files: vec![FileValidationReport {
                            path: source.storage_path.clone(),
                            format: source.dataset.format.clone(),
                            size_bytes: source.size_bytes,
                            conforms: false,
                            errors: vec![FileSchemaError {
                                field: String::new(),
                                kind: "schema_inference_failed".into(),
                                message: error,
                            }],
                        }],
                        schema_errors: vec![],
                    }),
                )
                    .into_response();
            }
        },
        Err(error) => {
            return (
                StatusCode::OK,
                Json(ValidateResponse {
                    conforms: false,
                    files: vec![FileValidationReport {
                        path: source.storage_path.clone(),
                        format: source.dataset.format.clone(),
                        size_bytes: source.size_bytes,
                        conforms: false,
                        errors: vec![FileSchemaError {
                            field: String::new(),
                            kind: "format_unsupported".into(),
                            message: error,
                        }],
                    }],
                    schema_errors: vec![],
                }),
            )
                .into_response();
        }
    };

    // 4. Compare proposal vs actual.
    let errors = compare_schemas(&body.schema.fields, &actual_fields);
    let report = FileValidationReport {
        path: source.storage_path.clone(),
        format: source.dataset.format.clone(),
        size_bytes: source.size_bytes,
        conforms: errors.is_empty(),
        errors,
    };
    let conforms = report.conforms;

    Json(ValidateResponse {
        conforms,
        files: vec![report],
        schema_errors: vec![],
    })
    .into_response()
}

fn compare_schemas(proposed: &[ProposedField], actual: &[ActualField]) -> Vec<FileSchemaError> {
    let mut errors = Vec::new();

    let mut actual_by_name = std::collections::HashMap::new();
    for f in actual {
        actual_by_name.insert(f.name.as_str(), f);
    }

    for field in proposed {
        let Some(actual) = actual_by_name.remove(field.name.as_str()) else {
            errors.push(FileSchemaError {
                field: field.name.clone(),
                kind: "missing_in_file".into(),
                message: format!(
                    "field `{}` is declared in schema but absent in file",
                    field.name
                ),
            });
            continue;
        };

        if !field.nullable && actual.nullable {
            errors.push(FileSchemaError {
                field: field.name.clone(),
                kind: "nullability_mismatch".into(),
                message: format!(
                    "field `{}` declared NOT NULL but file column is nullable",
                    field.name
                ),
            });
        }

        if !arrow_matches_field_type(&actual.field_type, &field.field_type) {
            errors.push(FileSchemaError {
                field: field.name.clone(),
                kind: "type_mismatch".into(),
                message: format!(
                    "field `{}` declared {} but file column is `{}`",
                    field.name,
                    foundry_type_name(&field.field_type),
                    actual.field_type
                ),
            });
        }
    }

    for extra in actual_by_name.keys() {
        errors.push(FileSchemaError {
            field: (*extra).to_string(),
            kind: "extra_in_file".into(),
            message: format!("file column `{}` is not declared in schema", extra),
        });
    }

    errors
}

/// Best-effort comparison between an Arrow `DataType::to_string()`
/// rendering (what `load_schema_fields` returns) and a Foundry
/// [`FieldType`]. Conservative: when the Arrow string isn't recognised
/// we return `false` to surface a mismatch the user can review.
fn arrow_matches_field_type(arrow_ty: &str, ft: &FieldType) -> bool {
    let lower = arrow_ty.to_ascii_lowercase();
    match ft {
        FieldType::Boolean => lower == "boolean",
        FieldType::Byte => lower == "int8",
        FieldType::Short => lower == "int16",
        FieldType::Integer => lower == "int32",
        FieldType::Long => lower == "int64",
        FieldType::Float => lower == "float32" || lower == "float",
        FieldType::Double => lower == "float64" || lower == "double",
        FieldType::String => lower == "utf8" || lower == "largeutf8" || lower == "string",
        FieldType::Binary => lower == "binary" || lower == "largebinary",
        FieldType::Date => lower.starts_with("date"),
        FieldType::Timestamp => lower.starts_with("timestamp"),
        FieldType::Decimal { precision, scale } => {
            // Arrow renders as e.g. "Decimal128(10, 2)".
            let needle = format!("({precision}, {scale})");
            lower.starts_with("decimal") && lower.contains(&needle)
        }
        FieldType::Array { .. } => lower.starts_with("list") || lower.starts_with("largelist"),
        FieldType::Map { .. } => lower.starts_with("map"),
        FieldType::Struct { .. } => lower.starts_with("struct"),
    }
}

fn foundry_type_name(ft: &FieldType) -> &'static str {
    match ft {
        FieldType::Boolean => "BOOLEAN",
        FieldType::Byte => "BYTE",
        FieldType::Short => "SHORT",
        FieldType::Integer => "INTEGER",
        FieldType::Long => "LONG",
        FieldType::Float => "FLOAT",
        FieldType::Double => "DOUBLE",
        FieldType::String => "STRING",
        FieldType::Binary => "BINARY",
        FieldType::Date => "DATE",
        FieldType::Timestamp => "TIMESTAMP",
        FieldType::Decimal { .. } => "DECIMAL",
        FieldType::Array { .. } => "ARRAY",
        FieldType::Map { .. } => "MAP",
        FieldType::Struct { .. } => "STRUCT",
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn af(name: &str, ty: &str, nullable: bool) -> ActualField {
        ActualField {
            name: name.into(),
            field_type: ty.into(),
            nullable,
        }
    }
    fn pf(name: &str, ft: FieldType, nullable: bool) -> ProposedField {
        ProposedField {
            name: name.into(),
            field_type: ft,
            nullable,
            description: None,
        }
    }

    #[test]
    fn matches_simple_primitives() {
        assert!(arrow_matches_field_type("Int64", &FieldType::Long));
        assert!(arrow_matches_field_type("Utf8", &FieldType::String));
        assert!(arrow_matches_field_type("Boolean", &FieldType::Boolean));
        assert!(arrow_matches_field_type("Float64", &FieldType::Double));
        assert!(!arrow_matches_field_type("Int64", &FieldType::Integer));
    }

    #[test]
    fn matches_decimal_precision_and_scale() {
        assert!(arrow_matches_field_type(
            "Decimal128(10, 2)",
            &FieldType::Decimal {
                precision: 10,
                scale: 2
            }
        ));
        assert!(!arrow_matches_field_type(
            "Decimal128(10, 3)",
            &FieldType::Decimal {
                precision: 10,
                scale: 2
            }
        ));
    }

    #[test]
    fn missing_extra_and_type_mismatches_are_reported() {
        let proposed = vec![
            pf("id", FieldType::Long, false),
            pf("name", FieldType::String, true),
            pf("ghost", FieldType::Integer, true),
        ];
        let actual = vec![
            af("id", "Int64", true),   // nullability mismatch
            af("name", "Int32", true), // type mismatch
            af("extra", "Utf8", true), // extra column
        ];
        let errors = compare_schemas(&proposed, &actual);
        let kinds: std::collections::HashSet<_> = errors.iter().map(|e| e.kind.as_str()).collect();
        assert!(kinds.contains("missing_in_file"));
        assert!(kinds.contains("nullability_mismatch"));
        assert!(kinds.contains("type_mismatch"));
        assert!(kinds.contains("extra_in_file"));
    }

    #[test]
    fn fully_conforming_schema_yields_no_errors() {
        let proposed = vec![
            pf("id", FieldType::Long, true),
            pf("name", FieldType::String, true),
        ];
        let actual = vec![af("id", "Int64", true), af("name", "Utf8", true)];
        assert!(compare_schemas(&proposed, &actual).is_empty());
    }
}
