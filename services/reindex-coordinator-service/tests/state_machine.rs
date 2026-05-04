//! State-machine regression tests. Live alongside the unit tests
//! in `src/state.rs` but exercise the public surface (`pub use`
//! exports from `lib.rs`) so a refactor that breaks the wire
//! contract trips here too.

use reindex_coordinator_service::{JobStatus, StateError};

#[test]
fn enum_round_trips_through_wire_string() {
    for (s, wire) in [
        (JobStatus::Queued, "queued"),
        (JobStatus::Running, "running"),
        (JobStatus::Completed, "completed"),
        (JobStatus::Failed, "failed"),
        (JobStatus::Cancelled, "cancelled"),
    ] {
        assert_eq!(s.as_str(), wire, "wire string for {s:?}");
        assert_eq!(JobStatus::parse(wire).unwrap(), s);
    }
}

#[test]
fn happy_path_transitions_are_legal() {
    JobStatus::Queued
        .validate_transition(JobStatus::Running)
        .unwrap();
    JobStatus::Running
        .validate_transition(JobStatus::Completed)
        .unwrap();
}

#[test]
fn cancel_path_is_legal_from_either_pre_terminal_state() {
    JobStatus::Queued
        .validate_transition(JobStatus::Cancelled)
        .unwrap();
    JobStatus::Running
        .validate_transition(JobStatus::Cancelled)
        .unwrap();
}

#[test]
fn cannot_skip_running_to_completed() {
    let err = JobStatus::Queued
        .validate_transition(JobStatus::Completed)
        .unwrap_err();
    assert!(matches!(
        err,
        StateError::IllegalTransition {
            from: JobStatus::Queued,
            to: JobStatus::Completed,
        }
    ));
}

#[test]
fn terminal_states_never_resurrect() {
    for terminal in [
        JobStatus::Completed,
        JobStatus::Failed,
        JobStatus::Cancelled,
    ] {
        for next in [JobStatus::Queued, JobStatus::Running] {
            assert!(terminal.validate_transition(next).is_err());
        }
    }
}

#[test]
fn idempotent_self_loop_is_allowed() {
    for s in [
        JobStatus::Completed,
        JobStatus::Failed,
        JobStatus::Cancelled,
    ] {
        s.validate_transition(s).unwrap();
    }
}

#[test]
fn unknown_status_strings_rejected() {
    let err = JobStatus::parse("running ").unwrap_err();
    assert!(matches!(err, StateError::UnknownStatus(_)));
}
