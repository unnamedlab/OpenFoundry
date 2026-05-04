//! Foundry doc § "Parameterized pipelines" is explicit:
//!
//!   "Automated triggers are not yet supported."
//!
//! Verifies the dispatch guard rejects every non-manual trigger kind
//! with the documented reason. The HTTP handler turns this rejection
//! into a 409 CONFLICT so the UI can surface a clear "manual only"
//! warning.

use pipeline_authoring_service::parameterized::{
    DispatchError, TriggerKind, assert_manual_dispatch,
};

#[test]
fn manual_trigger_is_accepted() {
    assert!(assert_manual_dispatch(TriggerKind::Manual).is_ok());
}

#[test]
fn every_automated_trigger_is_rejected_with_the_doc_reason() {
    for kind in [TriggerKind::Time, TriggerKind::Event, TriggerKind::Compound] {
        let err = assert_manual_dispatch(kind).expect_err("must reject");
        assert_eq!(err, DispatchError::AutomatedTriggerRejected);
        let msg = err.to_string();
        assert!(
            msg.contains("automated triggers"),
            "doc-aligned reason should mention automated triggers, got `{msg}`"
        );
    }
}
