//! Foundry "Build branch guarantees" — runtime invariants.
//!
//! Encodes three guarantees that must hold for every job in a build:
//!
//!   1. **A build never modifies any dataset branches other than the
//!      build branch.** The function [`assert_transaction_targets_build_branch`]
//!      asserts this on every transaction-open call.
//!   2. **A build never creates branches on input datasets.** Datasets
//!      that appear *only* as inputs in the JobGraph must not have
//!      `:create-branch` calls associated with them in the same build.
//!      [`assert_input_dataset_not_branched`] enforces this.
//!   3. **Build resolution only succeeds if the specified branch
//!      fallback sequence is compatible with the branch ancestries in
//!      the involved datasets.** Lives in
//!      [`super::branch_resolution::assert_chain_ancestry_compatible`].
//!
//! All three are *pure* — they take borrowed slices and return a
//! [`Result`]. The actual enforcement points live in `build_resolution`
//! (transaction open) and the lifecycle layer (job execution).

use std::collections::BTreeSet;

use core_models::dataset::transaction::{BranchName, DatasetRid};

use crate::domain::job_graph::JobGraph;

#[derive(Debug, thiserror::Error, PartialEq, Eq)]
pub enum GuaranteeError {
    #[error("transaction targets branch={target}, but build_branch={build_branch}")]
    TransactionOnWrongBranch {
        build_branch: BranchName,
        target: BranchName,
    },
    #[error("input-only dataset {dataset_rid} cannot be branched on by build {build_id}")]
    InputDatasetBranched {
        build_id: String,
        dataset_rid: DatasetRid,
    },
}

/// Foundry guarantee #1 — every transaction opened by a build must
/// target the *build branch*. Other branches on the same dataset are
/// untouched even when the build's outputs depend on them.
pub fn assert_transaction_targets_build_branch(
    build_branch: &BranchName,
    target_branch: &BranchName,
) -> Result<(), GuaranteeError> {
    if build_branch == target_branch {
        Ok(())
    } else {
        Err(GuaranteeError::TransactionOnWrongBranch {
            build_branch: build_branch.clone(),
            target: target_branch.clone(),
        })
    }
}

/// Foundry guarantee #2 — a dataset that appears *only* as an input
/// (never as an output) in the compiled JobGraph must not be the
/// target of `branch.create` in the same build run.
///
/// The job graph itself is trusted to be the source of truth for "what
/// is an output". Anything else is an input.
pub fn assert_input_dataset_not_branched(
    build_id: &str,
    graph: &JobGraph,
    branched_datasets: &[DatasetRid],
) -> Result<(), GuaranteeError> {
    let outputs: BTreeSet<&str> = graph
        .producers
        .keys()
        .map(|s| s.as_str())
        .collect();
    for dataset in branched_datasets {
        if !outputs.contains(dataset.as_str()) {
            return Err(GuaranteeError::InputDatasetBranched {
                build_id: build_id.to_string(),
                dataset_rid: dataset.clone(),
            });
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::build_resolution::{InputSpec, JobSpec};
    use crate::domain::job_graph::{JobGraphNode, JobGraph};
    use std::collections::BTreeMap;

    fn rid(s: &str) -> DatasetRid {
        format!("ri.foundry.main.dataset.{s}").parse().unwrap()
    }

    fn b(name: &str) -> BranchName {
        name.parse().unwrap()
    }

    #[test]
    fn transaction_on_build_branch_passes() {
        assert!(
            assert_transaction_targets_build_branch(&b("feature"), &b("feature")).is_ok()
        );
    }

    #[test]
    fn transaction_on_other_branch_fails() {
        let err = assert_transaction_targets_build_branch(&b("feature"), &b("master"))
            .expect_err("must fail");
        assert!(matches!(err, GuaranteeError::TransactionOnWrongBranch { .. }));
    }

    fn graph_with_outputs(outputs: &[&str]) -> JobGraph {
        let mut producers = BTreeMap::new();
        let mut nodes = Vec::new();
        for output in outputs {
            producers.insert(output.to_string(), format!("spec/{output}"));
            nodes.push(JobGraphNode {
                job_spec: JobSpec {
                    rid: format!("spec/{output}"),
                    pipeline_rid: "p1".into(),
                    branch_name: "feature".into(),
                    inputs: vec![InputSpec {
                        dataset_rid: "00000000-INPUT".into(),
                        fallback_chain: vec!["master".into()],
                        view_filter: vec![],
                        require_fresh: false,
                    }],
                    output_dataset_rids: vec![output.to_string()],
                    logic_kind: "sql".into(),
                    logic_payload: serde_json::Value::Null,
                    content_hash: "h".into(),
                },
                upstream_producers: BTreeMap::new(),
            });
        }
        JobGraph { nodes, producers }
    }

    #[test]
    fn branching_an_output_dataset_is_allowed() {
        let graph = graph_with_outputs(&["B", "C"]);
        assert!(
            assert_input_dataset_not_branched("build/1", &graph, &[rid("B")]).is_ok()
        );
    }

    #[test]
    fn branching_an_input_only_dataset_is_rejected() {
        let graph = graph_with_outputs(&["B", "C"]);
        let err = assert_input_dataset_not_branched(
            "build/1",
            &graph,
            &[rid("00000000-INPUT")],
        )
        .expect_err("must fail");
        assert!(matches!(err, GuaranteeError::InputDatasetBranched { .. }));
    }
}
