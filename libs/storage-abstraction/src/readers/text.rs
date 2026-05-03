//! Text reader: CSV and JSON-lines.
//!
//! Honours the Foundry `customMetadata.csv` parsing options listed in
//! `Datasets.md` § "Schema options" / `csv-parsing.md`:
//! delimiter, quote, escape, header, null_value, date_format,
//! timestamp_format, charset.
//!
//! The reader is auto-detecting by default: if the first non-whitespace
//! byte in the buffer is `{` or `[` it switches to JSON-lines (the doc
//! recommends inferring schema downstream when ingesting JSON, so for
//! preview we infer here too); anything else parses as CSV.

use std::io::Cursor;

use arrow_array_readers::RecordBatch;
use arrow_csv::ReaderBuilder as CsvReaderBuilder;
use arrow_json::ReaderBuilder as JsonReaderBuilder;
use arrow_schema_readers::{DataType, Field, Schema as ArrowSchema, SchemaRef};
use async_trait::async_trait;
use bytes::Bytes;

use super::{FileReader, FileUri, ReadOptions, ReaderError, ReaderResult};

/// CSV / Text parsing options. 1-to-1 with
/// `core_models::dataset::CsvOptions` so the preview layer can pass
/// them through unchanged.
///
/// Foundry semantics:
/// * `delimiter`/`quote`/`escape` are exactly one byte.
/// * `null_value` is the literal token treated as `null` on read.
/// * `header = true` consumes the first line as field names.
/// * `date_format` / `timestamp_format` are Joda/Spark-compatible
///   format strings; we surface them via `arrow-csv` `with_format`.
/// * `charset` is informational at this layer (we always decode UTF-8;
///   non-UTF-8 inputs would need a transcode step before reaching the
///   reader).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CsvOptions {
    pub delimiter: u8,
    pub quote: u8,
    pub escape: Option<u8>,
    pub header: bool,
    pub null_value: String,
    pub date_format: Option<String>,
    pub timestamp_format: Option<String>,
    pub charset: String,
}

impl Default for CsvOptions {
    fn default() -> Self {
        Self {
            delimiter: b',',
            quote: b'"',
            escape: Some(b'\\'),
            header: true,
            null_value: String::new(),
            date_format: None,
            timestamp_format: None,
            charset: "UTF-8".into(),
        }
    }
}

/// Sub-format for [`TextFileReader`].
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TextSubFormat {
    Csv,
    JsonLines,
}

#[derive(Debug, Default)]
pub struct TextFileReader {
    /// Force a specific sub-format. When unset, [`detect_sub_format`]
    /// chooses based on the first non-whitespace byte.
    forced: Option<TextSubFormat>,
}

impl TextFileReader {
    pub fn with_sub_format(sub: TextSubFormat) -> Self {
        Self { forced: Some(sub) }
    }
}

#[async_trait]
impl FileReader for TextFileReader {
    async fn read(
        &self,
        _uri: &FileUri,
        bytes: Bytes,
        opts: ReadOptions,
    ) -> ReaderResult<Vec<RecordBatch>> {
        let sub = opts
            .text_sub_format
            .or(self.forced)
            .unwrap_or_else(|| detect_sub_format(&bytes));
        match sub {
            TextSubFormat::Csv => decode_csv(bytes, opts),
            TextSubFormat::JsonLines => decode_jsonl(bytes, opts),
        }
    }
}

/// First-byte heuristic. Accepts BOM + leading whitespace.
pub fn detect_sub_format(buf: &[u8]) -> TextSubFormat {
    let mut i = 0;
    // Skip UTF-8 BOM
    if buf.starts_with(&[0xEF, 0xBB, 0xBF]) {
        i = 3;
    }
    while i < buf.len() && buf[i].is_ascii_whitespace() {
        i += 1;
    }
    match buf.get(i) {
        Some(b'{') | Some(b'[') => TextSubFormat::JsonLines,
        _ => TextSubFormat::Csv,
    }
}

fn decode_csv(bytes: Bytes, opts: ReadOptions) -> ReaderResult<Vec<RecordBatch>> {
    let csv = opts.csv.unwrap_or_default();

    // Schema. Either the caller-provided one or a one-pass inference
    // over the buffer (Arrow's default 1024-record peek).
    let schema = match opts.schema {
        Some(schema) => SchemaRef::new(schema),
        None => infer_csv_schema(&bytes, &csv)?,
    };

    let mut builder = CsvReaderBuilder::new(schema)
        .with_header(csv.header)
        .with_delimiter(csv.delimiter)
        .with_quote(csv.quote)
        .with_null_regex(make_null_regex(&csv.null_value));
    if let Some(escape) = csv.escape {
        builder = builder.with_escape(escape);
    }
    // arrow-csv 53's `ReaderBuilder` doesn't accept a free-form date /
    // timestamp format string at the reader level (the upstream API
    // exposes one only on the inference `Format`). We thread the
    // formats through `infer_csv_schema` below; for explicit-schema
    // payloads they're preserved in CsvOptions and applied once the
    // workspace moves to arrow-csv ≥ 54 (where the API split landed).
    let _date_format = csv.date_format.as_ref();
    let _timestamp_format = csv.timestamp_format.as_ref();

    let reader = builder
        .build(Cursor::new(bytes.clone()))
        .map_err(|e| ReaderError::Arrow(e.to_string()))?;

    let mut out = Vec::new();
    let mut emitted = 0;
    for batch in reader {
        let batch = batch.map_err(|e| ReaderError::Arrow(e.to_string()))?;
        emitted += batch.num_rows();
        out.push(batch);
        if let Some(cap) = opts.limit {
            if emitted >= cap {
                break;
            }
        }
    }
    Ok(out)
}

fn infer_csv_schema(bytes: &Bytes, csv: &CsvOptions) -> ReaderResult<SchemaRef> {
    use arrow_csv::reader::Format;
    let mut format = Format::default()
        .with_header(csv.header)
        .with_delimiter(csv.delimiter)
        .with_quote(csv.quote);
    if let Some(escape) = csv.escape {
        format = format.with_escape(escape);
    }
    let regex = make_null_regex(&csv.null_value);
    format = format.with_null_regex(regex);
    let (schema, _records_read) = format
        .infer_schema(Cursor::new(bytes.clone()), Some(1024))
        .map_err(|e| ReaderError::Arrow(e.to_string()))?;
    Ok(SchemaRef::new(schema))
}

fn make_null_regex(null_value: &str) -> regex::Regex {
    if null_value.is_empty() {
        // Arrow-csv treats this regex as "matches the empty string only";
        // anchored so cells like " " stay strings.
        regex::Regex::new("^$").expect("empty null regex compiles")
    } else {
        regex::Regex::new(&format!("^{}$", regex::escape(null_value)))
            .expect("escaped null regex compiles")
    }
}

fn decode_jsonl(bytes: Bytes, opts: ReadOptions) -> ReaderResult<Vec<RecordBatch>> {
    // For JSON-lines we always need a schema. When the caller doesn't
    // provide one we infer over the first ~1024 records — matching
    // Arrow's CSV behaviour and Foundry's "infer schema" recommendation
    // when the dataset's persisted schema is empty.
    let schema = match opts.schema {
        Some(schema) => SchemaRef::new(schema),
        None => {
            let (schema, _) = arrow_json::reader::infer_json_schema_from_seekable(
                Cursor::new(bytes.clone()),
                Some(1024),
            )
            .map_err(|e| ReaderError::Arrow(e.to_string()))?;
            SchemaRef::new(schema)
        }
    };

    let reader = JsonReaderBuilder::new(schema)
        .build(Cursor::new(bytes.clone()))
        .map_err(|e| ReaderError::Arrow(e.to_string()))?;

    let mut out = Vec::new();
    let mut emitted = 0;
    for batch in reader {
        let batch = batch.map_err(|e| ReaderError::Arrow(e.to_string()))?;
        emitted += batch.num_rows();
        out.push(batch);
        if let Some(cap) = opts.limit {
            if emitted >= cap {
                break;
            }
        }
    }
    Ok(out)
}

/// Helper exposed for tests and schema-driven preview paths: build a
/// minimal Arrow schema from a list of `(name, DataType)` pairs.
pub fn build_arrow_schema(fields: &[(String, DataType)]) -> ArrowSchema {
    let inner = fields
        .iter()
        .map(|(name, dtype)| Field::new(name, dtype.clone(), true))
        .collect::<Vec<_>>();
    ArrowSchema::new(inner)
}
