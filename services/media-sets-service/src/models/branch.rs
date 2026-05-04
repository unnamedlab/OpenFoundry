//! `media_set_branches` row type + REST DTOs.
//!
//! Mirrors the Foundry branching contract documented in
//! `Core concepts/Branching.md` ("Every non-root branch has exactly
//! one parent branch", "One open transaction per branch", "Deleting
//! a branch deletes the pointer, not the transactions"). The
//! authoritative key is the composite `(media_set_rid, branch_name)`;
//! `branch_rid` is a generated stored column we expose alongside so
//! neighbours (audit-sink, ontology, the front-end) can address a
//! branch by RID alone.
//!
//! See `services/dataset-versioning-service/src/models/branch.rs` for
//! the dataset-side analogue — both surfaces share the same field
//! shape (parent / head / created_*) and the same `is_root()` helper.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

/// Postgres row of `media_set_branches` after `0006_branching.sql`.
///
/// The table is keyed on the composite `(media_set_rid, branch_name)`
/// — the same key the partial unique transaction index uses. The
/// generated `branch_rid` column is the public Foundry-style
/// identifier (`ri.foundry.main.media_branch.<md5(set_rid:name)>`).
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct MediaSetBranch {
    pub media_set_rid: String,
    pub branch_name: String,
    /// Generated stored column. Stable across inserts.
    pub branch_rid: String,
    /// `branch_rid` of the parent branch. NULL = root branch.
    #[serde(default)]
    pub parent_branch_rid: Option<String>,
    /// RID of the most recent committed (or open) transaction on
    /// this branch. NULL until the first commit lands.
    #[serde(default)]
    pub head_transaction_rid: Option<String>,
    pub created_at: DateTime<Utc>,
    #[serde(default)]
    pub created_by: String,
}

impl MediaSetBranch {
    /// Derived from `parent_branch_rid IS NULL`. Mirrors the
    /// `Branch.is_root` semantics on the dataset side.
    pub fn is_root(&self) -> bool {
        self.parent_branch_rid.is_none()
    }
}

/// `POST /media-sets/{rid}/branches` body.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct CreateBranchBody {
    pub name: String,
    /// Optional parent branch *name*. Defaults to `"main"`.
    /// Per Foundry: a child branch is created from a parent (or a
    /// transaction); we always inherit the parent's head transaction
    /// at creation time so the new branch sees the same view.
    #[serde(default)]
    pub from_branch: Option<String>,
    /// Optional transaction RID to fork the branch from. When set,
    /// the new branch's head pointer is initialised to this RID
    /// instead of the parent's current head — matches Foundry "create
    /// branch from any transaction" plus the "Restore to this point"
    /// affordance the History UI uses.
    #[serde(default)]
    pub from_transaction_rid: Option<String>,
}

/// `POST /media-sets/{rid}/branches/{name}/merge` body.
///
/// The merge is per-path (the contract item-stores can give without
/// row-level diffs): for every path live on the source branch, copy
/// the most recent live item to the target branch. The conflict
/// resolution selects between two policies the H4 spec calls for.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct MergeBranchBody {
    /// Branch to merge INTO. Required; refusing a default keeps
    /// operators explicit about destination.
    pub target_branch: String,
    /// Resolution strategy when both source and target have a live
    /// item at the same path. Defaults to `LATEST_WINS`.
    #[serde(default)]
    pub resolution: MergeResolution,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MergeResolution {
    /// Source path always wins; the target's live item at the same
    /// path is soft-deleted and the source's item is copied across.
    #[default]
    LatestWins,
    /// Refuses the merge on the first overlapping path. Returns
    /// HTTP 409 with the conflicting paths surfaced in the body.
    FailOnConflict,
}

impl MergeResolution {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::LatestWins => "LATEST_WINS",
            Self::FailOnConflict => "FAIL_ON_CONFLICT",
        }
    }
}

/// `POST /media-sets/{rid}/branches/{name}/reset` response body.
/// Surfaces the branch in its post-reset form so the UI can refresh
/// without a follow-up GET.
#[derive(Debug, Clone, Serialize)]
pub struct ResetBranchResponse {
    pub branch: MediaSetBranch,
    pub items_soft_deleted: i64,
}

/// `POST /media-sets/{rid}/branches/{name}/merge` response body.
/// Surfaces the per-path resolution decision so callers can audit.
#[derive(Debug, Clone, Serialize)]
pub struct MergeBranchResponse {
    pub source_branch: String,
    pub target_branch: String,
    pub resolution: String,
    pub paths_copied: i64,
    pub paths_overwritten: i64,
    pub paths_skipped: i64,
}
