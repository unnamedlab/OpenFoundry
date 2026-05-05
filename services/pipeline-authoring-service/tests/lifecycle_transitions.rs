//! FASE 1 — Pipeline lifecycle FSM transition coverage.
//!
//! Pure-logic tests over `pipeline_authoring_service::lifecycle`. Covers
//! every legal transition documented in `domain/lifecycle.rs` and at
//! least one illegal jump per non-terminal source state.
//!
//! Ref: docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Considerations Pipeline Builder and Code Repositories.md
//!      (Versioning: full versioning workflow on rails)

use pipeline_authoring_service::lifecycle::{LifecycleError, PipelineLifecycle};

#[test]
fn parse_round_trips_for_every_state() {
    for state in PipelineLifecycle::ALL.iter().copied() {
        let parsed = PipelineLifecycle::parse(state.as_str()).expect("known literal parses");
        assert_eq!(parsed, state, "round-trip mismatch for {state:?}");
    }
}

#[test]
fn parse_is_case_insensitive_and_trims() {
    assert_eq!(
        PipelineLifecycle::parse("  draft  ").unwrap(),
        PipelineLifecycle::Draft
    );
    assert_eq!(
        PipelineLifecycle::parse("Validated").unwrap(),
        PipelineLifecycle::Validated
    );
}

#[test]
fn parse_unknown_returns_typed_error() {
    let err = PipelineLifecycle::parse("RUNNING").unwrap_err();
    assert!(matches!(err, LifecycleError::Unknown(value) if value == "RUNNING"));
}

#[test]
fn default_is_draft() {
    assert_eq!(PipelineLifecycle::default(), PipelineLifecycle::Draft);
}

#[test]
fn archived_is_terminal() {
    assert!(PipelineLifecycle::Archived.is_terminal());
    for state in PipelineLifecycle::ALL.iter().copied() {
        if state == PipelineLifecycle::Archived {
            continue;
        }
        assert!(!state.is_terminal(), "{state:?} must not be terminal");
    }
}

// Self-loops are no-ops on every state — they keep update endpoints
// idempotent when the caller resends the current lifecycle.
#[test]
fn self_transitions_are_noops() {
    for state in PipelineLifecycle::ALL.iter().copied() {
        assert_eq!(
            state.transition(state).expect("self transition is allowed"),
            state
        );
    }
}

#[test]
fn legal_transitions_succeed() {
    let cases = [
        // Draft → Validated/Archived
        (PipelineLifecycle::Draft, PipelineLifecycle::Validated),
        (PipelineLifecycle::Draft, PipelineLifecycle::Archived),
        // Validated → Deployed/Draft/Archived
        (PipelineLifecycle::Validated, PipelineLifecycle::Deployed),
        (PipelineLifecycle::Validated, PipelineLifecycle::Draft),
        (PipelineLifecycle::Validated, PipelineLifecycle::Archived),
        // Deployed → Validated/Archived
        (PipelineLifecycle::Deployed, PipelineLifecycle::Validated),
        (PipelineLifecycle::Deployed, PipelineLifecycle::Archived),
    ];
    for (from, to) in cases {
        let next = from
            .transition(to)
            .unwrap_or_else(|err| panic!("expected legal {from:?}->{to:?}: {err}"));
        assert_eq!(next, to);
    }
}

#[test]
fn illegal_transitions_are_rejected_with_typed_error() {
    let cases = [
        // Draft can only go forward to Validated or sideways to Archived.
        (PipelineLifecycle::Draft, PipelineLifecycle::Deployed),
        // Deployed cannot drop straight back to Draft — rollback path is
        // Deployed -> Validated.
        (PipelineLifecycle::Deployed, PipelineLifecycle::Draft),
        // Archived is terminal.
        (PipelineLifecycle::Archived, PipelineLifecycle::Draft),
        (PipelineLifecycle::Archived, PipelineLifecycle::Validated),
        (PipelineLifecycle::Archived, PipelineLifecycle::Deployed),
    ];
    for (from, to) in cases {
        let err = from
            .transition(to)
            .expect_err(&format!("expected illegal {from:?}->{to:?}"));
        match err {
            LifecycleError::IllegalTransition { from: f, to: t } => {
                assert_eq!(f, from);
                assert_eq!(t, to);
            }
            other => panic!("expected IllegalTransition, got {other:?}"),
        }
    }
}
