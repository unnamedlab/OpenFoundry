//! P2 — `branch_resolution::assert_chain_ancestry_compatible` rejects
//! a fallback chain that disagrees with a dataset's recorded branch
//! ancestry. Maps to the Foundry doc § "Build branch guarantees" line:
//! "Build resolution only succeeds if the specified branch fallback
//! sequence is compatible with the branch ancestries in the involved
//! datasets."

use core_models::dataset::transaction::{BranchName, DatasetRid};
use pipeline_build_service::domain::branch_resolution::{
    ResolveError, assert_chain_ancestry_compatible,
};

fn b(name: &str) -> BranchName {
    name.parse().unwrap()
}

fn rid(suffix: &str) -> DatasetRid {
    format!("ri.foundry.main.dataset.{suffix}").parse().unwrap()
}

#[test]
fn chain_in_walk_order_passes() {
    // Ancestry: feature → develop → master. Chain: develop → master.
    // Order matches: OK.
    assert!(
        assert_chain_ancestry_compatible(
            &rid("00000000-0000-0000-0000-000000000001"),
            &b("feature"),
            &[b("develop"), b("master")],
            &[b("feature"), b("develop"), b("master")],
        )
        .is_ok()
    );
}

#[test]
fn reversed_chain_is_rejected_with_incompatible_ancestry() {
    let err = assert_chain_ancestry_compatible(
        &rid("00000000-0000-0000-0000-000000000002"),
        &b("feature"),
        // Inverted: master before develop, even though ancestry has
        // develop above master.
        &[b("master"), b("develop")],
        &[b("feature"), b("develop"), b("master")],
    )
    .expect_err("must fail");
    match err {
        ResolveError::IncompatibleAncestry {
            dataset_rid,
            build_branch,
            target_chain,
            ancestry,
        } => {
            assert_eq!(
                dataset_rid.as_str(),
                "ri.foundry.main.dataset.00000000-0000-0000-0000-000000000002"
            );
            assert_eq!(build_branch, b("feature"));
            assert_eq!(target_chain, vec![b("master"), b("develop")]);
            assert_eq!(ancestry, vec![b("feature"), b("develop"), b("master")]);
        }
        other => panic!("expected IncompatibleAncestry, got {other:?}"),
    }
}

#[test]
fn empty_ancestry_is_trivially_compatible() {
    // Brand-new dataset: ancestry empty, any chain is fine.
    assert!(
        assert_chain_ancestry_compatible(
            &rid("00000000-0000-0000-0000-000000000003"),
            &b("feature"),
            &[b("develop"), b("master")],
            &[],
        )
        .is_ok()
    );
}
