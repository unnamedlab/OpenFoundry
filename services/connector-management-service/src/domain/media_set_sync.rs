//! Media-set sync configuration, filters and per-file decisions.
//!
//! This module is the pure-Rust core that backs the
//! `POST /sources/{rid}/media-set-syncs` route in
//! [`crate::handlers::data_connection`] and the executor that pushes
//! enumerated source files to `media-sets-service`.
//!
//! The Foundry contract lives in
//! `docs_original_palantir_foundry/.../Set up a media set sync.md`.
//! Two flavours exist:
//!
//! * `MEDIA_SET_SYNC` — bytes are copied into Foundry via
//!   `POST /media-sets/{rid}/items/upload-url` on `media-sets-service`.
//! * `VIRTUAL_MEDIA_SET_SYNC` — only the pointer is registered, via
//!   `POST /media-sets/{rid}/virtual-items`. No bytes ever land in
//!   Foundry storage (see *Virtual media sets.md* limitations).
//!
//! All non-trivial logic that the integration test exercises lives
//! here as side-effect-free functions so the test can pull the module
//! in via `#[path]` without dragging the whole service in.

use std::collections::HashSet;
use std::str::FromStr;

use globset::{Glob, GlobMatcher};
use serde::{Deserialize, Serialize};

/// The two sync flavours for media-bearing sources.
///
/// Serialised as the upper-snake-case strings the docs use, which is
/// also what the discriminator column on `media_set_syncs.sync_type`
/// stores verbatim.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MediaSetSyncKind {
    /// Bytes are copied into Foundry's backing store.
    MediaSetSync,
    /// Bytes stay in the external system; only metadata is registered.
    VirtualMediaSetSync,
}

impl MediaSetSyncKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::MediaSetSync => "MEDIA_SET_SYNC",
            Self::VirtualMediaSetSync => "VIRTUAL_MEDIA_SET_SYNC",
        }
    }
}

/// Per-Foundry-doc sync filters. Defaults all reject nothing; filters
/// are only applied when set explicitly.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct MediaSetSyncFilters {
    /// "Exclude files already synced" toggle from the docs.
    pub exclude_already_synced: bool,
    /// "Path matches" glob (e.g. `**/*.png`).
    pub path_glob: Option<String>,
    /// "File size limit" in bytes (any file strictly above this is skipped).
    pub file_size_limit: Option<u64>,
    /// "Ignore items not matching schema" — when true, files whose MIME
    /// is not in the target media set's `allowed_mime_types` are
    /// skipped silently. When false, the same files are surfaced as
    /// per-file errors so the operator notices.
    pub ignore_unmatched_schema: bool,
}

/// Wire-format config persisted on `media_set_syncs.filters`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MediaSetSyncConfig {
    pub kind: MediaSetSyncKind,
    pub target_media_set_rid: String,
    /// Subfolder inside the source bucket (empty = bucket root).
    #[serde(default)]
    pub subfolder: String,
    #[serde(default)]
    pub filters: MediaSetSyncFilters,
    /// Optional cron schedule. When absent the sync only runs on demand.
    #[serde(default)]
    pub schedule_cron: Option<String>,
}

impl MediaSetSyncConfig {
    /// Validate the config independent of any source. Returns the list of
    /// human-readable errors (empty = valid).
    pub fn validate(&self) -> Vec<String> {
        let mut errors = Vec::new();
        if !self
            .target_media_set_rid
            .starts_with("ri.foundry.main.media_set.")
        {
            errors.push(format!(
                "target_media_set_rid `{}` must start with `ri.foundry.main.media_set.`",
                self.target_media_set_rid
            ));
        }
        if let Some(glob) = &self.filters.path_glob {
            if let Err(err) = Glob::new(glob) {
                errors.push(format!("invalid path_glob `{glob}`: {err}"));
            }
        }
        if let Some(limit) = self.filters.file_size_limit {
            if limit == 0 {
                errors.push("file_size_limit must be > 0".into());
            }
        }
        if let Some(cron_expr) = &self.schedule_cron {
            if let Err(err) = cron::Schedule::from_str(cron_expr) {
                errors.push(format!("invalid schedule_cron `{cron_expr}`: {err}"));
            }
        }
        errors
    }

    /// Compile the path glob once so callers can re-use the matcher
    /// across many files.
    pub fn matcher(&self) -> Result<Option<GlobMatcher>, String> {
        match self.filters.path_glob.as_deref() {
            Some(glob) => Glob::new(glob)
                .map(|g| Some(g.compile_matcher()))
                .map_err(|e| e.to_string()),
            None => Ok(None),
        }
    }
}

/// Minimal description of a file enumerated from the source bucket.
#[derive(Debug, Clone)]
pub struct SourceFile {
    pub path: String,
    pub size_bytes: u64,
    pub mime_type: String,
}

/// Outcome of applying the configured filters to a single source file.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyncDecision {
    /// File passes all filters and should be ingested.
    Accept,
    /// File is intentionally skipped (already synced, glob mismatch,
    /// over the size limit, or schema-mismatched with
    /// `ignore_unmatched_schema = true`).
    Skip,
    /// File's MIME does not match `allowed_mime_types` and the user
    /// asked us to surface that as an error rather than silently skip.
    /// The caller maps this to a per-file failure record on the run.
    SchemaMismatch,
}

/// Decide what to do with a single file under the sync's filter set.
///
/// `already_synced` carries the set of paths the previous run already
/// landed (only consulted when `filters.exclude_already_synced = true`).
/// `allowed_mime_types` is the parent media set's `allowed_mime_types`
/// list; when empty all MIME types are accepted.
pub fn decide(
    cfg: &MediaSetSyncConfig,
    file: &SourceFile,
    already_synced: &HashSet<String>,
    allowed_mime_types: &[String],
    matcher: Option<&GlobMatcher>,
) -> SyncDecision {
    if cfg.filters.exclude_already_synced && already_synced.contains(&file.path) {
        return SyncDecision::Skip;
    }
    if let Some(limit) = cfg.filters.file_size_limit {
        if file.size_bytes > limit {
            return SyncDecision::Skip;
        }
    }
    if let Some(matcher) = matcher {
        if !matcher.is_match(&file.path) {
            return SyncDecision::Skip;
        }
    }
    if !allowed_mime_types.is_empty()
        && !allowed_mime_types
            .iter()
            .any(|allowed| allowed.eq_ignore_ascii_case(&file.mime_type))
    {
        return if cfg.filters.ignore_unmatched_schema {
            SyncDecision::Skip
        } else {
            SyncDecision::SchemaMismatch
        };
    }
    SyncDecision::Accept
}

/// Aggregated counters returned by an executor pass over a file batch.
#[derive(Debug, Clone, Default, Serialize)]
pub struct SyncStats {
    pub accepted: u32,
    pub skipped: u32,
    pub schema_mismatched: u32,
}

/// Apply [`decide`] across a whole batch and roll the per-file
/// outcomes up into [`SyncStats`]. Used by the executor to pre-classify
/// files before issuing any HTTP traffic against `media-sets-service`.
pub fn classify_batch(
    cfg: &MediaSetSyncConfig,
    files: &[SourceFile],
    already_synced: &HashSet<String>,
    allowed_mime_types: &[String],
) -> Result<(Vec<(SourceFile, SyncDecision)>, SyncStats), String> {
    let matcher = cfg.matcher()?;
    let mut stats = SyncStats::default();
    let mut out = Vec::with_capacity(files.len());
    for file in files {
        let decision = decide(cfg, file, already_synced, allowed_mime_types, matcher.as_ref());
        match decision {
            SyncDecision::Accept => stats.accepted += 1,
            SyncDecision::Skip => stats.skipped += 1,
            SyncDecision::SchemaMismatch => stats.schema_mismatched += 1,
        }
        out.push((file.clone(), decision));
    }
    Ok((out, stats))
}
