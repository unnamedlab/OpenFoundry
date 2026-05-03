//! P2 — file-format readers (Parquet, Avro, Text/CSV+JSON-lines) used by
//! the dataset preview path.
//!
//! All readers consume bytes from a [`StorageBackend`] and produce Arrow
//! `RecordBatch`es so the upper layer (preview / Foundry-style row
//! payloads) can stay format-agnostic. The trait is intentionally
//! batch-oriented — preview limits the number of rows, so an eager Vec
//! suffices and we avoid the streaming complexity for now.
//!
//! Foundry's `Datasets.md` § "File formats" lists three first-party
//! formats: Parquet (default), Avro, and Text (CSV/TSV/JSON-lines).
//! `Schema.custom_metadata.csv` carries the Text-specific parsing
//! options (delimiter, quote, escape, header, null_value, …).
//!
//! ```text
//!   FileFormat → FileReader impl → Vec<RecordBatch>
//!   ┌────────────────────────────────────────────┐
//!   │  PARQUET → parquet::ParquetFileReader      │
//!   │  AVRO    → avro::AvroFileReader            │
//!   │  TEXT    → text::TextFileReader (CSV/JSON) │
//!   └────────────────────────────────────────────┘
//! ```

use arrow_array_readers::RecordBatch;
use arrow_schema_readers::Schema as ArrowSchema;
use async_trait::async_trait;

use crate::backend::StorageError;

pub mod avro;
pub mod parquet;
pub mod text;

pub use avro::AvroFileReader;
pub use parquet::ParquetFileReader;
pub use text::{CsvOptions, TextFileReader, TextSubFormat};

/// Storage URI. Today we accept plain paths (`/datasets/foo/file.parquet`)
/// because every backend in the workspace dereferences a path string;
/// keeping the input as a [`String`] makes the trait callable without
/// pulling `url::Url` into every callsite.
pub type FileUri = String;

/// Foundry "file format" enum, mirroring `proto/dataset/schema.proto`'s
/// `FileFormat` and `core_models::dataset::FileFormat`. Kept local to the
/// readers module so the storage crate doesn't take a dependency on
/// `core-models` (which would create an import cycle through the
/// dataset-versioning-service test harness).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FileFormat {
    Parquet,
    Avro,
    /// Text (CSV/TSV/JSON-lines). The actual sub-format is decided at
    /// read time by [`ReadOptions::text_sub_format`] (defaults to CSV)
    /// or by [`text::TextFileReader::with_sub_format`].
    Text,
}

impl FileFormat {
    pub fn parse(raw: &str) -> Self {
        match raw.trim().to_ascii_uppercase().as_str() {
            "AVRO" => FileFormat::Avro,
            "TEXT" | "CSV" | "TSV" | "JSON" | "JSON_LINES" | "JSONL" => FileFormat::Text,
            _ => FileFormat::Parquet,
        }
    }
}

/// Read-time options. The `schema` hint, when present, is used by the
/// CSV reader to skip Arrow's schema inference (Foundry's recommended
/// path: persist the schema once, read it back on every preview).
#[derive(Debug, Default)]
pub struct ReadOptions {
    /// Optional projected/explicit Arrow schema. When `None`, readers
    /// fall back to format-specific inference.
    pub schema: Option<ArrowSchema>,
    /// CSV / Text parsing options. Ignored for Parquet/Avro.
    pub csv: Option<CsvOptions>,
    /// `Some` forces the Text reader to a specific sub-format
    /// (`Csv` vs `JsonLines`); when `None` the reader sniffs the first
    /// non-whitespace byte (`{` ⇒ JSON-lines, anything else ⇒ CSV).
    pub text_sub_format: Option<TextSubFormat>,
    /// Maximum rows to materialise. The trait honours this best-effort:
    /// readers may return one extra batch worth of rows when the
    /// underlying decoder can't slice mid-batch.
    pub limit: Option<usize>,
}

#[derive(Debug, thiserror::Error)]
pub enum ReaderError {
    #[error("storage error: {0}")]
    Storage(#[from] StorageError),
    #[error("arrow error: {0}")]
    Arrow(String),
    #[error("avro error: {0}")]
    Avro(String),
    #[error("parquet error: {0}")]
    Parquet(String),
    #[error("invalid input: {0}")]
    Invalid(String),
}

pub type ReaderResult<T> = Result<T, ReaderError>;

/// Format-agnostic reader. Implementations are stateless: callers pass
/// the bytes (already fetched from the storage backend) plus options;
/// the reader returns a Vec of in-memory `RecordBatch`es.
#[async_trait]
pub trait FileReader: Send + Sync {
    async fn read(
        &self,
        uri: &FileUri,
        bytes: bytes::Bytes,
        opts: ReadOptions,
    ) -> ReaderResult<Vec<RecordBatch>>;
}

/// Build the right reader for a given Foundry file format. Returned as a
/// boxed trait object so the preview layer can stay format-agnostic.
pub fn dispatch_reader(format: FileFormat) -> Box<dyn FileReader> {
    match format {
        FileFormat::Parquet => Box::new(ParquetFileReader::default()),
        FileFormat::Avro => Box::new(AvroFileReader::default()),
        FileFormat::Text => Box::new(TextFileReader::default()),
    }
}

/// Re-export the canonical arrow-array crate under its expected name so
/// downstream callers (e.g. `dataset-versioning-service::storage::preview`)
/// don't have to thread the `arrow_array_readers` rename through their
/// imports. The `pub use` here is intentional — `arrow_array_readers` is
/// a Cargo `package = "arrow-array"` rename used to coexist with the
/// iceberg feature's arrow 57 dep tree.
pub use arrow_array_readers as arrow_array;
pub use arrow_schema_readers as arrow_schema;
