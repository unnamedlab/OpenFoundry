//! Dataset branch model.
//!
//! P1 — unified surface aligned with `proto/dataset/branch.proto`.
//! `DatasetBranch` mirrors a row of `dataset_branches` after the
//! `20260504000010_branches_unify` migration. The struct exposes both
//! the new Foundry-style fields (rid, parent_branch_rid, fallback_chain,
//! labels, …) and the legacy columns (`version`, `base_version`,
//! `is_default`) kept for backwards compatibility with the pre-Foundry
//! handlers in `handlers::branches`.
//!
//! `is_root` is intentionally not stored: it is derived from
//! `parent_branch_id IS NULL` so a single column remains the source of
//! truth.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

/// Format a transaction UUID as the public Foundry RID. Inlined here
/// (instead of pulling from `models::transaction_rid`) so this module
/// is self-contained and serialises without an extra `mod` declaration.
fn format_transaction_rid_opt(id: Option<Uuid>) -> Option<String> {
    id.map(|u| format!("ri.foundry.main.transaction.{u}"))
}

/// Postgres row of `dataset_branches`. The `rid` column is generated
/// by the database (`'ri.foundry.main.branch.' || id::text`) so we
/// always read a stable, public identifier alongside the internal UUID.
#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetBranch {
    pub id: Uuid,
    /// `ri.foundry.main.branch.<uuid>`. Stored-generated.
    #[serde(default)]
    pub rid: String,
    pub dataset_id: Uuid,
    /// `ri.foundry.main.dataset.<uuid>`. Persisted on each row by
    /// `RuntimeStore::create_foundry_branch` so callers can address a
    /// branch from RID alone.
    #[serde(default)]
    pub dataset_rid: String,
    pub name: String,
    #[serde(default)]
    pub parent_branch_id: Option<Uuid>,
    #[serde(default)]
    pub head_transaction_id: Option<Uuid>,
    /// The transaction the branch was forked off when minted via
    /// `source.from_transaction_rid`. Distinct from
    /// `head_transaction_id`, which advances on every commit.
    #[serde(default)]
    pub created_from_transaction_id: Option<Uuid>,
    /// Bumped by trigger on every transaction INSERT for this branch.
    #[serde(default = "DatasetBranch::default_last_activity_at")]
    pub last_activity_at: DateTime<Utc>,
    /// Free-form metadata (persona, ticket, …).
    #[serde(default)]
    pub labels: serde_json::Value,
    /// Denormalised cache of `dataset_branch_fallbacks`. Source of
    /// truth still lives in that table; `RuntimeStore::replace_fallbacks`
    /// keeps both surfaces in sync.
    #[serde(default)]
    pub fallback_chain: Vec<String>,

    // ── Legacy columns ───────────────────────────────────────────────
    /// Deprecated — use `head_transaction_id`. Kept so the legacy
    /// `handlers::branches` (checkout / merge / promote) keep working.
    #[serde(default)]
    pub version: i32,
    /// Deprecated — see `version`.
    #[serde(default)]
    pub base_version: i32,
    pub description: String,
    /// Deprecated — derive "default branch" from `parent_branch_id IS NULL`.
    #[serde(default)]
    pub is_default: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl DatasetBranch {
    fn default_last_activity_at() -> DateTime<Utc> {
        Utc::now()
    }

    /// Derived from `parent_branch_id IS NULL`. Mirrors the proto
    /// `Branch.is_root` field.
    pub fn is_root(&self) -> bool {
        self.parent_branch_id.is_none()
    }

    /// Public branch RID. Falls back to a synthesised RID when reading
    /// rows that pre-date the `20260504000010_branches_unify` migration.
    pub fn branch_rid(&self) -> String {
        if !self.rid.is_empty() {
            self.rid.clone()
        } else {
            format!("ri.foundry.main.branch.{}", self.id)
        }
    }

    pub fn parent_branch_rid(&self) -> Option<String> {
        self.parent_branch_id
            .map(|id| format!("ri.foundry.main.branch.{id}"))
    }

    pub fn head_transaction_rid(&self) -> Option<String> {
        format_transaction_rid_opt(self.head_transaction_id)
    }

    pub fn created_from_transaction_rid(&self) -> Option<String> {
        format_transaction_rid_opt(self.created_from_transaction_id)
    }
}

#[derive(Debug, Deserialize)]
pub struct CreateDatasetBranchRequest {
    pub name: String,
    pub source_version: Option<i32>,
    pub description: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct MergeDatasetBranchRequest {
    pub target_branch: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;
    use serde_json::json;

    fn fixture() -> DatasetBranch {
        let id = Uuid::parse_str("00000000-0000-0000-0000-000000000001").unwrap();
        let parent = Uuid::parse_str("00000000-0000-0000-0000-000000000002").unwrap();
        let head = Uuid::parse_str("00000000-0000-0000-0000-00000000000a").unwrap();
        DatasetBranch {
            id,
            rid: format!("ri.foundry.main.branch.{id}"),
            dataset_id: Uuid::nil(),
            dataset_rid: "ri.foundry.main.dataset.foo".to_string(),
            name: "feature".to_string(),
            parent_branch_id: Some(parent),
            head_transaction_id: Some(head),
            created_from_transaction_id: Some(head),
            last_activity_at: Utc.with_ymd_and_hms(2026, 5, 3, 12, 0, 0).unwrap(),
            labels: json!({"persona": "data-eng"}),
            fallback_chain: vec!["develop".into(), "master".into()],
            version: 1,
            base_version: 1,
            description: String::new(),
            is_default: false,
            created_at: Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap(),
            updated_at: Utc.with_ymd_and_hms(2026, 5, 3, 0, 0, 0).unwrap(),
        }
    }

    #[test]
    fn is_root_is_derived_from_parent_branch_id() {
        let mut b = fixture();
        assert!(!b.is_root());
        b.parent_branch_id = None;
        assert!(b.is_root());
    }

    #[test]
    fn rid_helpers_synthesise_from_uuid() {
        let b = fixture();
        assert_eq!(b.branch_rid(), format!("ri.foundry.main.branch.{}", b.id));
        assert_eq!(
            b.parent_branch_rid().as_deref(),
            Some(format!("ri.foundry.main.branch.{}", b.parent_branch_id.unwrap()).as_str()),
        );
        assert_eq!(
            b.head_transaction_rid().as_deref(),
            Some(
                format!(
                    "ri.foundry.main.transaction.{}",
                    b.head_transaction_id.unwrap()
                )
                .as_str()
            ),
        );
    }

    #[test]
    fn serde_round_trip_preserves_new_fields() {
        let b = fixture();
        let raw = serde_json::to_value(&b).unwrap();
        assert_eq!(raw["name"], "feature");
        assert_eq!(raw["fallback_chain"], json!(["develop", "master"]));
        assert_eq!(raw["labels"]["persona"], "data-eng");
        assert!(raw.get("rid").is_some());
        assert!(raw.get("created_from_transaction_id").is_some());
        assert!(raw.get("last_activity_at").is_some());

        let back: DatasetBranch = serde_json::from_value(raw).unwrap();
        assert_eq!(back.fallback_chain, b.fallback_chain);
        assert_eq!(back.labels, b.labels);
        assert_eq!(
            back.created_from_transaction_id,
            b.created_from_transaction_id
        );
    }

    #[test]
    fn serde_defaults_apply_when_legacy_payload_is_deserialised() {
        let legacy = json!({
            "id": "00000000-0000-0000-0000-000000000001",
            "dataset_id": "00000000-0000-0000-0000-000000000000",
            "name": "old",
            "version": 1,
            "base_version": 1,
            "description": "",
            "is_default": true,
            "created_at": "2026-05-01T00:00:00Z",
            "updated_at": "2026-05-01T00:00:00Z"
        });
        let parsed: DatasetBranch = serde_json::from_value(legacy).unwrap();
        assert!(parsed.is_root());
        assert!(parsed.fallback_chain.is_empty());
        assert_eq!(parsed.labels, serde_json::Value::Null);
        assert!(parsed.dataset_rid.is_empty());
    }
}
