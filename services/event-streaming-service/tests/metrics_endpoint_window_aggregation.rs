//! Metrics endpoint — window parsing.
//!
//! Pins the contract the monitoring evaluator relies on: `window`
//! accepts `5m`, `30m`, `<n>s`, `<n>h` or a bare integer (seconds),
//! and clamps to `[60, 86400]`.

use event_streaming_service::handlers::streams::parse_window;

#[test]
fn metrics_endpoint_default_window_is_five_minutes() {
    let v = parse_window(None).unwrap();
    assert_eq!(v, 300);
}

#[test]
fn metrics_endpoint_accepts_minutes() {
    assert_eq!(parse_window(Some("5m")).unwrap(), 300);
    assert_eq!(parse_window(Some("30m")).unwrap(), 1800);
}

#[test]
fn metrics_endpoint_accepts_hours_and_seconds() {
    assert_eq!(parse_window(Some("1h")).unwrap(), 3600);
    assert_eq!(parse_window(Some("600s")).unwrap(), 600);
}

#[test]
fn metrics_endpoint_bare_integer_means_seconds() {
    assert_eq!(parse_window(Some("120")).unwrap(), 120);
}

#[test]
fn metrics_endpoint_rejects_outside_documented_range() {
    assert!(parse_window(Some("10s")).is_err());
    assert!(parse_window(Some("48h")).is_err());
    assert!(parse_window(Some("garbage")).is_err());
}

#[test]
fn metrics_endpoint_accepts_milliseconds_and_rounds_to_seconds() {
    // `300000ms` -> 300s; the parser accepts ms for completeness.
    assert_eq!(parse_window(Some("300000ms")).unwrap(), 300);
}
