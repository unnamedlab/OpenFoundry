//! P2 — view-scoped dataset preview.
//!
//! Reads the most-recent file in a view, dispatches to the right
//! [`storage_abstraction::readers::FileReader`] for the view's
//! `file_format`, and turns the resulting Arrow `RecordBatch`es into a
//! JSON columnar payload the SchemaViewer / DataPreview UI consumes.
//!
//! Foundry semantics (`Datasets.md` § "File formats", `csv-parsing.md`):
//! * Parquet: typed by the file footer; schema hint is optional.
//! * Avro: writer schema embedded in the file; preview maps it to Arrow.
//! * Text (CSV / JSON-lines): driven by `customMetadata.csv`. When the
//!   persisted schema is empty we infer types over the first ~1024
//!   records and flag `schema_inferred = true` so the UI can offer a
//!   "Schema not configured" banner.
//!
//! Override knobs (handled by the HTTP layer and forwarded here as
//! [`PreviewOverrides`]) let callers force a format or tweak CSV
//! parsing without re-saving the schema first — useful when a Text
//! dataset was registered with the wrong delimiter.

use std::collections::BTreeMap;
use std::sync::Arc;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sqlx::PgPool;
use storage_abstraction::StorageBackend;
use storage_abstraction::readers::arrow_array::{
    Array, BinaryArray, BooleanArray, Float32Array, Float64Array, Int16Array, Int32Array,
    Int64Array, Int8Array, RecordBatch, StringArray,
};
use storage_abstraction::readers::arrow_schema::{DataType, Schema as ArrowSchema};
use storage_abstraction::readers::{
    CsvOptions as ReaderCsvOptions, FileFormat, ReadOptions, ReaderError, TextSubFormat,
    dispatch_reader,
};
use uuid::Uuid;

use crate::models::schema::{
    CsvOptions as ModelCsvOptions, DatasetSchema, FileFormat as ModelFileFormat,
};

/// Fully-resolved preview payload returned by [`read_view_preview`].
#[derive(Debug, Clone, Serialize)]
pub struct PreviewPage {
    pub view_id: Uuid,
    pub dataset_id: Uuid,
    pub branch: Option<String>,
    pub file_format: String,
    /// `csv` / `json_lines` / `null`. Always set when `file_format = TEXT`.
    pub text_sub_format: Option<&'static str>,
    pub limit: usize,
    pub offset: usize,
    pub row_count: usize,
    pub total_rows: usize,
    pub columns: Vec<PreviewColumn>,
    pub rows: Vec<Value>,
    /// True when no persisted schema was available and the reader fell
    /// back to inference. The UI surfaces this as a banner.
    pub schema_inferred: bool,
    /// CSV options effectively used (after override / default merge).
    /// Only populated when `file_format = TEXT`.
    pub csv_options: Option<EffectiveCsvOptions>,
    pub warnings: Vec<String>,
    pub errors: Vec<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct PreviewColumn {
    pub name: String,
    pub field_type: String,
    pub nullable: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct EffectiveCsvOptions {
    pub delimiter: String,
    pub quote: String,
    pub escape: String,
    pub header: bool,
    pub null_value: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub date_format: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub timestamp_format: Option<String>,
    pub charset: String,
}

/// Per-call format / CSV overrides. None of these are persisted; they
/// only apply to this preview invocation.
#[derive(Debug, Default, Clone)]
pub struct PreviewOverrides {
    /// `auto | parquet | avro | text`. `auto` defers to the schema row.
    pub format: Option<FormatOverride>,
    pub csv_delimiter: Option<String>,
    pub csv_quote: Option<String>,
    pub csv_escape: Option<String>,
    pub csv_header: Option<bool>,
    pub csv_null_value: Option<String>,
    pub csv_charset: Option<String>,
    pub csv_date_format: Option<String>,
    pub csv_timestamp_format: Option<String>,
    /// Force CSV vs JSON-lines for TEXT dispatch. `Some(false)` ⇒ JSON-lines.
    pub csv: Option<bool>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FormatOverride {
    Auto,
    Parquet,
    Avro,
    Text,
}

impl FormatOverride {
    pub fn parse(raw: &str) -> Option<Self> {
        match raw.trim().to_ascii_lowercase().as_str() {
            "auto" => Some(Self::Auto),
            "parquet" => Some(Self::Parquet),
            "avro" => Some(Self::Avro),
            "text" | "csv" | "json" | "json_lines" | "jsonl" => Some(Self::Text),
            _ => None,
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum PreviewError {
    #[error("view not found")]
    ViewNotFound,
    #[error("view has no files")]
    ViewEmpty,
    #[error("storage error: {0}")]
    Storage(String),
    #[error("reader error: {0}")]
    Reader(#[from] ReaderError),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("internal error: {0}")]
    Internal(String),
}

/// Top-level entry point invoked by the preview handler.
pub async fn read_view_preview(
    db: &PgPool,
    storage: Arc<dyn StorageBackend>,
    view_id: Uuid,
    limit: usize,
    offset: usize,
    overrides: PreviewOverrides,
) -> Result<PreviewPage, PreviewError> {
    let limit = limit.clamp(1, 10_000);
    // 1) Load the schema row for the view (if any). Empty schema ⇒
    //    inference path. Branch label is purely informational.
    let stored = load_view_schema(db, view_id).await?;
    let (dataset_id, branch) = lookup_view_meta(db, view_id).await?;

    // 2) Decide which file format to dispatch to.
    let format = resolve_format(&stored, overrides.format);
    let csv_effective = build_effective_csv(&stored, &overrides);

    // 3) Resolve files in this view.
    let files = list_view_files(db, view_id).await?;
    if files.is_empty() {
        return Err(PreviewError::ViewEmpty);
    }
    // Foundry preview reads the *most recently introduced* file so users
    // see the latest sample after an APPEND. For multi-file SNAPSHOTs we
    // pick the lexicographically last one — deterministic and cheap.
    let file = files
        .into_iter()
        .max_by(|a, b| {
            a.introduced_by
                .cmp(&b.introduced_by)
                .then_with(|| a.logical_path.cmp(&b.logical_path))
        })
        .expect("non-empty after check");

    // 4) Fetch bytes through the storage backend.
    let bytes = storage
        .get(&file.physical_path)
        .await
        .map_err(|e| PreviewError::Storage(e.to_string()))?;

    // 5) Build read options + dispatch to the right reader.
    let arrow_schema_hint = stored
        .as_ref()
        .filter(|stored| !stored.schema.fields.is_empty() && format != FileFormat::Text)
        .map(|stored| stored.schema.to_arrow_schema());
    let reader_csv = csv_effective.as_ref().map(|c| reader_csv_options(c));
    let text_sub_format = match format {
        FileFormat::Text => Some(resolve_text_sub_format(&overrides)),
        _ => None,
    };
    let read_opts = ReadOptions {
        schema: arrow_schema_hint,
        csv: reader_csv,
        text_sub_format,
        limit: Some(limit + offset),
    };
    let reader = dispatch_reader(format);
    let batches = reader
        .read(&file.physical_path, bytes, read_opts)
        .await
        .map_err(PreviewError::Reader)?;

    // 6) Convert batches → JSON columnar. Apply offset/limit at this
    //    layer so a 50-row preview over a 1M-row file doesn't allocate
    //    1M JSON objects.
    let (columns, rows, total_rows) =
        materialise_rows(&batches, offset, limit, &stored, format)?;

    let schema_inferred = stored
        .as_ref()
        .map(|s| s.schema.fields.is_empty())
        .unwrap_or(true);

    let text_sub_format_label = text_sub_format.map(|s| match s {
        TextSubFormat::Csv => "csv",
        TextSubFormat::JsonLines => "json_lines",
    });

    Ok(PreviewPage {
        view_id,
        dataset_id,
        branch,
        file_format: file_format_label(format).to_string(),
        text_sub_format: text_sub_format_label,
        limit,
        offset,
        row_count: rows.len(),
        total_rows,
        columns,
        rows,
        schema_inferred,
        csv_options: csv_effective,
        warnings: Vec::new(),
        errors: Vec::new(),
    })
}

// ─────────────────────────────────────────────────────────────────────────────
// Schema + file helpers
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
struct StoredSchema {
    schema: DatasetSchema,
    file_format: ModelFileFormat,
}

async fn load_view_schema(
    db: &PgPool,
    view_id: Uuid,
) -> Result<Option<StoredSchema>, PreviewError> {
    #[derive(sqlx::FromRow)]
    struct Row {
        schema_json: Value,
        file_format: String,
        custom_metadata: Option<Value>,
    }
    let row = sqlx::query_as::<_, Row>(
        r#"SELECT schema_json, file_format, custom_metadata
             FROM dataset_view_schemas
            WHERE view_id = $1"#,
    )
    .bind(view_id)
    .fetch_optional(db)
    .await?;

    let row = match row {
        Some(r) => r,
        None => return Ok(None),
    };
    let mut schema: DatasetSchema = serde_json::from_value(row.schema_json)
        .map_err(|e| PreviewError::Internal(format!("schema_json decode: {e}")))?;
    let file_format = parse_model_format(&row.file_format);
    schema.file_format = file_format;
    if let Some(meta) = row.custom_metadata {
        if !meta.is_null() {
            schema.custom_metadata = serde_json::from_value(meta)
                .map_err(|e| PreviewError::Internal(format!("custom_metadata decode: {e}")))?;
        }
    }
    Ok(Some(StoredSchema {
        schema,
        file_format,
    }))
}

#[derive(Debug, Clone)]
struct ViewFile {
    logical_path: String,
    physical_path: String,
    introduced_by: Option<DateTime<Utc>>,
}

async fn list_view_files(
    db: &PgPool,
    view_id: Uuid,
) -> Result<Vec<ViewFile>, PreviewError> {
    #[derive(sqlx::FromRow)]
    struct Row {
        logical_path: String,
        physical_path: String,
        committed_at: Option<DateTime<Utc>>,
    }
    let rows = sqlx::query_as::<_, Row>(
        r#"SELECT vf.logical_path,
                  vf.physical_path,
                  t.committed_at
             FROM dataset_view_files vf
             LEFT JOIN dataset_transactions t ON t.id = vf.introduced_by
            WHERE vf.view_id = $1
            ORDER BY t.committed_at NULLS LAST, vf.logical_path"#,
    )
    .bind(view_id)
    .fetch_all(db)
    .await?;
    Ok(rows
        .into_iter()
        .map(|r| ViewFile {
            logical_path: r.logical_path,
            physical_path: r.physical_path,
            introduced_by: r.committed_at,
        })
        .collect())
}

async fn lookup_view_meta(
    db: &PgPool,
    view_id: Uuid,
) -> Result<(Uuid, Option<String>), PreviewError> {
    let row = sqlx::query_as::<_, (Uuid, String)>(
        r#"SELECT v.dataset_id, b.name
             FROM dataset_views v
             JOIN dataset_branches b ON b.id = v.branch_id
            WHERE v.id = $1"#,
    )
    .bind(view_id)
    .fetch_optional(db)
    .await?;
    let (dataset_id, branch) = row.ok_or(PreviewError::ViewNotFound)?;
    Ok((dataset_id, Some(branch)))
}

fn parse_model_format(raw: &str) -> ModelFileFormat {
    match raw.to_ascii_uppercase().as_str() {
        "AVRO" => ModelFileFormat::Avro,
        "TEXT" => ModelFileFormat::Text,
        _ => ModelFileFormat::Parquet,
    }
}

fn resolve_format(
    stored: &Option<StoredSchema>,
    override_value: Option<FormatOverride>,
) -> FileFormat {
    match override_value {
        Some(FormatOverride::Parquet) => FileFormat::Parquet,
        Some(FormatOverride::Avro) => FileFormat::Avro,
        Some(FormatOverride::Text) => FileFormat::Text,
        Some(FormatOverride::Auto) | None => match stored {
            Some(s) => match s.file_format {
                ModelFileFormat::Parquet => FileFormat::Parquet,
                ModelFileFormat::Avro => FileFormat::Avro,
                ModelFileFormat::Text => FileFormat::Text,
            },
            None => FileFormat::Parquet,
        },
    }
}

fn file_format_label(f: FileFormat) -> &'static str {
    match f {
        FileFormat::Parquet => "PARQUET",
        FileFormat::Avro => "AVRO",
        FileFormat::Text => "TEXT",
    }
}

fn resolve_text_sub_format(overrides: &PreviewOverrides) -> TextSubFormat {
    match overrides.csv {
        Some(true) => TextSubFormat::Csv,
        Some(false) => TextSubFormat::JsonLines,
        // None → let the reader sniff based on first byte. We use Csv as
        // the default sentinel; the reader's `with_sub_format` is only
        // honoured when the option is explicitly set, so passing Csv
        // here without `forced` keeps the auto-detect.
        None => TextSubFormat::Csv,
    }
}

fn build_effective_csv(
    stored: &Option<StoredSchema>,
    overrides: &PreviewOverrides,
) -> Option<EffectiveCsvOptions> {
    // Pull the persisted CsvOptions (if the schema was TEXT and had
    // them); fall back to defaults so we always have a baseline to
    // override.
    let stored_csv = stored
        .as_ref()
        .and_then(|s| s.schema.custom_metadata.as_ref())
        .and_then(|meta| meta.csv.as_ref())
        .cloned();
    let base = stored_csv.unwrap_or_else(default_model_csv);

    let mut opts = EffectiveCsvOptions::from(base);

    if let Some(d) = &overrides.csv_delimiter {
        opts.delimiter = d.clone();
    }
    if let Some(q) = &overrides.csv_quote {
        opts.quote = q.clone();
    }
    if let Some(e) = &overrides.csv_escape {
        opts.escape = e.clone();
    }
    if let Some(h) = overrides.csv_header {
        opts.header = h;
    }
    if let Some(nv) = &overrides.csv_null_value {
        opts.null_value = nv.clone();
    }
    if let Some(c) = &overrides.csv_charset {
        opts.charset = c.clone();
    }
    if let Some(d) = &overrides.csv_date_format {
        opts.date_format = Some(d.clone());
    }
    if let Some(t) = &overrides.csv_timestamp_format {
        opts.timestamp_format = Some(t.clone());
    }

    Some(opts)
}

fn default_model_csv() -> ModelCsvOptions {
    ModelCsvOptions {
        delimiter: ",".into(),
        quote: "\"".into(),
        escape: "\\".into(),
        header: true,
        null_value: String::new(),
        date_format: None,
        timestamp_format: None,
        charset: "UTF-8".into(),
    }
}

impl From<ModelCsvOptions> for EffectiveCsvOptions {
    fn from(c: ModelCsvOptions) -> Self {
        Self {
            delimiter: c.delimiter,
            quote: c.quote,
            escape: c.escape,
            header: c.header,
            null_value: c.null_value,
            date_format: c.date_format,
            timestamp_format: c.timestamp_format,
            charset: c.charset,
        }
    }
}

fn reader_csv_options(eff: &EffectiveCsvOptions) -> ReaderCsvOptions {
    ReaderCsvOptions {
        delimiter: first_byte(&eff.delimiter, b','),
        quote: first_byte(&eff.quote, b'"'),
        escape: if eff.escape.is_empty() {
            None
        } else {
            Some(first_byte(&eff.escape, b'\\'))
        },
        header: eff.header,
        null_value: eff.null_value.clone(),
        date_format: eff.date_format.clone(),
        timestamp_format: eff.timestamp_format.clone(),
        charset: eff.charset.clone(),
    }
}

fn first_byte(s: &str, fallback: u8) -> u8 {
    s.as_bytes().first().copied().unwrap_or(fallback)
}

// ─────────────────────────────────────────────────────────────────────────────
// RecordBatch → JSON
// ─────────────────────────────────────────────────────────────────────────────

fn materialise_rows(
    batches: &[RecordBatch],
    offset: usize,
    limit: usize,
    stored: &Option<StoredSchema>,
    format: FileFormat,
) -> Result<(Vec<PreviewColumn>, Vec<Value>, usize), PreviewError> {
    if batches.is_empty() {
        return Ok((Vec::new(), Vec::new(), 0));
    }
    let schema = batches[0].schema();
    let columns = describe_columns(&schema, stored, format);

    let total_rows: usize = batches.iter().map(|b| b.num_rows()).sum();

    let mut rows = Vec::with_capacity(limit.min(total_rows));
    let mut skipped = 0usize;
    for batch in batches {
        if rows.len() >= limit {
            break;
        }
        let n = batch.num_rows();
        // Skip into this batch if necessary.
        let local_start = if skipped + n <= offset {
            skipped += n;
            continue;
        } else {
            offset.saturating_sub(skipped)
        };
        skipped += local_start;

        for row_idx in local_start..n {
            if rows.len() >= limit {
                break;
            }
            let mut obj = Map::new();
            for (col_idx, field) in batch.schema().fields().iter().enumerate() {
                let value = array_value_to_json(batch.column(col_idx).as_ref(), row_idx);
                obj.insert(field.name().clone(), value);
            }
            rows.push(Value::Object(obj));
        }
    }

    Ok((columns, rows, total_rows))
}

fn describe_columns(
    schema: &Arc<ArrowSchema>,
    stored: &Option<StoredSchema>,
    _format: FileFormat,
) -> Vec<PreviewColumn> {
    // If the persisted schema has a (matching) field, surface its
    // Foundry-style name (e.g. "DECIMAL(38,18)") so the UI badge stays
    // consistent with what the user saved. Otherwise fall back to the
    // Arrow type pretty-print.
    let by_name: BTreeMap<&str, &crate::models::schema::Field> = stored
        .as_ref()
        .map(|s| {
            s.schema
                .fields
                .iter()
                .map(|f| (f.name.as_str(), f))
                .collect()
        })
        .unwrap_or_default();

    schema
        .fields()
        .iter()
        .map(|f| {
            let name = f.name().clone();
            let foundry = by_name.get(name.as_str()).map(|field| {
                format_foundry_type(field)
            });
            PreviewColumn {
                name: name.clone(),
                field_type: foundry.unwrap_or_else(|| arrow_type_name(f.data_type())),
                nullable: f.is_nullable(),
            }
        })
        .collect()
}

fn format_foundry_type(field: &crate::models::schema::Field) -> String {
    use crate::models::schema::FieldType;
    match &field.field_type {
        FieldType::Decimal { precision, scale } => format!(
            "DECIMAL({},{})",
            precision.unwrap_or(38),
            scale.unwrap_or(18)
        ),
        FieldType::Array { .. } => "ARRAY".into(),
        FieldType::Map { .. } => "MAP".into(),
        FieldType::Struct { .. } => "STRUCT".into(),
        ft => format!("{ft:?}").to_uppercase(),
    }
}

fn arrow_type_name(dtype: &DataType) -> String {
    format!("{dtype}")
}

fn array_value_to_json(array: &dyn Array, row: usize) -> Value {
    if array.is_null(row) {
        return Value::Null;
    }
    if let Some(a) = array.as_any().downcast_ref::<BooleanArray>() {
        return Value::Bool(a.value(row));
    }
    if let Some(a) = array.as_any().downcast_ref::<Int8Array>() {
        return Value::Number(a.value(row).into());
    }
    if let Some(a) = array.as_any().downcast_ref::<Int16Array>() {
        return Value::Number(a.value(row).into());
    }
    if let Some(a) = array.as_any().downcast_ref::<Int32Array>() {
        return Value::Number(a.value(row).into());
    }
    if let Some(a) = array.as_any().downcast_ref::<Int64Array>() {
        return Value::Number(a.value(row).into());
    }
    if let Some(a) = array.as_any().downcast_ref::<Float32Array>() {
        return serde_json::Number::from_f64(a.value(row) as f64)
            .map(Value::Number)
            .unwrap_or(Value::Null);
    }
    if let Some(a) = array.as_any().downcast_ref::<Float64Array>() {
        return serde_json::Number::from_f64(a.value(row))
            .map(Value::Number)
            .unwrap_or(Value::Null);
    }
    if let Some(a) = array.as_any().downcast_ref::<StringArray>() {
        return Value::String(a.value(row).to_string());
    }
    if let Some(a) = array.as_any().downcast_ref::<BinaryArray>() {
        // base64-ish but cheaper: hex for preview readability.
        let bytes = a.value(row);
        return Value::String(
            bytes
                .iter()
                .map(|b| format!("{:02x}", b))
                .collect::<String>(),
        );
    }
    // Fallback for Arrow types we don't natively project (List, Map,
    // Struct, Date32, Timestamp, …): emit a debug-formatted string so
    // the preview row still renders something readable rather than 500.
    Value::String(format!("{:?}", array.slice(row, 1)))
}
