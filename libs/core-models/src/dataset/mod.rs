//! Dataset-related canonical domain primitives.
//!
//! Sub-modules expose the typed building blocks shared by every service
//! that touches datasets (catalog, versioning, ingestion, ML, pipelines).

pub mod schema;
pub mod transaction;

pub use schema::{
    CsvOptions, FieldType, Schema, SchemaField, SchemaValidationError,
};
pub use transaction::{
    BranchName, DatasetRid, InvalidBranchName, InvalidDatasetRid, TransactionId,
    TransactionState, TransactionType, UnknownTransactionState, UnknownTransactionType,
};
