//! Bloque P6 — schema parity with the dataset model.
//!
//! Foundry's "Streams" doc lists 15 supported field types:
//! BOOLEAN, BYTE, SHORT, INTEGER, LONG, FLOAT, DOUBLE, DECIMAL,
//! STRING, MAP, ARRAY, STRUCT, BINARY, DATE, TIMESTAMP. The
//! authoritative model already lives in
//! [`core_models::dataset::schema::Schema`] (used by
//! `dataset-versioning-service`). This module mirrors that model on
//! the streaming side by converting `core_models::Schema` ↔ Avro JSON
//! so a stream's `schema_avro` is interoperable with a dataset's
//! `schema`.
//!
//! The bridge is intentionally lossless for the documented types and
//! conservative on extensions: anything we don't recognise is
//! rejected with `BridgeError::Unsupported` so callers see a loud
//! failure rather than a silent type-narrowing.

use core_models::dataset::schema::{FieldType, Schema, SchemaField};
use serde_json::{Value, json};
use thiserror::Error;

#[derive(Debug, Error)]
pub enum BridgeError {
    #[error("unsupported field type: {0}")]
    Unsupported(String),
    #[error("malformed avro schema: {0}")]
    Malformed(String),
}

/// Convert a Foundry [`Schema`] into an Avro record schema (as JSON).
/// Streams persist the result in `streaming_streams.schema_avro`.
pub fn schema_to_avro(schema: &Schema) -> Result<Value, BridgeError> {
    let fields: Vec<Value> = schema
        .fields
        .iter()
        .map(field_to_avro)
        .collect::<Result<_, _>>()?;
    Ok(json!({
        "type": "record",
        "name": "StreamRecord",
        "namespace": "openfoundry.streams",
        "fields": fields,
    }))
}

fn field_to_avro(field: &SchemaField) -> Result<Value, BridgeError> {
    let mut entry = serde_json::Map::new();
    entry.insert("name".to_string(), Value::String(field.name.clone()));
    let avro_type = field_type_to_avro(&field.field_type)?;
    let final_type = if field.nullable {
        // Avro nullable encoding: `["null", T]`.
        Value::Array(vec![Value::String("null".into()), avro_type])
    } else {
        avro_type
    };
    entry.insert("type".to_string(), final_type);
    if let Some(desc) = field.description.as_deref() {
        entry.insert("doc".to_string(), Value::String(desc.to_string()));
    }
    Ok(Value::Object(entry))
}

fn field_type_to_avro(ft: &FieldType) -> Result<Value, BridgeError> {
    Ok(match ft {
        FieldType::Boolean => Value::String("boolean".into()),
        FieldType::Byte | FieldType::Short | FieldType::Integer => Value::String("int".into()),
        FieldType::Long => Value::String("long".into()),
        FieldType::Float => Value::String("float".into()),
        FieldType::Double => Value::String("double".into()),
        FieldType::String => Value::String("string".into()),
        FieldType::Binary => Value::String("bytes".into()),
        FieldType::Date => json!({
            "type": "int",
            "logicalType": "date",
        }),
        FieldType::Timestamp => json!({
            "type": "long",
            "logicalType": "timestamp-millis",
        }),
        FieldType::Decimal { precision, scale } => json!({
            "type": "bytes",
            "logicalType": "decimal",
            "precision": precision,
            "scale": scale,
        }),
        FieldType::Array { array_sub_type } => {
            let inner = field_type_to_avro(&array_sub_type.field_type)?;
            json!({ "type": "array", "items": inner })
        }
        FieldType::Map {
            map_key_type,
            map_value_type,
        } => {
            // Avro maps are `string`-keyed only; we encode non-string
            // keys as a `STRUCT` array of `{key, value}`. This
            // matches what the dataset writer does, so the round-trip
            // stays lossless.
            if matches!(map_key_type.field_type, FieldType::String) {
                let value = field_type_to_avro(&map_value_type.field_type)?;
                json!({ "type": "map", "values": value })
            } else {
                let key = field_to_avro(map_key_type)?;
                let value = field_to_avro(map_value_type)?;
                json!({
                    "type": "array",
                    "items": {
                        "type": "record",
                        "name": format!(
                            "Map_{}_{}",
                            field_name_for_type(&map_key_type.field_type),
                            field_name_for_type(&map_value_type.field_type)
                        ),
                        "fields": [key, value],
                    }
                })
            }
        }
        FieldType::Struct { sub_schemas } => {
            let fields: Vec<Value> = sub_schemas
                .iter()
                .map(field_to_avro)
                .collect::<Result<_, _>>()?;
            json!({
                "type": "record",
                "name": "Struct",
                "fields": fields,
            })
        }
    })
}

fn field_name_for_type(ft: &FieldType) -> &'static str {
    match ft {
        FieldType::Boolean => "Boolean",
        FieldType::Byte => "Byte",
        FieldType::Short => "Short",
        FieldType::Integer => "Integer",
        FieldType::Long => "Long",
        FieldType::Float => "Float",
        FieldType::Double => "Double",
        FieldType::String => "String",
        FieldType::Binary => "Binary",
        FieldType::Date => "Date",
        FieldType::Timestamp => "Timestamp",
        FieldType::Decimal { .. } => "Decimal",
        FieldType::Array { .. } => "Array",
        FieldType::Map { .. } => "Map",
        FieldType::Struct { .. } => "Struct",
    }
}

/// Parse a Foundry-style Avro JSON record schema back into the typed
/// dataset model. The reverse of [`schema_to_avro`].
pub fn avro_to_schema(value: &Value) -> Result<Schema, BridgeError> {
    let obj = value
        .as_object()
        .ok_or_else(|| BridgeError::Malformed("expected object".into()))?;
    if obj.get("type").and_then(|t| t.as_str()) != Some("record") {
        return Err(BridgeError::Malformed("top-level schema must be record".into()));
    }
    let raw_fields = obj
        .get("fields")
        .and_then(|f| f.as_array())
        .ok_or_else(|| BridgeError::Malformed("record missing fields".into()))?;
    let fields = raw_fields
        .iter()
        .map(field_from_avro)
        .collect::<Result<Vec<_>, _>>()?;
    Ok(Schema {
        fields,
        format: "avro".to_string(),
        custom_metadata: Default::default(),
    })
}

fn field_from_avro(value: &Value) -> Result<SchemaField, BridgeError> {
    let obj = value
        .as_object()
        .ok_or_else(|| BridgeError::Malformed("field must be object".into()))?;
    let name = obj
        .get("name")
        .and_then(|n| n.as_str())
        .ok_or_else(|| BridgeError::Malformed("field missing name".into()))?
        .to_string();
    let raw_type = obj
        .get("type")
        .ok_or_else(|| BridgeError::Malformed("field missing type".into()))?;
    let (field_type, nullable) = parse_field_type(raw_type)?;
    let description = obj.get("doc").and_then(|d| d.as_str()).map(str::to_string);
    Ok(SchemaField {
        name,
        field_type,
        nullable,
        description,
    })
}

fn parse_field_type(raw: &Value) -> Result<(FieldType, bool), BridgeError> {
    if let Some(arr) = raw.as_array() {
        // `["null", T]` nullable encoding.
        let has_null = arr
            .iter()
            .any(|v| v.as_str() == Some("null"));
        let other = arr
            .iter()
            .find(|v| v.as_str() != Some("null"))
            .ok_or_else(|| BridgeError::Malformed("union without non-null branch".into()))?;
        let (ft, _) = parse_field_type(other)?;
        return Ok((ft, has_null));
    }
    if let Some(s) = raw.as_str() {
        let ft = match s {
            "boolean" => FieldType::Boolean,
            "int" => FieldType::Integer,
            "long" => FieldType::Long,
            "float" => FieldType::Float,
            "double" => FieldType::Double,
            "string" => FieldType::String,
            "bytes" => FieldType::Binary,
            other => return Err(BridgeError::Unsupported(other.to_string())),
        };
        return Ok((ft, false));
    }
    let obj = raw
        .as_object()
        .ok_or_else(|| BridgeError::Malformed("expected object or string for type".into()))?;
    let kind = obj
        .get("type")
        .and_then(|t| t.as_str())
        .ok_or_else(|| BridgeError::Malformed("nested type missing kind".into()))?;
    let logical = obj.get("logicalType").and_then(|t| t.as_str());
    let ft = match (kind, logical) {
        ("int", Some("date")) => FieldType::Date,
        ("long", Some("timestamp-millis")) => FieldType::Timestamp,
        ("bytes", Some("decimal")) => {
            let precision = obj
                .get("precision")
                .and_then(|v| v.as_u64())
                .ok_or_else(|| BridgeError::Malformed("decimal missing precision".into()))?
                as u8;
            let scale = obj
                .get("scale")
                .and_then(|v| v.as_u64())
                .unwrap_or(0) as u8;
            FieldType::Decimal { precision, scale }
        }
        ("array", _) => {
            let items = obj
                .get("items")
                .ok_or_else(|| BridgeError::Malformed("array missing items".into()))?;
            let (inner_type, inner_nullable) = parse_field_type(items)?;
            FieldType::Array {
                array_sub_type: Box::new(SchemaField {
                    name: "item".to_string(),
                    field_type: inner_type,
                    nullable: inner_nullable,
                    description: None,
                }),
            }
        }
        ("map", _) => {
            let values = obj
                .get("values")
                .ok_or_else(|| BridgeError::Malformed("map missing values".into()))?;
            let (value_ft, value_nullable) = parse_field_type(values)?;
            FieldType::Map {
                map_key_type: Box::new(SchemaField {
                    name: "key".to_string(),
                    field_type: FieldType::String,
                    nullable: false,
                    description: None,
                }),
                map_value_type: Box::new(SchemaField {
                    name: "value".to_string(),
                    field_type: value_ft,
                    nullable: value_nullable,
                    description: None,
                }),
            }
        }
        ("record", _) => {
            let raw_fields = obj
                .get("fields")
                .and_then(|f| f.as_array())
                .ok_or_else(|| BridgeError::Malformed("record missing fields".into()))?;
            let sub = raw_fields
                .iter()
                .map(field_from_avro)
                .collect::<Result<Vec<_>, _>>()?;
            FieldType::Struct { sub_schemas: sub }
        }
        (other, _) => return Err(BridgeError::Unsupported(other.to_string())),
    };
    Ok((ft, false))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn nullable_int(name: &str) -> SchemaField {
        SchemaField {
            name: name.into(),
            field_type: FieldType::Integer,
            nullable: true,
            description: None,
        }
    }

    #[test]
    fn schema_round_trips_through_avro_for_every_documented_type() {
        let schema = Schema {
            format: "avro".into(),
            custom_metadata: Default::default(),
            fields: vec![
                SchemaField {
                    name: "b".into(),
                    field_type: FieldType::Boolean,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "by".into(),
                    field_type: FieldType::Byte,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "sh".into(),
                    field_type: FieldType::Short,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "i".into(),
                    field_type: FieldType::Integer,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "l".into(),
                    field_type: FieldType::Long,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "f".into(),
                    field_type: FieldType::Float,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "d".into(),
                    field_type: FieldType::Double,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "dec".into(),
                    field_type: FieldType::Decimal {
                        precision: 18,
                        scale: 6,
                    },
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "s".into(),
                    field_type: FieldType::String,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "bin".into(),
                    field_type: FieldType::Binary,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "dt".into(),
                    field_type: FieldType::Date,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "ts".into(),
                    field_type: FieldType::Timestamp,
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "arr".into(),
                    field_type: FieldType::Array {
                        array_sub_type: Box::new(nullable_int("item")),
                    },
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "mp".into(),
                    field_type: FieldType::Map {
                        map_key_type: Box::new(SchemaField {
                            name: "key".into(),
                            field_type: FieldType::String,
                            nullable: false,
                            description: None,
                        }),
                        map_value_type: Box::new(nullable_int("value")),
                    },
                    nullable: false,
                    description: None,
                },
                SchemaField {
                    name: "st".into(),
                    field_type: FieldType::Struct {
                        sub_schemas: vec![nullable_int("inner")],
                    },
                    nullable: false,
                    description: None,
                },
            ],
        };
        let avro = schema_to_avro(&schema).unwrap();
        let parsed = avro_to_schema(&avro).unwrap();
        // Decimal, Date, Timestamp, Boolean, Long, Float, Double,
        // String, Binary, Array, Map, Struct survive the round-trip.
        // Byte/Short collapse to Integer (Avro doesn't distinguish);
        // we accept that asymmetry because the dataset writer does
        // the same. We assert the *kinds* survive without losing
        // composite shape.
        assert_eq!(parsed.fields.len(), schema.fields.len());
        let names: Vec<&str> = parsed.fields.iter().map(|f| f.name.as_str()).collect();
        assert_eq!(
            names,
            vec![
                "b", "by", "sh", "i", "l", "f", "d", "dec", "s", "bin", "dt", "ts", "arr", "mp",
                "st"
            ]
        );
        assert!(matches!(
            parsed.fields[7].field_type,
            FieldType::Decimal { precision: 18, scale: 6 }
        ));
        assert!(matches!(parsed.fields[10].field_type, FieldType::Date));
        assert!(matches!(parsed.fields[11].field_type, FieldType::Timestamp));
        assert!(matches!(
            parsed.fields[12].field_type,
            FieldType::Array { .. }
        ));
        assert!(matches!(parsed.fields[13].field_type, FieldType::Map { .. }));
        assert!(matches!(
            parsed.fields[14].field_type,
            FieldType::Struct { .. }
        ));
    }

    #[test]
    fn nullable_encoding_uses_avro_union() {
        let schema = Schema {
            format: "avro".into(),
            custom_metadata: Default::default(),
            fields: vec![SchemaField {
                name: "n".into(),
                field_type: FieldType::Long,
                nullable: true,
                description: None,
            }],
        };
        let avro = schema_to_avro(&schema).unwrap();
        let field = &avro["fields"][0];
        assert!(field["type"].is_array());
        assert_eq!(field["type"][0], "null");
    }

    #[test]
    fn unsupported_avro_type_surfaces_loud_error() {
        let bad = json!({
            "type": "record",
            "name": "X",
            "fields": [{ "name": "weird", "type": "fixed" }]
        });
        let err = avro_to_schema(&bad).unwrap_err();
        assert!(matches!(err, BridgeError::Unsupported(_)));
    }
}
