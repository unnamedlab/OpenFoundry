//! JobSpec model — the serialized output-dataset definition published
//! by a Code Repository commit on a branch.
//!
//! Mirrors the Foundry "Branches in builds" doc § "Job graph
//! compilation": every commit on a branch publishes one JobSpec per
//! output dataset; builds on that branch collect JobSpecs by walking
//! the configured fallback chain.
//!
//! ## Wire format
//!
//! ```json
//! {
//!   "pipeline_rid": "ri.foundry.main.pipeline.…",
//!   "branch_name":  "feature/bookings",
//!   "output_dataset_rid": "ri.foundry.main.dataset.…",
//!   "output_branch": "feature/bookings",
//!   "inputs": [
//!     {
//!       "input": "ri.foundry.main.dataset.…",
//!       "fallback_chain": ["develop", "master"]
//!     }
//!   ],
//!   "job_spec_json": { … },           // arbitrary node serialisation
//!   "version": 3,
//!   "published_at": "2026-05-04T12:00:00Z"
//! }
//! ```
//!
//! `fallback_chain` replaces the legacy `fallback_enabled: bool` knob
//! on the dataset-input wire format. The translation rule is:
//!
//! * `fallback_enabled = true`  ⇒ `fallback_chain = ["master"]`
//! * `fallback_enabled = false` ⇒ `fallback_chain = []`
//!
//! Both are accepted on input via `JobSpecInputCompat::into_input`
//! so existing publishers keep working while clients migrate.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct JobSpecRow {
    pub id: Uuid,
    pub rid: String,
    pub pipeline_rid: String,
    pub branch_name: String,
    pub output_dataset_rid: String,
    pub output_branch: String,
    pub job_spec_json: Value,
    pub inputs: Value,
    pub content_hash: String,
    pub version: i32,
    pub published_by: Uuid,
    pub published_at: DateTime<Utc>,
}

/// Per-input declaration on a JobSpec. `fallback_chain` is the
/// per-input override; the build-level chain is the global default.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct JobSpecInput {
    /// `ri.foundry.main.dataset.<uuid>`.
    pub input: String,
    #[serde(default)]
    pub fallback_chain: Vec<String>,
}

/// Backwards-compatible deserialiser: accepts either the new shape
/// (`fallback_chain`) or the legacy shape (`fallback_enabled`).
#[derive(Debug, Deserialize)]
pub struct JobSpecInputCompat {
    pub input: String,
    #[serde(default)]
    pub fallback_chain: Option<Vec<String>>,
    #[serde(default)]
    pub fallback_enabled: Option<bool>,
}

impl JobSpecInputCompat {
    /// Translate to the canonical [`JobSpecInput`] shape. Trims
    /// whitespace from each entry and drops empties.
    pub fn into_input(self) -> JobSpecInput {
        let chain = match (self.fallback_chain, self.fallback_enabled) {
            (Some(chain), _) => chain,
            (None, Some(true)) => vec!["master".to_string()],
            (None, Some(false)) => Vec::new(),
            (None, None) => Vec::new(),
        };
        JobSpecInput {
            input: self.input.trim().to_string(),
            fallback_chain: chain
                .into_iter()
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect(),
        }
    }
}

#[derive(Debug, Deserialize)]
pub struct PublishJobSpecRequest {
    pub output_dataset_rid: String,
    /// Optional output branch override. Defaults to the path branch.
    #[serde(default)]
    pub output_branch: Option<String>,
    pub job_spec_json: Value,
    #[serde(default)]
    pub inputs: Vec<JobSpecInputCompat>,
}

#[derive(Debug, Serialize)]
pub struct PublishJobSpecResponse {
    pub job_spec: JobSpecRow,
    /// `true` when the publish was a no-op (idempotent re-publish of
    /// identical content); `false` when a new version was minted.
    pub new_version: bool,
}

#[derive(Debug, Deserialize)]
pub struct ListByPipelineQuery {
    /// Filter by output dataset (optional).
    pub output_dataset_rid: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ListByDatasetQuery {
    /// `?on_branch=master` — return JobSpecs published on this branch.
    /// When omitted, returns every branch.
    pub on_branch: Option<String>,
}

/// Compute the canonical content hash for `(job_spec_json, inputs)`.
///
/// MD5 over the deterministic serialisation of `[job_spec_json, inputs]`.
/// MD5 is fine here — the hash is used for change detection, not
/// cryptographic identity.
pub fn content_hash(job_spec_json: &Value, inputs: &[JobSpecInput]) -> String {
    use md5::{Digest, Md5};
    let payload = serde_json::json!([job_spec_json, inputs]);
    let bytes = serde_json::to_vec(&payload).unwrap_or_default();
    let mut hasher = Md5::new();
    hasher.update(&bytes);
    format!("{:x}", hasher.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn legacy_fallback_enabled_true_maps_to_master_only() {
        let raw = json!({ "input": "ri.foundry.main.dataset.x", "fallback_enabled": true });
        let compat: JobSpecInputCompat = serde_json::from_value(raw).unwrap();
        assert_eq!(
            compat.into_input(),
            JobSpecInput {
                input: "ri.foundry.main.dataset.x".into(),
                fallback_chain: vec!["master".into()],
            }
        );
    }

    #[test]
    fn legacy_fallback_enabled_false_maps_to_empty_chain() {
        let raw = json!({ "input": "ri.foundry.main.dataset.x", "fallback_enabled": false });
        let compat: JobSpecInputCompat = serde_json::from_value(raw).unwrap();
        assert!(compat.into_input().fallback_chain.is_empty());
    }

    #[test]
    fn explicit_fallback_chain_wins_over_legacy_flag() {
        let raw = json!({
            "input": "ri.foundry.main.dataset.x",
            "fallback_chain": ["develop", "master"],
            "fallback_enabled": true
        });
        let compat: JobSpecInputCompat = serde_json::from_value(raw).unwrap();
        assert_eq!(
            compat.into_input().fallback_chain,
            vec!["develop".to_string(), "master".to_string()]
        );
    }

    #[test]
    fn content_hash_is_stable_for_equivalent_payloads() {
        let json = json!({ "node": "transform" });
        let inputs = vec![JobSpecInput {
            input: "ri.foundry.main.dataset.a".into(),
            fallback_chain: vec!["master".into()],
        }];
        let h1 = content_hash(&json, &inputs);
        let h2 = content_hash(&json, &inputs);
        assert_eq!(h1, h2);
    }

    #[test]
    fn content_hash_differs_when_inputs_change() {
        let json = json!({ "node": "transform" });
        let h1 = content_hash(
            &json,
            &[JobSpecInput {
                input: "ri.foundry.main.dataset.a".into(),
                fallback_chain: vec!["master".into()],
            }],
        );
        let h2 = content_hash(
            &json,
            &[JobSpecInput {
                input: "ri.foundry.main.dataset.a".into(),
                fallback_chain: vec!["develop".into()],
            }],
        );
        assert_ne!(h1, h2);
    }
}
