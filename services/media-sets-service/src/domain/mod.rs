//! Domain layer: storage abstraction, path layout, dedup logic, errors.

pub mod cedar;
pub mod dedup;
pub mod error;
pub mod path;
pub mod retention;
pub mod storage;

pub use error::{MediaError, MediaResult};
pub use path::{MediaItemKey, storage_uri};
pub use retention::{ExpiredItem, drop_bytes, emit_audit, reap_due, reap_media_set, spawn_reaper};
pub use storage::{BackendMediaStorage, MediaStorage, PresignedUrl};
