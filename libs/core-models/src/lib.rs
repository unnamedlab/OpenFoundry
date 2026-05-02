//! `core-models` — primitives shared across every OpenFoundry service.
//!
//! Re-exports the dataset-side canonical types (RIDs, branch names,
//! transaction states) used by `dataset-versioning-service`,
//! `pipeline-build-service`, the catalog and the ML stack.

pub mod dataset;
pub mod error;
pub mod health;
pub mod ids;
pub mod observability;
pub mod pagination;
pub mod security;
pub mod timestamp;

pub use dataset::{
    BranchName, DatasetRid, InvalidBranchName, InvalidDatasetRid, TransactionId,
    TransactionState, TransactionType, UnknownTransactionState, UnknownTransactionType,
};
pub use security::{EffectiveMarking, InvalidMarkingId, MarkingId, MarkingSource};
