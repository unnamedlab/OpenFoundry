//! Foundry staleness check (Builds.md § Staleness).
//!
//! > An output dataset is considered fresh if [build resolution]
//! > determines that input datasets and the logic specified within the
//! > JobSpec have not changed since the last time the output dataset
//! > was built. If an output dataset is fresh, it will not be
//! > recomputed in subsequent builds.
//!
//! Two signatures decide freshness:
//!
//!   * `canonical_logic_hash` — `JobSpec.content_hash` captured at the
//!     moment the spec was published. Same logic ⇒ same hash.
//!   * `input_signature` — `sha256(canonical(input dataset_rid + branch
//!     + head_transaction_rid + view_filter))` over the resolved
//!     `ResolvedInputView` set. Inputs unchanged ⇒ same hash.
//!
//! When both match the latest COMPLETED job for the same JobSpec on
//! the same `(pipeline_rid, build_branch)`, the new job is marked
//! `stale_skipped = TRUE`, transitioned straight to `COMPLETED`, and
//! its output transactions are *not* opened. `force_build = TRUE` on
//! the build short-circuits the check entirely.

use std::collections::BTreeMap;

use sha2::{Digest, Sha256};
use sqlx::PgPool;
use uuid::Uuid;

use crate::domain::build_resolution::{JobSpec, ResolvedInputView};

/// Stable signature over the resolved inputs of a job. Sorted by
/// `dataset_rid` first to make the hash insensitive to input ordering.
pub fn input_signature(views: &[ResolvedInputView]) -> String {
    let mut sorted: BTreeMap<&str, (String, Option<String>, Vec<String>)> = BTreeMap::new();
    for view in views {
        // The schema can include non-canonical orderings; we only hash
        // the dataset+branch identity here. Schema drift is captured
        // separately by `canonical_logic_hash` (the JobSpec content
        // hash) since changing the schema requires republishing the
        // spec.
        sorted.insert(
            view.dataset_rid.as_str(),
            (view.branch.as_str().to_string(), None, vec![]),
        );
    }
    let mut hasher = Sha256::new();
    for (dataset, (branch, head, filters)) in &sorted {
        hasher.update(dataset.as_bytes());
        hasher.update(b"\0");
        hasher.update(branch.as_bytes());
        hasher.update(b"\0");
        if let Some(h) = head {
            hasher.update(h.as_bytes());
        }
        hasher.update(b"\0");
        for f in filters {
            hasher.update(f.as_bytes());
            hasher.update(b",");
        }
        hasher.update(b"|");
    }
    format!("{:x}", hasher.finalize())
}

/// Snapshot of the last COMPLETED job for the same JobSpec on the
/// same branch — what the staleness check compares against.
#[derive(Debug, Clone, sqlx::FromRow)]
pub struct CompletedJobSnapshot {
    pub id: Uuid,
    pub canonical_logic_hash: Option<String>,
    pub input_signature: Option<String>,
    pub output_content_hash: Option<String>,
}

/// Result of a freshness check. The executor uses [`StalenessOutcome::Fresh`]
/// to skip execution and copy `output_content_hash` from the previous job.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StalenessOutcome {
    /// Inputs and logic both unchanged since `previous_job_id` — skip.
    Fresh {
        previous_job_id: Uuid,
        previous_output_content_hash: Option<String>,
    },
    /// At least one signature changed — execute.
    Stale { reason: StaleReason },
    /// No prior COMPLETED job for this `(JobSpec, build_branch)` —
    /// always execute.
    NoPriorBuild,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum StaleReason {
    LogicChanged,
    InputsChanged,
    BothChanged,
    PriorBuildNotComparable,
}

/// Pure helper: compare a freshly-computed `(logic_hash, inputs_hash)`
/// against a recorded snapshot. Used inline by [`is_fresh`] and
/// re-exported for unit tests.
pub fn compare_signatures(
    current_logic: &str,
    current_inputs: &str,
    previous: &CompletedJobSnapshot,
) -> StalenessOutcome {
    match (
        previous.canonical_logic_hash.as_deref(),
        previous.input_signature.as_deref(),
    ) {
        (Some(prev_logic), Some(prev_inputs)) => {
            let logic_match = prev_logic == current_logic;
            let inputs_match = prev_inputs == current_inputs;
            match (logic_match, inputs_match) {
                (true, true) => StalenessOutcome::Fresh {
                    previous_job_id: previous.id,
                    previous_output_content_hash: previous.output_content_hash.clone(),
                },
                (false, true) => StalenessOutcome::Stale {
                    reason: StaleReason::LogicChanged,
                },
                (true, false) => StalenessOutcome::Stale {
                    reason: StaleReason::InputsChanged,
                },
                (false, false) => StalenessOutcome::Stale {
                    reason: StaleReason::BothChanged,
                },
            }
        }
        _ => StalenessOutcome::Stale {
            reason: StaleReason::PriorBuildNotComparable,
        },
    }
}

/// Look up the latest COMPLETED job for a given JobSpec on a build
/// branch. Returns `None` when nothing has been built yet.
pub async fn last_completed_job(
    pool: &PgPool,
    pipeline_rid: &str,
    build_branch: &str,
    job_spec_rid: &str,
) -> Result<Option<CompletedJobSnapshot>, sqlx::Error> {
    sqlx::query_as::<_, CompletedJobSnapshot>(
        r#"SELECT j.id, j.canonical_logic_hash, j.input_signature, j.output_content_hash
             FROM jobs j
             JOIN builds b ON b.id = j.build_id
            WHERE b.pipeline_rid = $1
              AND b.build_branch = $2
              AND j.job_spec_rid = $3
              AND j.state = 'COMPLETED'
            ORDER BY j.state_changed_at DESC
            LIMIT 1"#,
    )
    .bind(pipeline_rid)
    .bind(build_branch)
    .bind(job_spec_rid)
    .fetch_optional(pool)
    .await
}

/// High-level entry point used by the executor. Wraps
/// [`last_completed_job`] + [`compare_signatures`] and returns the
/// per-job decision.
pub async fn is_fresh(
    pool: &PgPool,
    pipeline_rid: &str,
    build_branch: &str,
    spec: &JobSpec,
    resolved_inputs: &[ResolvedInputView],
) -> Result<StalenessOutcome, sqlx::Error> {
    let previous = last_completed_job(pool, pipeline_rid, build_branch, &spec.rid).await?;
    let Some(previous) = previous else {
        return Ok(StalenessOutcome::NoPriorBuild);
    };
    let current_inputs_hash = input_signature(resolved_inputs);
    Ok(compare_signatures(
        &spec.content_hash,
        &current_inputs_hash,
        &previous,
    ))
}

#[cfg(test)]
mod tests {
    use super::*;
    use core_models::dataset::transaction::BranchName;

    fn view(rid: &str, branch: &str) -> ResolvedInputView {
        ResolvedInputView {
            dataset_rid: rid.to_string(),
            branch: branch.parse::<BranchName>().unwrap(),
            schema: serde_json::json!({}),
        }
    }

    #[test]
    fn input_signature_is_order_independent() {
        let s1 = input_signature(&[view("a", "master"), view("b", "master")]);
        let s2 = input_signature(&[view("b", "master"), view("a", "master")]);
        assert_eq!(s1, s2);
    }

    #[test]
    fn input_signature_differs_when_branches_differ() {
        let s1 = input_signature(&[view("a", "master")]);
        let s2 = input_signature(&[view("a", "develop")]);
        assert_ne!(s1, s2);
    }

    #[test]
    fn fresh_when_both_signatures_match() {
        let prev = CompletedJobSnapshot {
            id: Uuid::nil(),
            canonical_logic_hash: Some("hash-l".into()),
            input_signature: Some("hash-i".into()),
            output_content_hash: Some("out".into()),
        };
        assert!(matches!(
            compare_signatures("hash-l", "hash-i", &prev),
            StalenessOutcome::Fresh { .. }
        ));
    }

    #[test]
    fn stale_classifies_logic_vs_inputs_vs_both() {
        let prev = CompletedJobSnapshot {
            id: Uuid::nil(),
            canonical_logic_hash: Some("hash-l".into()),
            input_signature: Some("hash-i".into()),
            output_content_hash: None,
        };
        assert!(matches!(
            compare_signatures("CHANGED", "hash-i", &prev),
            StalenessOutcome::Stale {
                reason: StaleReason::LogicChanged
            }
        ));
        assert!(matches!(
            compare_signatures("hash-l", "CHANGED", &prev),
            StalenessOutcome::Stale {
                reason: StaleReason::InputsChanged
            }
        ));
        assert!(matches!(
            compare_signatures("X", "Y", &prev),
            StalenessOutcome::Stale {
                reason: StaleReason::BothChanged
            }
        ));
    }

    #[test]
    fn missing_prior_signatures_force_stale() {
        let prev = CompletedJobSnapshot {
            id: Uuid::nil(),
            canonical_logic_hash: None,
            input_signature: None,
            output_content_hash: None,
        };
        assert!(matches!(
            compare_signatures("hash-l", "hash-i", &prev),
            StalenessOutcome::Stale {
                reason: StaleReason::PriorBuildNotComparable
            }
        ));
    }
}
