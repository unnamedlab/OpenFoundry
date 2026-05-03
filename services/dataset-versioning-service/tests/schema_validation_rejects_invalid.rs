//! T6.x — schema validation rejects invalid payloads.
//!
//! These are pure-Rust unit tests: the validation rules don't need the
//! HTTP harness, they live entirely in
//! `dataset_versioning_service::models::schema::validate_schema`.

use dataset_versioning_service::models::schema::{
    CsvOptions, CustomMetadata, DatasetSchema, Field, FieldType, FileFormat,
    SchemaValidationError, validate_schema,
};

fn primitive(name: &str, ft: FieldType) -> Field {
    Field {
        name: name.into(),
        field_type: ft,
        nullable: true,
        description: None,
    }
}

#[test]
fn decimal_without_precision_or_scale_is_rejected() {
    // Foundry doc § "Supported field types": DECIMAL requires
    // `precision` and `scale`. Default suggestion is 38/18.
    let schema = DatasetSchema {
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
        validate_schema(&schema),
        Err(SchemaValidationError::DecimalMissingParams(_))
    ));
}

#[test]
fn decimal_precision_out_of_range_is_rejected() {
    let schema = DatasetSchema {
        file_format: FileFormat::Parquet,
        custom_metadata: None,
        fields: vec![primitive(
            "x",
            FieldType::Decimal {
                precision: Some(39),
                scale: Some(0),
            },
        )],
    };
    assert!(matches!(
        validate_schema(&schema),
        Err(SchemaValidationError::DecimalPrecisionOutOfRange(39))
    ));
}

#[test]
fn map_without_key_or_value_is_rejected() {
    let schema = DatasetSchema {
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
        validate_schema(&schema),
        Err(SchemaValidationError::MapMissingKeyOrValue(_))
    ));
}

#[test]
fn map_with_complex_key_is_rejected() {
    // Foundry's MAP requires the key type to be a primitive. Spark
    // supports composite keys but this is documented as primitive-only.
    let schema = DatasetSchema {
        file_format: FileFormat::Parquet,
        custom_metadata: None,
        fields: vec![Field {
            name: "m".into(),
            field_type: FieldType::Map {
                map_key_type: Some(Box::new(Field {
                    name: "k".into(),
                    field_type: FieldType::Array {
                        array_sub_type: Some(Box::new(primitive("inner", FieldType::Long))),
                    },
                    nullable: false,
                    description: None,
                })),
                map_value_type: Some(Box::new(primitive("v", FieldType::Long))),
            },
            nullable: true,
            description: None,
        }],
    };
    assert!(matches!(
        validate_schema(&schema),
        Err(SchemaValidationError::MapKeyNotPrimitive(_, "ARRAY"))
    ));
}

#[test]
fn array_without_subtype_is_rejected() {
    let schema = DatasetSchema {
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
        validate_schema(&schema),
        Err(SchemaValidationError::ArrayMissingSubType(_))
    ));
}

#[test]
fn struct_without_subschemas_is_rejected() {
    let schema = DatasetSchema {
        file_format: FileFormat::Parquet,
        custom_metadata: None,
        fields: vec![Field {
            name: "address".into(),
            field_type: FieldType::Struct {
                sub_schemas: vec![],
            },
            nullable: true,
            description: None,
        }],
    };
    assert!(matches!(
        validate_schema(&schema),
        Err(SchemaValidationError::StructEmpty(_))
    ));
}

#[test]
fn csv_options_on_non_text_format_is_rejected() {
    // CSV options are only meaningful when file_format = TEXT
    // (Foundry doc § "Schema options").
    let schema = DatasetSchema {
        file_format: FileFormat::Parquet,
        custom_metadata: Some(CustomMetadata {
            csv: Some(CsvOptions::default()),
        }),
        fields: vec![primitive("a", FieldType::String)],
    };
    assert!(matches!(
        validate_schema(&schema),
        Err(SchemaValidationError::CsvOptionsRequireText(_))
    ));
}

#[test]
fn duplicate_top_level_field_names_are_rejected() {
    let schema = DatasetSchema {
        file_format: FileFormat::Parquet,
        custom_metadata: None,
        fields: vec![
            primitive("a", FieldType::Long),
            primitive("a", FieldType::String),
        ],
    };
    assert!(matches!(
        validate_schema(&schema),
        Err(SchemaValidationError::DuplicateFieldName(_))
    ));
}

#[test]
fn valid_decimal_38_18_default_round_trips() {
    // Sanity: the documented default precision=38 / scale=18 is valid.
    let schema = DatasetSchema {
        file_format: FileFormat::Parquet,
        custom_metadata: None,
        fields: vec![primitive(
            "amount",
            FieldType::Decimal {
                precision: Some(38),
                scale: Some(18),
            },
        )],
    };
    assert!(validate_schema(&schema).is_ok());
}
