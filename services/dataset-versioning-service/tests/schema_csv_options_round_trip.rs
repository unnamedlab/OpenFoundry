//! T6.x — CSV options round-trip through JSON serialisation.
//!
//! Foundry doc § "Schema options" specifies that CSV parsing options
//! are stored alongside the schema in `customMetadata`. Persisting and
//! reloading must be lossless across all 8 fields the UI exposes:
//! delimiter, quote, escape, header, null_value, date_format,
//! timestamp_format, charset.

use dataset_versioning_service::models::schema::{
    CsvOptions, CustomMetadata, DatasetSchema, Field, FieldType, FileFormat, validate_schema,
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
fn full_csv_options_payload_round_trips_through_serde() {
    let csv = CsvOptions {
        delimiter: ";".into(),
        quote: "'".into(),
        escape: "\\".into(),
        header: false,
        null_value: "\\N".into(),
        date_format: Some("yyyy-MM-dd".into()),
        timestamp_format: Some("yyyy-MM-dd HH:mm:ss.SSS".into()),
        charset: "ISO-8859-1".into(),
    };
    let schema = DatasetSchema {
        file_format: FileFormat::Text,
        custom_metadata: Some(CustomMetadata {
            csv: Some(csv.clone()),
        }),
        fields: vec![
            primitive("id", FieldType::Long),
            primitive("name", FieldType::String),
        ],
    };
    validate_schema(&schema).expect("valid TEXT schema");
    let json = serde_json::to_string(&schema).expect("serialise");
    let back: DatasetSchema = serde_json::from_str(&json).expect("deserialise");
    assert_eq!(schema, back);

    let back_csv = back
        .custom_metadata
        .as_ref()
        .and_then(|meta| meta.csv.as_ref())
        .expect("csv options preserved");
    assert_eq!(back_csv, &csv);
}

#[test]
fn csv_payload_keeps_optional_format_strings_when_unset() {
    // date_format / timestamp_format are optional; an empty Foundry
    // payload should serialise without those keys and deserialise back
    // to None, not Some(String::new()).
    let csv = CsvOptions {
        delimiter: ",".into(),
        quote: "\"".into(),
        escape: "\\".into(),
        header: true,
        null_value: String::new(),
        date_format: None,
        timestamp_format: None,
        charset: "UTF-8".into(),
    };
    let schema = DatasetSchema {
        file_format: FileFormat::Text,
        custom_metadata: Some(CustomMetadata {
            csv: Some(csv.clone()),
        }),
        fields: vec![primitive("a", FieldType::String)],
    };
    validate_schema(&schema).expect("valid");
    let json = serde_json::to_string(&schema).unwrap();
    assert!(
        !json.contains("dateFormat"),
        "absent dateFormat must be skipped on serialise: {json}"
    );
    let back: DatasetSchema = serde_json::from_str(&json).unwrap();
    let back_csv = back
        .custom_metadata
        .and_then(|meta| meta.csv)
        .expect("csv kept");
    assert_eq!(back_csv.date_format, None);
    assert_eq!(back_csv.timestamp_format, None);
    assert_eq!(back_csv, csv);
}

#[test]
fn csv_payload_detected_via_camel_case_keys() {
    // The on-the-wire keys use camelCase (matches the Foundry
    // `customMetadata.textParserParams`-style options listed in
    // `docs/.../CSV parsing.md`). We accept and emit the camelCase
    // form so the SchemaViewer UI and external clients can treat the
    // payload as Foundry-compatible.
    let json = r#"{
        "fields": [{"name":"a","type":"STRING","nullable":true}],
        "file_format": "TEXT",
        "custom_metadata": {
            "csv": {
                "delimiter": "|",
                "quote": "\"",
                "escape": "\\",
                "header": true,
                "nullValue": "NULL",
                "dateFormat": "yyyyMMdd",
                "timestampFormat": "yyyyMMdd-HHmmss",
                "charset": "UTF-8"
            }
        }
    }"#;
    let schema: DatasetSchema = serde_json::from_str(json).expect("parse camelCase");
    let csv = schema
        .custom_metadata
        .and_then(|meta| meta.csv)
        .expect("csv kept");
    assert_eq!(csv.delimiter, "|");
    assert_eq!(csv.null_value, "NULL");
    assert_eq!(csv.date_format.as_deref(), Some("yyyyMMdd"));
    assert_eq!(csv.timestamp_format.as_deref(), Some("yyyyMMdd-HHmmss"));
}
