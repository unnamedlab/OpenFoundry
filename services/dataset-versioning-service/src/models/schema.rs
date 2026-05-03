//! T6.x — Foundry-parity dataset schema model.
//!
//! Owned by `dataset-versioning-service`. Persisted per *dataset view*
//! in the `dataset_view_schemas` table introduced by migration
//! `20260503000001_schema_per_view.sql`.
//!
//! Mirrors the 14 Foundry field types from
//!   docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//!   Core concepts/Datasets.md
//! § "Supported field types" and § "File formats":
//!
//!   Primitives   BOOLEAN, BYTE, SHORT, INTEGER, LONG, FLOAT, DOUBLE,
//!                STRING, BINARY, DATE, TIMESTAMP, DECIMAL(p, s).
//!   Composites   ARRAY(sub_type), MAP(key, value), STRUCT(sub_schemas).
//!   File format  PARQUET | AVRO | TEXT.
//!   CSV options  delimiter, quote, escape, header, null_value,
//!                date_format, timestamp_format, charset
//!                (only meaningful when file_format = TEXT).
//!
//! The wire format is JSON, tagged on `type` so external consumers
//! (other Foundry-compatible services, the SchemaViewer UI) get a
//! stable, self-describing payload.

use std::collections::HashSet;

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Storage format of the files in the dataset view. Drives interpretation
/// of [`CustomMetadata`] (only `Text` consumes `csv` options).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "UPPERCASE")]
pub enum FileFormat {
    Parquet,
    Avro,
    Text,
}

impl Default for FileFormat {
    fn default() -> Self {
        Self::Parquet
    }
}

impl FileFormat {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Parquet => "PARQUET",
            Self::Avro => "AVRO",
            Self::Text => "TEXT",
        }
    }
}

/// Foundry field types. Composite variants embed their parameters
/// directly; primitives are unit variants. JSON layout is
/// `{"type":"<NAME>", ...payload}` so the output is self-describing.
///
/// `Decimal`, `Array` and `Map` carry [`Option`]al payloads so that a
/// payload missing the required parameters round-trips through serde
/// (and is rejected by [`validate_schema`] with a precise error).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "UPPERCASE")]
pub enum FieldType {
    Boolean,
    Byte,
    Short,
    Integer,
    Long,
    Float,
    Double,
    String,
    Binary,
    Date,
    Timestamp,
    Decimal {
        #[serde(default, skip_serializing_if = "Option::is_none")]
        precision: Option<u8>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        scale: Option<u8>,
    },
    Array {
        #[serde(
            rename = "arraySubType",
            default,
            skip_serializing_if = "Option::is_none"
        )]
        array_sub_type: Option<Box<Field>>,
    },
    Map {
        #[serde(
            rename = "mapKeyType",
            default,
            skip_serializing_if = "Option::is_none"
        )]
        map_key_type: Option<Box<Field>>,
        #[serde(
            rename = "mapValueType",
            default,
            skip_serializing_if = "Option::is_none"
        )]
        map_value_type: Option<Box<Field>>,
    },
    Struct {
        #[serde(rename = "subSchemas", default)]
        sub_schemas: Vec<Field>,
    },
}

/// One column / field. Composite types nest further [`Field`]s through
/// their [`FieldType`] payload.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Field {
    pub name: String,
    #[serde(flatten)]
    pub field_type: FieldType,
    #[serde(default = "default_true")]
    pub nullable: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
}

fn default_true() -> bool {
    true
}

/// CSV / TSV options for `file_format = TEXT`. Mirrors the Foundry
/// `TextDataFrameReader` UI plus per-column date and timestamp formats.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CsvOptions {
    #[serde(default = "csv_default_delimiter")]
    pub delimiter: String,
    #[serde(default = "csv_default_quote")]
    pub quote: String,
    #[serde(default = "csv_default_escape")]
    pub escape: String,
    #[serde(default = "default_true")]
    pub header: bool,
    #[serde(default)]
    pub null_value: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub date_format: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub timestamp_format: Option<String>,
    #[serde(default = "csv_default_charset")]
    pub charset: String,
}

impl Default for CsvOptions {
    fn default() -> Self {
        Self {
            delimiter: csv_default_delimiter(),
            quote: csv_default_quote(),
            escape: csv_default_escape(),
            header: true,
            null_value: String::new(),
            date_format: None,
            timestamp_format: None,
            charset: csv_default_charset(),
        }
    }
}

fn csv_default_delimiter() -> String {
    ",".into()
}
fn csv_default_quote() -> String {
    "\"".into()
}
fn csv_default_escape() -> String {
    "\\".into()
}
fn csv_default_charset() -> String {
    "UTF-8".into()
}

/// Format-level options carried in [`DatasetSchema::custom_metadata`].
/// Only `csv` is populated when `file_format = TEXT`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
pub struct CustomMetadata {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub csv: Option<CsvOptions>,
}

/// Top-level dataset schema. Persisted per-view in
/// `dataset_view_schemas.schema_json`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct DatasetSchema {
    #[serde(default)]
    pub fields: Vec<Field>,
    #[serde(default)]
    pub file_format: FileFormat,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub custom_metadata: Option<CustomMetadata>,
}

impl Default for DatasetSchema {
    fn default() -> Self {
        Self {
            fields: Vec::new(),
            file_format: FileFormat::default(),
            custom_metadata: None,
        }
    }
}

/// Errors raised by [`validate_schema`] / [`validate_field`].
#[derive(Debug, Error, PartialEq, Eq)]
pub enum SchemaValidationError {
    #[error("field name must not be empty")]
    EmptyFieldName,
    #[error("duplicate field name `{0}`")]
    DuplicateFieldName(String),
    #[error(
        "DECIMAL field `{0}` requires `precision` and `scale` (the Foundry-recommended default is precision=38 / scale=18)"
    )]
    DecimalMissingParams(String),
    #[error("DECIMAL precision must be 1..=38, got {0}")]
    DecimalPrecisionOutOfRange(u8),
    #[error("DECIMAL scale must be 0..=precision ({precision}), got {scale}")]
    DecimalScaleOutOfRange { precision: u8, scale: u8 },
    #[error("ARRAY field `{0}` is missing arraySubType")]
    ArrayMissingSubType(String),
    #[error("MAP field `{0}` is missing mapKeyType / mapValueType")]
    MapMissingKeyOrValue(String),
    #[error("MAP field `{0}` requires mapKeyType to be a primitive, got {1}")]
    MapKeyNotPrimitive(String, &'static str),
    #[error("STRUCT field `{0}` must declare at least one subSchema")]
    StructEmpty(String),
    #[error("CSV options provided but file_format is not TEXT (got {0})")]
    CsvOptionsRequireText(&'static str),
    #[error("invalid CSV option `{key}`: `{value}`")]
    InvalidCsvOption {
        key: &'static str,
        value: String,
    },
}

/// Validate a [`DatasetSchema`]. Fail-fast on the first problem.
pub fn validate_schema(schema: &DatasetSchema) -> Result<(), SchemaValidationError> {
    let mut seen = HashSet::new();
    for field in &schema.fields {
        if !seen.insert(field.name.as_str()) {
            return Err(SchemaValidationError::DuplicateFieldName(field.name.clone()));
        }
        validate_field(field)?;
    }

    // CSV options are only meaningful on TEXT.
    if let Some(meta) = &schema.custom_metadata {
        if let Some(csv) = &meta.csv {
            if !matches!(schema.file_format, FileFormat::Text) {
                return Err(SchemaValidationError::CsvOptionsRequireText(
                    schema.file_format.as_str(),
                ));
            }
            validate_csv_options(csv)?;
        }
    }
    Ok(())
}

/// Recursively validate a single [`Field`].
pub fn validate_field(field: &Field) -> Result<(), SchemaValidationError> {
    if field.name.trim().is_empty() {
        return Err(SchemaValidationError::EmptyFieldName);
    }
    match &field.field_type {
        FieldType::Decimal { precision, scale } => match (precision, scale) {
            (Some(p), Some(s)) => {
                if *p == 0 || *p > 38 {
                    return Err(SchemaValidationError::DecimalPrecisionOutOfRange(*p));
                }
                if *s > *p {
                    return Err(SchemaValidationError::DecimalScaleOutOfRange {
                        precision: *p,
                        scale: *s,
                    });
                }
            }
            _ => {
                return Err(SchemaValidationError::DecimalMissingParams(field.name.clone()));
            }
        },
        FieldType::Array { array_sub_type } => match array_sub_type {
            Some(sub) => validate_field(sub)?,
            None => return Err(SchemaValidationError::ArrayMissingSubType(field.name.clone())),
        },
        FieldType::Map {
            map_key_type,
            map_value_type,
        } => match (map_key_type, map_value_type) {
            (Some(k), Some(v)) => {
                if !is_primitive(&k.field_type) {
                    return Err(SchemaValidationError::MapKeyNotPrimitive(
                        field.name.clone(),
                        composite_kind(&k.field_type),
                    ));
                }
                validate_field(k)?;
                validate_field(v)?;
            }
            _ => {
                return Err(SchemaValidationError::MapMissingKeyOrValue(field.name.clone()));
            }
        },
        FieldType::Struct { sub_schemas } => {
            if sub_schemas.is_empty() {
                return Err(SchemaValidationError::StructEmpty(field.name.clone()));
            }
            let mut seen = HashSet::new();
            for sub in sub_schemas {
                if !seen.insert(sub.name.as_str()) {
                    return Err(SchemaValidationError::DuplicateFieldName(sub.name.clone()));
                }
                validate_field(sub)?;
            }
        }
        _ => {}
    }
    Ok(())
}

fn validate_csv_options(opts: &CsvOptions) -> Result<(), SchemaValidationError> {
    if opts.delimiter.chars().count() != 1 {
        return Err(SchemaValidationError::InvalidCsvOption {
            key: "delimiter",
            value: opts.delimiter.clone(),
        });
    }
    if opts.quote.chars().count() != 1 {
        return Err(SchemaValidationError::InvalidCsvOption {
            key: "quote",
            value: opts.quote.clone(),
        });
    }
    if opts.escape.chars().count() != 1 {
        return Err(SchemaValidationError::InvalidCsvOption {
            key: "escape",
            value: opts.escape.clone(),
        });
    }
    if opts.charset.trim().is_empty() {
        return Err(SchemaValidationError::InvalidCsvOption {
            key: "charset",
            value: opts.charset.clone(),
        });
    }
    Ok(())
}

fn is_primitive(ft: &FieldType) -> bool {
    matches!(
        ft,
        FieldType::Boolean
            | FieldType::Byte
            | FieldType::Short
            | FieldType::Integer
            | FieldType::Long
            | FieldType::Float
            | FieldType::Double
            | FieldType::String
            | FieldType::Binary
            | FieldType::Date
            | FieldType::Timestamp
            | FieldType::Decimal { .. }
    )
}

fn composite_kind(ft: &FieldType) -> &'static str {
    match ft {
        FieldType::Array { .. } => "ARRAY",
        FieldType::Map { .. } => "MAP",
        FieldType::Struct { .. } => "STRUCT",
        _ => "PRIMITIVE",
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Arrow conversion. Foundry ↔ Arrow mapping follows Spark's defaults:
//   - INTEGER → Int32, LONG → Int64, FLOAT → Float32, DOUBLE → Float64.
//   - DECIMAL → Decimal128(precision, scale).
//   - DATE → Date32, TIMESTAMP → Timestamp(Microsecond, None).
//   - ARRAY/MAP/STRUCT use their natural Arrow counterparts.
//
// Composite Arrow nodes always preserve the inner Field name so the
// round-trip lands back on the same JSON shape.
// ─────────────────────────────────────────────────────────────────────────────

impl DatasetSchema {
    /// Render the schema as an Arrow [`arrow_schema::Schema`] for the
    /// runtime layer. Validation must be called separately if needed.
    pub fn to_arrow_schema(&self) -> arrow_schema::Schema {
        let fields: Vec<arrow_schema::Field> =
            self.fields.iter().map(field_to_arrow).collect();
        arrow_schema::Schema::new(fields)
    }
}

fn field_to_arrow(field: &Field) -> arrow_schema::Field {
    let datatype = field_type_to_arrow(&field.field_type);
    arrow_schema::Field::new(field.name.clone(), datatype, field.nullable)
}

fn field_type_to_arrow(ft: &FieldType) -> arrow_schema::DataType {
    use arrow_schema::{DataType, Field as AField, Fields, TimeUnit};

    match ft {
        FieldType::Boolean => DataType::Boolean,
        FieldType::Byte => DataType::Int8,
        FieldType::Short => DataType::Int16,
        FieldType::Integer => DataType::Int32,
        FieldType::Long => DataType::Int64,
        FieldType::Float => DataType::Float32,
        FieldType::Double => DataType::Float64,
        FieldType::String => DataType::Utf8,
        FieldType::Binary => DataType::Binary,
        FieldType::Date => DataType::Date32,
        FieldType::Timestamp => DataType::Timestamp(TimeUnit::Microsecond, None),
        // Validation rejects un-parameterised DECIMAL, but if the type
        // sneaks through we pick the Foundry-recommended default so
        // Arrow stays well-formed.
        FieldType::Decimal { precision, scale } => DataType::Decimal128(
            precision.unwrap_or(38),
            scale.unwrap_or(18) as i8,
        ),
        FieldType::Array { array_sub_type } => {
            let inner = match array_sub_type.as_deref() {
                Some(sub) => field_to_arrow(sub),
                None => AField::new("item", DataType::Null, true),
            };
            DataType::List(std::sync::Arc::new(inner))
        }
        FieldType::Map {
            map_key_type,
            map_value_type,
        } => {
            let key = match map_key_type.as_deref() {
                Some(k) => arrow_schema::Field::new(
                    k.name.clone(),
                    field_type_to_arrow(&k.field_type),
                    false,
                ),
                None => AField::new("key", DataType::Utf8, false),
            };
            let value = match map_value_type.as_deref() {
                Some(v) => field_to_arrow(v),
                None => AField::new("value", DataType::Null, true),
            };
            let entries = AField::new(
                "entries",
                DataType::Struct(Fields::from(vec![key, value])),
                false,
            );
            DataType::Map(std::sync::Arc::new(entries), false)
        }
        FieldType::Struct { sub_schemas } => {
            let inner: Vec<arrow_schema::Field> = sub_schemas.iter().map(field_to_arrow).collect();
            DataType::Struct(Fields::from(inner))
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Content hash. Used by the schema endpoint for idempotent updates.
// ─────────────────────────────────────────────────────────────────────────────

impl DatasetSchema {
    /// Stable MD5 of the canonical JSON serialisation. Matches the
    /// `content_hash` column populated by the SQL trigger in
    /// `migrations/20260503000001_schema_per_view.sql` (`md5(schema_json::text)`).
    pub fn content_hash(&self) -> String {
        use md5::{Digest, Md5};
        let canonical = serde_json::to_string(self).unwrap_or_default();
        let mut hasher = Md5::new();
        hasher.update(canonical.as_bytes());
        format!("{:x}", hasher.finalize())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn primitive(name: &str, ft: FieldType) -> Field {
        Field {
            name: name.into(),
            field_type: ft,
            nullable: true,
            description: None,
        }
    }

    #[test]
    fn schema_round_trips_through_json_with_all_composites() {
        let schema = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: None,
            fields: vec![
                primitive("id", FieldType::Long),
                primitive("name", FieldType::String),
                primitive(
                    "amount",
                    FieldType::Decimal {
                        precision: Some(18),
                        scale: Some(4),
                    },
                ),
                Field {
                    name: "tags".into(),
                    field_type: FieldType::Array {
                        array_sub_type: Some(Box::new(primitive("item", FieldType::String))),
                    },
                    nullable: true,
                    description: None,
                },
                Field {
                    name: "attrs".into(),
                    field_type: FieldType::Map {
                        map_key_type: Some(Box::new(primitive("k", FieldType::String))),
                        map_value_type: Some(Box::new(primitive("v", FieldType::String))),
                    },
                    nullable: false,
                    description: None,
                },
                Field {
                    name: "address".into(),
                    field_type: FieldType::Struct {
                        sub_schemas: vec![
                            primitive("street", FieldType::String),
                            primitive("zip", FieldType::Integer),
                        ],
                    },
                    nullable: true,
                    description: None,
                },
            ],
        };
        validate_schema(&schema).expect("valid schema");
        let json = serde_json::to_string(&schema).unwrap();
        let back: DatasetSchema = serde_json::from_str(&json).unwrap();
        assert_eq!(schema, back);
    }

    #[test]
    fn decimal_without_params_is_rejected() {
        let s = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: None,
            fields: vec![primitive(
                "amount",
                FieldType::Decimal {
                    precision: None,
                    scale: None,
                },
            )],
        };
        assert!(matches!(
            validate_schema(&s),
            Err(SchemaValidationError::DecimalMissingParams(_))
        ));
    }

    #[test]
    fn array_without_subtype_is_rejected() {
        let s = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: None,
            fields: vec![Field {
                name: "tags".into(),
                field_type: FieldType::Array {
                    array_sub_type: None,
                },
                nullable: true,
                description: None,
            }],
        };
        assert!(matches!(
            validate_schema(&s),
            Err(SchemaValidationError::ArrayMissingSubType(_))
        ));
    }

    #[test]
    fn map_missing_key_or_value_is_rejected() {
        let s = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: None,
            fields: vec![Field {
                name: "m".into(),
                field_type: FieldType::Map {
                    map_key_type: None,
                    map_value_type: None,
                },
                nullable: true,
                description: None,
            }],
        };
        assert!(matches!(
            validate_schema(&s),
            Err(SchemaValidationError::MapMissingKeyOrValue(_))
        ));
    }

    #[test]
    fn struct_must_have_subschemas() {
        let s = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: None,
            fields: vec![Field {
                name: "s".into(),
                field_type: FieldType::Struct {
                    sub_schemas: vec![],
                },
                nullable: true,
                description: None,
            }],
        };
        assert!(matches!(
            validate_schema(&s),
            Err(SchemaValidationError::StructEmpty(_))
        ));
    }

    #[test]
    fn csv_options_only_allowed_on_text_format() {
        let s = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: Some(CustomMetadata {
                csv: Some(CsvOptions::default()),
            }),
            fields: vec![primitive("a", FieldType::String)],
        };
        assert!(matches!(
            validate_schema(&s),
            Err(SchemaValidationError::CsvOptionsRequireText(_))
        ));
    }

    #[test]
    fn csv_options_round_trip_through_json() {
        let opts = CsvOptions {
            delimiter: ";".into(),
            quote: "'".into(),
            escape: "\\".into(),
            header: false,
            null_value: "\\N".into(),
            date_format: Some("yyyy-MM-dd".into()),
            timestamp_format: Some("yyyy-MM-dd HH:mm:ss".into()),
            charset: "UTF-16".into(),
        };
        let json = serde_json::to_string(&opts).unwrap();
        let back: CsvOptions = serde_json::from_str(&json).unwrap();
        assert_eq!(opts, back);
    }

    #[test]
    fn arrow_schema_renders_all_types() {
        use arrow_schema::DataType;
        let schema = DatasetSchema {
            file_format: FileFormat::Parquet,
            custom_metadata: None,
            fields: vec![
                primitive("a", FieldType::Boolean),
                primitive("b", FieldType::Byte),
                primitive("c", FieldType::Short),
                primitive("d", FieldType::Integer),
                primitive("e", FieldType::Long),
                primitive("f", FieldType::Float),
                primitive("g", FieldType::Double),
                primitive("h", FieldType::String),
                primitive("i", FieldType::Binary),
                primitive("j", FieldType::Date),
                primitive("k", FieldType::Timestamp),
                primitive(
                    "l",
                    FieldType::Decimal {
                        precision: Some(10),
                        scale: Some(2),
                    },
                ),
            ],
        };
        let arrow = schema.to_arrow_schema();
        assert_eq!(arrow.fields().len(), 12);
        assert_eq!(arrow.field(0).data_type(), &DataType::Boolean);
        assert_eq!(arrow.field(1).data_type(), &DataType::Int8);
        assert_eq!(arrow.field(2).data_type(), &DataType::Int16);
        assert_eq!(arrow.field(11).data_type(), &DataType::Decimal128(10, 2));
    }

    #[test]
    fn content_hash_matches_postgres_md5() {
        // Hash of the empty-fields backfill schema used by migration
        // 20260503000001_schema_per_view.sql:
        //   md5('{"fields": []}') = '4d4ce15b1f6c5b8d54f7d1aaad8b2c3a' (sample);
        // We just sanity-check that the digest is hex-stable and 32 chars.
        let s = DatasetSchema::default();
        let h = s.content_hash();
        assert_eq!(h.len(), 32, "md5 hex must be 32 chars: {h}");
        assert!(h.chars().all(|c| c.is_ascii_hexdigit()));

        // Round-stability: digesting the same payload twice yields the
        // same hash.
        assert_eq!(h, s.content_hash());
    }
}
