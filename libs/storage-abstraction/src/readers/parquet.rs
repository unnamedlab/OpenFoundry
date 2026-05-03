//! Parquet reader. Re-exports the upstream `parquet` crate's Arrow
//! decoder behind the [`FileReader`] trait so the preview path can stay
//! format-agnostic. This keeps Parquet as the default Foundry format
//! (`Datasets.md` § "File formats") while letting us swap to a streaming
//! reader later by changing only this file.

use arrow_array_readers::RecordBatch;
use async_trait::async_trait;
use bytes::Bytes;
use parquet_readers::arrow::arrow_reader::ParquetRecordBatchReaderBuilder;

use super::{FileReader, FileUri, ReadOptions, ReaderError, ReaderResult};

#[derive(Debug, Default)]
pub struct ParquetFileReader;

#[async_trait]
impl FileReader for ParquetFileReader {
    async fn read(
        &self,
        _uri: &FileUri,
        bytes: Bytes,
        opts: ReadOptions,
    ) -> ReaderResult<Vec<RecordBatch>> {
        // Parquet's Arrow reader is sync; do the decode on a blocking
        // task so we don't park the tokio runtime on big files.
        let limit = opts.limit;
        tokio::task::spawn_blocking(move || decode_parquet(bytes, limit))
            .await
            .map_err(|e| ReaderError::Parquet(format!("blocking task panicked: {e}")))?
    }
}

fn decode_parquet(bytes: Bytes, limit: Option<usize>) -> ReaderResult<Vec<RecordBatch>> {
    let builder = ParquetRecordBatchReaderBuilder::try_new(bytes)
        .map_err(|e| ReaderError::Parquet(e.to_string()))?;
    let reader = builder
        .build()
        .map_err(|e| ReaderError::Parquet(e.to_string()))?;
    let mut out = Vec::new();
    let mut emitted = 0usize;
    for batch in reader {
        let batch = batch.map_err(|e| ReaderError::Parquet(e.to_string()))?;
        emitted += batch.num_rows();
        out.push(batch);
        if let Some(cap) = limit {
            if emitted >= cap {
                break;
            }
        }
    }
    Ok(out)
}
