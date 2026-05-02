//! Cross-branch input/output resolution for pipeline builds.
//!
//! ## Why this exists
//!
//! Foundry pipelines build "on a branch". When the build runs on branch
//! `feature/x`, each input dataset is *not* required to have that branch:
//! many datasets live only on `master` and the build still has to pull
//! their latest committed state. The Datasets doc encodes this as a
//! **fallback chain**: try the build branch first, then walk a configured
//! list (`feature/x → develop → master`) and use the first one that
//! exists on each input dataset.
//!
//! For *outputs*, the same chain decides where the new transaction lands
//! when the build branch doesn't yet exist on the output dataset: the
//! resolver returns the first present fallback so the caller can branch
//! off it before opening the write transaction. (Foundry never merges
//! dataset branches; new branches are forks.)
//!
//! Both functions in this module are **pure**: they take the set of
//! branches that exist on a dataset and return what to do. The HTTP
//! glue lives in `handlers::execute` (input lookup) and uses
//! `dataset-versioning-service` for the actual branch creation /
//! transaction open.

use core_models::dataset::transaction::BranchName;

/// Outcome of resolving a *read* on an input dataset.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedInput {
    /// Branch the build will read from for this dataset.
    pub branch: BranchName,
    /// Where in the chain the match was found:
    /// * `0` ⇒ exact match on the build branch.
    /// * `n ≥ 1` ⇒ fell back to `fallback_chain[n - 1]`.
    pub fallback_index: usize,
}

/// Outcome of resolving a *write* (output) on a dataset whose branch the
/// build is targeting.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ResolvedOutput {
    /// The build branch already exists; just open a transaction on it.
    Existing(BranchName),
    /// The build branch is missing; create it from `from` then open the
    /// transaction. `from` is the first present entry of the fallback
    /// chain.
    CreateFrom {
        new_branch: BranchName,
        from: BranchName,
    },
}

/// Why a resolution failed.
#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum ResolveError {
    /// Neither the build branch nor any fallback is present on the
    /// dataset. For inputs this is fatal (nothing to read). For outputs
    /// it means the dataset is empty: the caller may want to create the
    /// build branch as a *root* instead of forking.
    #[error("no matching branch for build='{build_branch}'; tried [{}] against dataset branches [{}]", join_branches(.tried), join_branches(.available))]
    NoMatch {
        build_branch: BranchName,
        tried: Vec<BranchName>,
        available: Vec<BranchName>,
    },
}

fn join_branches(items: &[BranchName]) -> String {
    items
        .iter()
        .map(|b| b.as_str())
        .collect::<Vec<_>>()
        .join(", ")
}

/// Resolve which branch of an input dataset the build should read from.
///
/// Algorithm (mirrors the Datasets doc § "Cross-branch builds"):
///
/// 1. If `build_branch` is present on the dataset, use it.
/// 2. Otherwise walk `fallback_chain` in order; return the first present.
/// 3. If none match, return `NoMatch`.
///
/// `dataset_branches` is the list of branches that currently exist on the
/// dataset (typically obtained from
/// `GET /v1/datasets/{rid}/branches` on `dataset-versioning-service`).
pub fn resolve_input_dataset(
    build_branch: &BranchName,
    fallback_chain: &[BranchName],
    dataset_branches: &[BranchName],
) -> Result<ResolvedInput, ResolveError> {
    if dataset_branches.contains(build_branch) {
        return Ok(ResolvedInput {
            branch: build_branch.clone(),
            fallback_index: 0,
        });
    }
    for (i, candidate) in fallback_chain.iter().enumerate() {
        if dataset_branches.contains(candidate) {
            return Ok(ResolvedInput {
                branch: candidate.clone(),
                // i is 0-based in the chain; we expose 1-based to keep
                // "0 = exact build branch" stable.
                fallback_index: i + 1,
            });
        }
    }
    Err(ResolveError::NoMatch {
        build_branch: build_branch.clone(),
        tried: fallback_chain.to_vec(),
        available: dataset_branches.to_vec(),
    })
}

/// Resolve where to open the write transaction for an output dataset.
///
/// Returns:
/// * [`ResolvedOutput::Existing`] when `build_branch` already exists on
///   the dataset (open a transaction on it).
/// * [`ResolvedOutput::CreateFrom`] when it doesn't: the caller should
///   first call `create_child_branch(from_branch = from)` on
///   `dataset-versioning-service`, then open the transaction.
///
/// Returns [`ResolveError::NoMatch`] when neither the build branch nor
/// any fallback is present (the dataset is brand new with no branches at
/// all). The caller can then create a root branch.
pub fn resolve_output_dataset(
    build_branch: &BranchName,
    fallback_chain: &[BranchName],
    dataset_branches: &[BranchName],
) -> Result<ResolvedOutput, ResolveError> {
    if dataset_branches.contains(build_branch) {
        return Ok(ResolvedOutput::Existing(build_branch.clone()));
    }
    for candidate in fallback_chain {
        if dataset_branches.contains(candidate) {
            return Ok(ResolvedOutput::CreateFrom {
                new_branch: build_branch.clone(),
                from: candidate.clone(),
            });
        }
    }
    Err(ResolveError::NoMatch {
        build_branch: build_branch.clone(),
        tried: fallback_chain.to_vec(),
        available: dataset_branches.to_vec(),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn b(name: &str) -> BranchName {
        name.parse().expect("valid branch name")
    }

    /// Scenario from `Datasets.md` § "Cross-branch builds":
    ///
    /// ```text
    ///   build_branch  = feature
    ///   fallback      = [develop, master]
    ///
    ///   dataset A:    [master]                  → master   (fallback #2)
    ///   dataset B:    [feature, master]         → feature  (exact)
    ///   dataset C:    [feature, develop, master]→ feature  (exact)
    ///   dataset D:    [develop, master]         → develop  (fallback #1)
    /// ```
    #[test]
    fn cross_branch_input_resolution_doc_scenario() {
        let build = b("feature");
        let chain = vec![b("develop"), b("master")];

        let a = resolve_input_dataset(&build, &chain, &[b("master")]).unwrap();
        assert_eq!(a.branch, b("master"));
        assert_eq!(a.fallback_index, 2);

        let b_ = resolve_input_dataset(&build, &chain, &[b("feature"), b("master")]).unwrap();
        assert_eq!(b_.branch, b("feature"));
        assert_eq!(b_.fallback_index, 0);

        let c = resolve_input_dataset(
            &build,
            &chain,
            &[b("feature"), b("develop"), b("master")],
        )
        .unwrap();
        assert_eq!(c.branch, b("feature"));
        assert_eq!(c.fallback_index, 0);

        let d = resolve_input_dataset(&build, &chain, &[b("develop"), b("master")]).unwrap();
        assert_eq!(d.branch, b("develop"));
        assert_eq!(d.fallback_index, 1);
    }

    #[test]
    fn input_returns_no_match_when_chain_is_exhausted() {
        let err = resolve_input_dataset(&b("feature"), &[b("develop")], &[b("staging")])
            .expect_err("no overlap");
        match err {
            ResolveError::NoMatch { build_branch, .. } => assert_eq!(build_branch, b("feature")),
        }
    }

    #[test]
    fn input_with_no_chain_only_succeeds_on_exact_match() {
        assert!(resolve_input_dataset(&b("master"), &[], &[b("master")]).is_ok());
        assert!(resolve_input_dataset(&b("master"), &[], &[b("develop")]).is_err());
    }

    #[test]
    fn output_uses_existing_branch_when_present() {
        let r = resolve_output_dataset(&b("feature"), &[b("master")], &[b("feature"), b("master")])
            .unwrap();
        assert_eq!(r, ResolvedOutput::Existing(b("feature")));
    }

    #[test]
    fn output_creates_branch_from_first_present_fallback() {
        // build branch missing → fork from `develop` (first present fallback),
        // skipping `master` even though it also exists.
        let r = resolve_output_dataset(
            &b("feature"),
            &[b("develop"), b("master")],
            &[b("develop"), b("master")],
        )
        .unwrap();
        assert_eq!(
            r,
            ResolvedOutput::CreateFrom {
                new_branch: b("feature"),
                from: b("develop"),
            }
        );
    }

    #[test]
    fn output_skips_absent_fallbacks() {
        // `develop` not on the dataset → resolver walks past it to `master`.
        let r = resolve_output_dataset(
            &b("feature"),
            &[b("develop"), b("master")],
            &[b("master")],
        )
        .unwrap();
        assert_eq!(
            r,
            ResolvedOutput::CreateFrom {
                new_branch: b("feature"),
                from: b("master"),
            }
        );
    }

    #[test]
    fn output_no_match_for_empty_dataset() {
        let err =
            resolve_output_dataset(&b("feature"), &[b("master")], &[]).expect_err("no branches");
        assert!(matches!(err, ResolveError::NoMatch { .. }));
    }
}
