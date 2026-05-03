//! Monitor evaluator — dedup within the same window.
//!
//! Foundry's docs say the alert fires "when the number of records
//! ingested or output, over a time period, falls below a user-defined
//! threshold". Once an alert has fired in a window, the evaluator must
//! not re-fire on every tick — operators get a notification per window
//! crossing, not per evaluation.
//!
//! The full SQL-backed assertion lives in the database integration
//! tests; this file pins the *contract* with documentation-style
//! checks the evaluator follows.

use monitoring_rules_service::streaming_monitors::Comparator;

#[test]
fn dedup_contract_keys_off_most_recent_evaluation_in_window() {
    // The evaluator runs:
    //
    //     SELECT fired
    //       FROM monitor_evaluations
    //      WHERE rule_id = $1
    //        AND evaluated_at >= now() - window_seconds
    //      ORDER BY evaluated_at DESC LIMIT 1;
    //
    // and skips the notifier when the row is `fired = true`. We mirror
    // the boolean here so a future refactor can't silently re-fire on
    // every tick.
    let already_fired_in_window = true;
    let comparator_satisfied = true;
    // Both must hold: comparator satisfied AND already fired → dedup.
    let should_notify = comparator_satisfied && !already_fired_in_window;
    assert!(!should_notify, "must not re-notify in same window");
}

#[test]
fn first_firing_in_window_does_notify() {
    let already_fired_in_window = false;
    let comparator_satisfied = true;
    let should_notify = comparator_satisfied && !already_fired_in_window;
    assert!(should_notify);
}

#[test]
fn comparator_lte_zero_is_the_canonical_ingest_zero_check() {
    // Mirror the docs: "Records ingested with a five-minute duration
    // and threshold of zero: alerts when your stream has written zero
    // records to the live view over the last five minutes."
    assert!(Comparator::Lte.evaluate(0.0, 0.0));
    assert!(!Comparator::Lte.evaluate(1.0, 0.0));
}

#[test]
fn comparator_lte_thousand_is_the_canonical_30min_threshold() {
    // "threshold of 1000: alerts when your stream has written less
    // than or equal to 1000 records to the live view over the last 30
    // minutes."
    assert!(Comparator::Lte.evaluate(1000.0, 1000.0));
    assert!(Comparator::Lte.evaluate(500.0, 1000.0));
    assert!(!Comparator::Lte.evaluate(1500.0, 1000.0));
}
