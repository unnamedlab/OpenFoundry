//! P2 — Foundry guarantee: "A build never modifies any dataset
//! branches other than the build branch."
//!
//! Pure assertion test: drives
//! [`run_guarantees::assert_transaction_targets_build_branch`] across
//! the surface a real build would hit (one transaction per output)
//! and verifies that any drift to a non-build branch trips the
//! invariant.

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::run_guarantees::{
    GuaranteeError, assert_transaction_targets_build_branch,
};

fn b(name: &str) -> BranchName {
    name.parse().unwrap()
}

#[test]
fn every_output_transaction_lands_on_build_branch() {
    let build_branch = b("feature");
    // Three outputs, every transaction targets the build branch.
    for _ in ["dataset.A", "dataset.B", "dataset.C"] {
        assert!(assert_transaction_targets_build_branch(&build_branch, &build_branch).is_ok());
    }
}

#[test]
fn drift_to_master_is_rejected() {
    let build_branch = b("feature");
    let err = assert_transaction_targets_build_branch(&build_branch, &b("master"))
        .expect_err("must fail");
    assert!(matches!(
        err,
        GuaranteeError::TransactionOnWrongBranch { .. }
    ));
}

#[test]
fn drift_to_sibling_branch_is_rejected() {
    let build_branch = b("feature");
    let err = assert_transaction_targets_build_branch(&build_branch, &b("develop"))
        .expect_err("must fail");
    match err {
        GuaranteeError::TransactionOnWrongBranch {
            build_branch,
            target,
        } => {
            assert_eq!(build_branch, b("feature"));
            assert_eq!(target, b("develop"));
        }
        other => panic!("unexpected: {other:?}"),
    }
}
