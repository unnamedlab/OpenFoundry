//! Avro Object Container Format reader.
//!
//! Implementation strategy: the workspace pins `apache-avro = 0.17`,
//! which exposes a streaming `Reader<R>` and an embedded writer
//! schema, but does not produce Arrow `RecordBatch`es directly.
//! Pulling `arrow-avro` would conflict with `xz2 / liblzma` from the
//! datafusion ecosystem (`arrow-avro` uses `xz`, datafusion uses `xz2`,
//! and Cargo refuses two `links = "lzma"` packages in the same graph).
//!
//! Instead we decode each Avro record to `apache_avro::types::Value`
//! and then build Arrow arrays column-by-column, deriving the Arrow
//! schema from either the caller-provided hint or the writer schema
//! embedded in the file.
//!
//! This keeps the reader self-contained and binary-faithful for the
//! types Foundry's preview cares about: scalars, strings, bytes,
//! booleans, integers, floats, doubles, and `null`.

use std::sync::Arc;

use apache_avro::Reader as AvroReader;
use apache_avro::Schema as WriterSchema;
use apache_avro::types::Value as AvroValue;
use arrow_array_readers::{
    ArrayRef, BinaryArray, BooleanArray, Float32Array, Float64Array, Int32Array, Int64Array,
    RecordBatch, StringArray,
};
use arrow_schema_readers::{DataType, Field, Schema as ArrowSchema, SchemaRef};
use async_trait::async_trait;
use bytes::Bytes;

use super::{FileReader, FileUri, ReadOptions, ReaderError, ReaderResult};

#[derive(Debug, Default)]
pub struct AvroFileReader;

#[async_trait]
impl FileReader for AvroFileReader {
    async fn read(
        &self,
        _uri: &FileUri,
        bytes: Bytes,
        opts: ReadOptions,
    ) -> ReaderResult<Vec<RecordBatch>> {
        let limit = opts.limit;
        let hint = opts.schema;
        tokio::task::spawn_blocking(move || decode_avro(bytes, hint, limit))
            .await
            .map_err(|e| ReaderError::Avro(format!("blocking task panicked: {e}")))?
    }
}

fn decode_avro(
    bytes: Bytes,
    schema_hint: Option<ArrowSchema>,
    limit: Option<usize>,
) -> ReaderResult<Vec<RecordBatch>> {
    let reader = AvroReader::new(std::io::Cursor::new(bytes.as_ref()))
        .map_err(|e| ReaderError::Avro(e.to_string()))?;
    let writer_schema = reader.writer_schema().clone();

    let arrow_schema: SchemaRef = match schema_hint {
        Some(s) => SchemaRef::new(s),
        None => SchemaRef::new(arrow_schema_from_avro(&writer_schema)?),
    };

    let mut columns: Vec<Vec<Option<AvroValue>>> =
        vec![Vec::new(); arrow_schema.fields().len()];
    let field_names: Vec<&str> = arrow_schema
        .fields()
        .iter()
        .map(|f| f.name().as_str())
        .collect();

    let mut row_count = 0usize;
    for record in reader {
        let value = record.map_err(|e| ReaderError::Avro(e.to_string()))?;
        let entries = match value {
            AvroValue::Record(entries) => entries,
            other => {
                return Err(ReaderError::Avro(format!(
                    "expected Avro record at top level, got {other:?}"
                )));
            }
        };
        // Map by field name so column ordering matches the Arrow schema.
        let lookup: std::collections::HashMap<String, AvroValue> = entries.into_iter().collect();
        for (idx, name) in field_names.iter().enumerate() {
            columns[idx].push(lookup.get(*name).cloned());
        }
        row_count += 1;
        if let Some(cap) = limit {
            if row_count >= cap {
                break;
            }
        }
    }

    let mut arrays: Vec<ArrayRef> = Vec::with_capacity(columns.len());
    for (idx, column) in columns.into_iter().enumerate() {
        let dtype = arrow_schema.field(idx).data_type();
        arrays.push(build_array(dtype, &column)?);
    }

    let batch =
        RecordBatch::try_new(arrow_schema, arrays).map_err(|e| ReaderError::Arrow(e.to_string()))?;
    Ok(vec![batch])
}

/// Map an Avro writer schema (top-level Record) to an Arrow schema.
/// Foundry preview only needs the primitive overlap; we project unions
/// of `null + T` to nullable `T` and treat anything we don't understand
/// as `Utf8` so the preview surfaces a string fallback rather than 500.
fn arrow_schema_from_avro(writer: &WriterSchema) -> ReaderResult<ArrowSchema> {
    let fields = match writer {
        WriterSchema::Record(rec) => rec
            .fields
            .iter()
            .map(|f| {
                let (dtype, nullable) = avro_to_arrow(&f.schema);
                Field::new(&f.name, dtype, nullable)
            })
            .collect::<Vec<_>>(),
        other => {
            return Err(ReaderError::Avro(format!(
                "Avro top-level must be a Record, got {other:?}"
            )));
        }
    };
    Ok(ArrowSchema::new(fields))
}

fn avro_to_arrow(s: &WriterSchema) -> (DataType, bool) {
    use apache_avro::schema::Schema as A;
    match s {
        A::Null => (DataType::Null, true),
        A::Boolean => (DataType::Boolean, false),
        A::Int => (DataType::Int32, false),
        A::Long => (DataType::Int64, false),
        A::Float => (DataType::Float32, false),
        A::Double => (DataType::Float64, false),
        A::String | A::Enum(_) | A::Uuid => (DataType::Utf8, false),
        A::Bytes | A::Fixed(_) => (DataType::Binary, false),
        A::Union(u) => {
            // Foundry's Avro typically uses ["null", T] unions for
            // nullable columns; promote the non-null branch and mark
            // it nullable.
            let non_null = u.variants().iter().find(|v| !matches!(v, A::Null));
            match non_null {
                Some(branch) => {
                    let (dtype, _) = avro_to_arrow(branch);
                    (dtype, true)
                }
                None => (DataType::Null, true),
            }
        }
        // Fallback for nested types (Array/Map/Record): expose as Utf8
        // and let the preview JSON-stringify the value. A future PR can
        // promote these to Arrow List/Map/Struct on demand.
        _ => (DataType::Utf8, true),
    }
}

fn build_array(
    dtype: &DataType,
    values: &[Option<AvroValue>],
) -> ReaderResult<ArrayRef> {
    match dtype {
        DataType::Boolean => {
            let xs: Vec<Option<bool>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_bool)).collect();
            Ok(Arc::new(BooleanArray::from(xs)))
        }
        DataType::Int32 => {
            let xs: Vec<Option<i32>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_i32)).collect();
            Ok(Arc::new(Int32Array::from(xs)))
        }
        DataType::Int64 => {
            let xs: Vec<Option<i64>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_i64)).collect();
            Ok(Arc::new(Int64Array::from(xs)))
        }
        DataType::Float32 => {
            let xs: Vec<Option<f32>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_f32)).collect();
            Ok(Arc::new(Float32Array::from(xs)))
        }
        DataType::Float64 => {
            let xs: Vec<Option<f64>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_f64)).collect();
            Ok(Arc::new(Float64Array::from(xs)))
        }
        DataType::Utf8 => {
            let xs: Vec<Option<String>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_string)).collect();
            Ok(Arc::new(StringArray::from(xs)))
        }
        DataType::Binary => {
            let xs: Vec<Option<Vec<u8>>> =
                values.iter().map(|v| v.as_ref().and_then(coerce_bytes)).collect();
            // BinaryArray requires the exact owning shape: convert Vec<Option<Vec<u8>>>
            // into Vec<Option<&[u8]>> for the From impl.
            let refs: Vec<Option<&[u8]>> = xs.iter().map(|x| x.as_deref()).collect();
            Ok(Arc::new(BinaryArray::from(refs)))
        }
        DataType::Null => {
            // Build an all-null Utf8 column as a placeholder so preview
            // still returns a row count.
            let xs: Vec<Option<String>> = values.iter().map(|_| None).collect();
            Ok(Arc::new(StringArray::from(xs)))
        }
        other => Err(ReaderError::Avro(format!(
            "unsupported Avro→Arrow target type {other:?}"
        ))),
    }
}

fn unwrap_union(v: &AvroValue) -> &AvroValue {
    match v {
        AvroValue::Union(_, inner) => inner.as_ref(),
        other => other,
    }
}

fn coerce_bool(v: &AvroValue) -> Option<bool> {
    match unwrap_union(v) {
        AvroValue::Boolean(b) => Some(*b),
        AvroValue::Null => None,
        _ => None,
    }
}

fn coerce_i32(v: &AvroValue) -> Option<i32> {
    match unwrap_union(v) {
        AvroValue::Int(x) => Some(*x),
        AvroValue::Long(x) => i32::try_from(*x).ok(),
        AvroValue::Null => None,
        _ => None,
    }
}

fn coerce_i64(v: &AvroValue) -> Option<i64> {
    match unwrap_union(v) {
        AvroValue::Int(x) => Some(*x as i64),
        AvroValue::Long(x) => Some(*x),
        AvroValue::Null => None,
        _ => None,
    }
}

fn coerce_f32(v: &AvroValue) -> Option<f32> {
    match unwrap_union(v) {
        AvroValue::Float(x) => Some(*x),
        AvroValue::Double(x) => Some(*x as f32),
        AvroValue::Null => None,
        _ => None,
    }
}

fn coerce_f64(v: &AvroValue) -> Option<f64> {
    match unwrap_union(v) {
        AvroValue::Double(x) => Some(*x),
        AvroValue::Float(x) => Some(*x as f64),
        AvroValue::Null => None,
        _ => None,
    }
}

fn coerce_string(v: &AvroValue) -> Option<String> {
    match unwrap_union(v) {
        AvroValue::String(s) => Some(s.clone()),
        AvroValue::Enum(_, s) => Some(s.clone()),
        AvroValue::Uuid(u) => Some(u.to_string()),
        AvroValue::Null => None,
        // Last-resort string fallback for nested types we don't natively
        // map (Array/Map/Record). Keeps preview functional rather than
        // failing with "unsupported type".
        other => Some(format!("{other:?}")),
    }
}

fn coerce_bytes(v: &AvroValue) -> Option<Vec<u8>> {
    match unwrap_union(v) {
        AvroValue::Bytes(b) | AvroValue::Fixed(_, b) => Some(b.clone()),
        AvroValue::Null => None,
        _ => None,
    }
}
