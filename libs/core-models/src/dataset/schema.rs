//! T6.1 — Foundry-style dataset schema model.
//!
//! Mirrors the 15 primitive + composite field types documented in the
//! Foundry dataset reference (Boolean, Byte, Short, Integer, Long,
//! Float, Double, String, Binary, Date, Timestamp, Decimal, Array,
//! Map, Struct).
//!
//! The wire format is JSON-stable so a schema written by service
//! instance A can be re-imported by instance B (used by dataset
//! cross-instance clones).
//!
//! `Schema::custom_metadata` carries format-level options. For
//! `format = "text"` (CSV/TSV/etc.) those options are typed via
//! [`CsvOptions`] (delimiter, quote, escape, header, null value).

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Foundry-compatible field types.
///
/// All variants serialize as `{"type":"<NAME>", ...payload}` so the
/// output remains stable across crate refactors and other Foundry-
/// compatible implementations can consume it without renaming.
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
        /// Total number of digits, 1..=38.
        precision: u8,
        /// Digits to the right of the decimal point, 0..=precision.
        scale: u8,
    },
    Array {
        #[serde(rename = "arraySubType")]
        array_sub_type: Box<SchemaField>,
    },
    Map {
        #[serde(rename = "mapKeyType")]
        map_key_type: Box<SchemaField>,
        #[serde(rename = "mapValueType")]
        map_value_type: Box<SchemaField>,
    },
    Struct {
        #[serde(rename = "subSchemas")]
        sub_schemas: Vec<SchemaField>,
    },
}

/// One column / field in a schema. Composite types nest further
/// `SchemaField`s through their `FieldType` payload.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct SchemaField {
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

/// Top-level dataset schema. `format` is the storage format
/// (`"parquet"`, `"avro"`, `"text"`, …) and drives how
/// [`Schema::custom_metadata`] should be interpreted.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Schema {
    pub fields: Vec<SchemaField>,
    /// Storage format the schema applies to.
    pub format: String,
    /// Free-form per-format options. For `format = "text"`, prefer
    /// constructing this via [`CsvOptions::into_metadata`] so keys
    /// match the contract documented in [`CsvOptions`].
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub custom_metadata: BTreeMap<String, String>,
}

/// CSV / TSV options for `format = "text"`. Values round-trip through
/// the `BTreeMap<String, String>` in `Schema::custom_metadata` using
/// the keys `delimiter`, `quote`, `escape`, `header`, `nullValue`.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct CsvOptions {
    /// Field separator (one character). Default `,`.
    #[serde(default = "CsvOptions::default_delimiter")]
    pub delimiter: char,
    /// Quote character. Default `"`.
    #[serde(default = "CsvOptions::default_quote")]
    pub quote: char,
    /// Escape character. Default `\`.
    #[serde(default = "CsvOptions::default_escape")]
    pub escape: char,
    /// First row is a header.
    #[serde(default = "default_true")]
    pub header: bool,
    /// Token treated as `null` on read. Default empty string.
    #[serde(default)]
    pub null_value: String,
}

impl Default for CsvOptions {
    fn default() -> Self {
        Self {
            delimiter: Self::default_delimiter(),
            quote: Self::default_quote(),
            escape: Self::default_escape(),
            header: true,
            null_value: String::new(),
        }
    }
}

impl CsvOptions {
    fn default_delimiter() -> char {
        ','
    }
    fn default_quote() -> char {
        '"'
    }
    fn default_escape() -> char {
        '\\'
    }

    pub const KEY_DELIMITER: &'static str = "delimiter";
    pub const KEY_QUOTE: &'static str = "quote";
    pub const KEY_ESCAPE: &'static str = "escape";
    pub const KEY_HEADER: &'static str = "header";
    pub const KEY_NULL_VALUE: &'static str = "nullValue";

    /// Render the options as the key/value map persisted in
    /// `Schema::custom_metadata`.
    pub fn into_metadata(&self) -> BTreeMap<String, String> {
        let mut out = BTreeMap::new();
        out.insert(Self::KEY_DELIMITER.into(), self.delimiter.to_string());
        out.insert(Self::KEY_QUOTE.into(), self.quote.to_string());
        out.insert(Self::KEY_ESCAPE.into(), self.escape.to_string());
        out.insert(Self::KEY_HEADER.into(), self.header.to_string());
        out.insert(Self::KEY_NULL_VALUE.into(), self.null_value.clone());
        out
    }

    /// Parse the options from a `Schema::custom_metadata` map. Missing
    /// keys fall back to defaults; invalid character/boolean tokens
    /// yield [`SchemaValidationError::InvalidCsvOption`].
    pub fn from_metadata(
        metadata: &BTreeMap<String, String>,
    ) -> Result<Self, SchemaValidationError> {
        fn one_char(value: &str, key: &'static str) -> Result<char, SchemaValidationError> {
            let mut chars = value.chars();
            let first = chars
                .next()
                .ok_or(SchemaValidationError::InvalidCsvOption {
                    key,
                    value: value.into(),
                })?;
            if chars.next().is_some() {
                return Err(SchemaValidationError::InvalidCsvOption {
                    key,
                    value: value.into(),
                });
            }
            Ok(first)
        }

        let mut opts = CsvOptions::default();
        if let Some(value) = metadata.get(Self::KEY_DELIMITER) {
            opts.delimiter = one_char(value, Self::KEY_DELIMITER)?;
        }
        if let Some(value) = metadata.get(Self::KEY_QUOTE) {
            opts.quote = one_char(value, Self::KEY_QUOTE)?;
        }
        if let Some(value) = metadata.get(Self::KEY_ESCAPE) {
            opts.escape = one_char(value, Self::KEY_ESCAPE)?;
        }
        if let Some(value) = metadata.get(Self::KEY_HEADER) {
            opts.header = match value.as_str() {
                "true" | "TRUE" | "True" | "1" => true,
                "false" | "FALSE" | "False" | "0" => false,
                _ => {
                    return Err(SchemaValidationError::InvalidCsvOption {
                        key: Self::KEY_HEADER,
                        value: value.clone(),
                    });
                }
            };
        }
        if let Some(value) = metadata.get(Self::KEY_NULL_VALUE) {
            opts.null_value = value.clone();
        }
        Ok(opts)
    }
}

/// Errors raised when validating a [`Schema`] or one of its
/// [`SchemaField`]s. Returned by [`Schema::validate`] and
/// [`SchemaField::validate`].
#[derive(Debug, Error, PartialEq, Eq)]
pub enum SchemaValidationError {
    #[error("field name must not be empty")]
    EmptyFieldName,
    #[error("duplicate field name `{0}`")]
    DuplicateFieldName(String),
    #[error("decimal precision must be 1..=38, got {0}")]
    DecimalPrecisionOutOfRange(u8),
    #[error("decimal scale must be 0..=precision ({precision}), got {scale}")]
    DecimalScaleOutOfRange { precision: u8, scale: u8 },
    #[error("array field `{0}` is missing arraySubType")]
    ArrayMissingSubType(String),
    #[error("map field `{0}` requires mapKeyType to be a primitive, got {1}")]
    MapKeyNotPrimitive(String, &'static str),
    #[error("struct field `{0}` must declare at least one subSchema")]
    StructEmpty(String),
    #[error("invalid csv option `{key}`: `{value}`")]
    InvalidCsvOption { key: &'static str, value: String },
}

impl SchemaField {
    /// Recursively validate this field and its nested children.
    pub fn validate(&self) -> Result<(), SchemaValidationError> {
        if self.name.trim().is_empty() {
            return Err(SchemaValidationError::EmptyFieldName);
        }
        match &self.field_type {
            FieldType::Decimal { precision, scale } => {
                if *precision == 0 || *precision > 38 {
                    return Err(SchemaValidationError::DecimalPrecisionOutOfRange(
                        *precision,
                    ));
                }
                if *scale > *precision {
                    return Err(SchemaValidationError::DecimalScaleOutOfRange {
                        precision: *precision,
                        scale: *scale,
                    });
                }
            }
            FieldType::Array { array_sub_type } => {
                array_sub_type.validate()?;
            }
            FieldType::Map {
                map_key_type,
                map_value_type,
            } => {
                if !is_primitive(&map_key_type.field_type) {
                    return Err(SchemaValidationError::MapKeyNotPrimitive(
                        self.name.clone(),
                        composite_kind(&map_key_type.field_type),
                    ));
                }
                map_key_type.validate()?;
                map_value_type.validate()?;
            }
            FieldType::Struct { sub_schemas } => {
                if sub_schemas.is_empty() {
                    return Err(SchemaValidationError::StructEmpty(self.name.clone()));
                }
                let mut seen = std::collections::HashSet::new();
                for sub in sub_schemas {
                    if !seen.insert(sub.name.as_str()) {
                        return Err(SchemaValidationError::DuplicateFieldName(sub.name.clone()));
                    }
                    sub.validate()?;
                }
            }
            _ => {}
        }
        Ok(())
    }
}

impl Schema {
    /// Validate every top-level field and fail-fast on the first
    /// problem. Also rejects duplicate field names at the top level.
    pub fn validate(&self) -> Result<(), SchemaValidationError> {
        let mut seen = std::collections::HashSet::new();
        for field in &self.fields {
            if !seen.insert(field.name.as_str()) {
                return Err(SchemaValidationError::DuplicateFieldName(
                    field.name.clone(),
                ));
            }
            field.validate()?;
        }
        if self.format == "text" {
            // Sanity-check CSV options (without materialising them).
            let _ = CsvOptions::from_metadata(&self.custom_metadata)?;
        }
        Ok(())
    }
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

#[cfg(test)]
mod tests {
    use super::*;

    fn primitive(name: &str, ft: FieldType) -> SchemaField {
        SchemaField {
            name: name.into(),
            field_type: ft,
            nullable: true,
            description: None,
        }
    }

    #[test]
    fn all_15_field_types_serialize_with_stable_tag() {
        let cases: Vec<(FieldType, &str)> = vec![
            (FieldType::Boolean, "BOOLEAN"),
            (FieldType::Byte, "BYTE"),
            (FieldType::Short, "SHORT"),
            (FieldType::Integer, "INTEGER"),
            (FieldType::Long, "LONG"),
            (FieldType::Float, "FLOAT"),
            (FieldType::Double, "DOUBLE"),
            (FieldType::String, "STRING"),
            (FieldType::Binary, "BINARY"),
            (FieldType::Date, "DATE"),
            (FieldType::Timestamp, "TIMESTAMP"),
            (
                FieldType::Decimal {
                    precision: 10,
                    scale: 2,
                },
                "DECIMAL",
            ),
            (
                FieldType::Array {
                    array_sub_type: Box::new(primitive("inner", FieldType::Long)),
                },
                "ARRAY",
            ),
            (
                FieldType::Map {
                    map_key_type: Box::new(primitive("k", FieldType::String)),
                    map_value_type: Box::new(primitive("v", FieldType::Long)),
                },
                "MAP",
            ),
            (
                FieldType::Struct {
                    sub_schemas: vec![primitive("a", FieldType::Integer)],
                },
                "STRUCT",
            ),
        ];
        assert_eq!(cases.len(), 15);
        for (ft, expected_tag) in cases {
            let json = serde_json::to_value(&ft).unwrap();
            assert_eq!(
                json.get("type").and_then(|v| v.as_str()),
                Some(expected_tag)
            );
        }
    }

    #[test]
    fn schema_round_trips_through_json() {
        let schema = Schema {
            format: "parquet".into(),
            custom_metadata: BTreeMap::new(),
            fields: vec![
                primitive("id", FieldType::Long),
                primitive("name", FieldType::String),
                SchemaField {
                    name: "tags".into(),
                    field_type: FieldType::Array {
                        array_sub_type: Box::new(primitive("item", FieldType::String)),
                    },
                    nullable: true,
                    description: Some("list of tags".into()),
                },
                SchemaField {
                    name: "attrs".into(),
                    field_type: FieldType::Map {
                        map_key_type: Box::new(primitive("k", FieldType::String)),
                        map_value_type: Box::new(primitive("v", FieldType::String)),
                    },
                    nullable: false,
                    description: None,
                },
                SchemaField {
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
                SchemaField {
                    name: "amount".into(),
                    field_type: FieldType::Decimal {
                        precision: 18,
                        scale: 4,
                    },
                    nullable: true,
                    description: None,
                },
            ],
        };
        schema.validate().unwrap();
        let json = serde_json::to_string(&schema).unwrap();
        let back: Schema = serde_json::from_str(&json).unwrap();
        assert_eq!(schema, back);
    }

    #[test]
    fn decimal_precision_must_be_in_range() {
        let s = primitive(
            "x",
            FieldType::Decimal {
                precision: 0,
                scale: 0,
            },
        );
        assert!(matches!(
            s.validate(),
            Err(SchemaValidationError::DecimalPrecisionOutOfRange(0))
        ));

        let s = primitive(
            "x",
            FieldType::Decimal {
                precision: 39,
                scale: 0,
            },
        );
        assert!(matches!(
            s.validate(),
            Err(SchemaValidationError::DecimalPrecisionOutOfRange(39))
        ));
    }

    #[test]
    fn decimal_scale_cannot_exceed_precision() {
        let s = primitive(
            "x",
            FieldType::Decimal {
                precision: 4,
                scale: 5,
            },
        );
        assert!(matches!(
            s.validate(),
            Err(SchemaValidationError::DecimalScaleOutOfRange {
                precision: 4,
                scale: 5
            })
        ));
    }

    #[test]
    fn map_key_must_be_primitive() {
        let s = SchemaField {
            name: "m".into(),
            field_type: FieldType::Map {
                map_key_type: Box::new(SchemaField {
                    name: "k".into(),
                    field_type: FieldType::Array {
                        array_sub_type: Box::new(primitive("inner", FieldType::Long)),
                    },
                    nullable: true,
                    description: None,
                }),
                map_value_type: Box::new(primitive("v", FieldType::Long)),
            },
            nullable: true,
            description: None,
        };
        assert!(matches!(
            s.validate(),
            Err(SchemaValidationError::MapKeyNotPrimitive(_, "ARRAY"))
        ));
    }

    #[test]
    fn struct_must_have_subschemas() {
        let s = SchemaField {
            name: "s".into(),
            field_type: FieldType::Struct {
                sub_schemas: vec![],
            },
            nullable: true,
            description: None,
        };
        assert!(matches!(
            s.validate(),
            Err(SchemaValidationError::StructEmpty(_))
        ));
    }

    #[test]
    fn duplicate_field_names_are_rejected() {
        let schema = Schema {
            format: "parquet".into(),
            custom_metadata: BTreeMap::new(),
            fields: vec![
                primitive("a", FieldType::Long),
                primitive("a", FieldType::String),
            ],
        };
        assert!(matches!(
            schema.validate(),
            Err(SchemaValidationError::DuplicateFieldName(_))
        ));
    }

    #[test]
    fn csv_options_round_trip_through_metadata() {
        let opts = CsvOptions {
            delimiter: ';',
            quote: '\'',
            escape: '\\',
            header: false,
            null_value: "\\N".into(),
        };
        let meta = opts.into_metadata();
        let back = CsvOptions::from_metadata(&meta).unwrap();
        assert_eq!(opts, back);
    }

    #[test]
    fn csv_options_default_when_metadata_empty() {
        let back = CsvOptions::from_metadata(&BTreeMap::new()).unwrap();
        assert_eq!(back, CsvOptions::default());
    }

    #[test]
    fn csv_options_reject_multi_char_delimiter() {
        let mut meta = BTreeMap::new();
        meta.insert("delimiter".into(), "||".into());
        assert!(matches!(
            CsvOptions::from_metadata(&meta),
            Err(SchemaValidationError::InvalidCsvOption {
                key: "delimiter",
                ..
            })
        ));
    }

    #[test]
    fn text_format_validates_csv_metadata() {
        let mut meta = BTreeMap::new();
        meta.insert("header".into(), "yes".into()); // invalid bool token
        let schema = Schema {
            format: "text".into(),
            custom_metadata: meta,
            fields: vec![primitive("a", FieldType::String)],
        };
        assert!(matches!(
            schema.validate(),
            Err(SchemaValidationError::InvalidCsvOption { key: "header", .. })
        ));
    }
}
