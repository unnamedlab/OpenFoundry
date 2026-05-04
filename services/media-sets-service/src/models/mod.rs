//! Wire DTOs (REST request/response shapes) and Postgres row structs.

pub mod access_pattern;
pub mod branch;
pub mod media_item;
pub mod media_set;
pub mod transaction;

pub use access_pattern::{
    AccessPattern, AccessPatternRunResponse, PersistencePolicy, RegisterAccessPatternBody,
};

pub use branch::{
    CreateBranchBody, MediaSetBranch, MergeBranchBody, MergeBranchResponse, MergeResolution,
    ResetBranchResponse,
};
pub use media_item::{MediaItem, NewMediaItem, PresignedUploadRequest, PresignedUrlBody};
pub use media_set::{
    CreateMediaSetRequest, ListMediaSetsQuery, MediaSet, MediaSetSchema, TransactionPolicy,
};
pub use transaction::{
    MediaSetTransaction, OpenTransactionBody, TransactionHistoryEntry, TransactionState, WriteMode,
};
