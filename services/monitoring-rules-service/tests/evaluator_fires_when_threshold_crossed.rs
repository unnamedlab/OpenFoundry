//! Evaluator — fires when the comparator is satisfied.
//!
//! These are pure-Rust unit-style tests over the evaluator helpers
//! that don't require Postgres. The full HTTP/DB end-to-end is
//! exercised by the harness in
//! `services/event-streaming-service/tests/metrics_endpoint_window_aggregation.rs`,
//! which boots a Postgres testcontainer.

use chrono::Utc;
use monitoring_rules_service::evaluator::{InMemoryNotifier, Notifier, StaticMetricsSource};
use monitoring_rules_service::streaming_monitors::{
    Comparator, MonitorKind, MonitorRule, ResourceType, Severity,
};
use uuid::Uuid;

fn rule(kind: MonitorKind, comparator: Comparator, threshold: f64) -> MonitorRule {
    MonitorRule {
        id: Uuid::now_v7(),
        view_id: Uuid::nil(),
        name: "test".into(),
        resource_type: ResourceType::StreamingDataset,
        resource_rid: "ri.streams.main.stream.foo".into(),
        monitor_kind: kind,
        window_seconds: 300,
        comparator,
        threshold,
        severity: Severity::Warn,
        enabled: true,
        created_by: "tests".into(),
        created_at: Utc::now(),
        updated_at: Utc::now(),
    }
}

#[tokio::test]
async fn evaluator_fires_when_threshold_crossed_lt_zero() {
    // Foundry example: "Records ingested" five-minute window with
    // threshold 0 fires when the stream has written zero records.
    let r = rule(MonitorKind::IngestRecords, Comparator::Lte, 0.0);
    assert!(r.comparator.evaluate(0.0, r.threshold));
    assert!(!r.comparator.evaluate(1.0, r.threshold));
}

#[tokio::test]
async fn evaluator_does_not_fire_when_value_above_threshold() {
    let r = rule(MonitorKind::IngestRecords, Comparator::Lte, 1000.0);
    assert!(r.comparator.evaluate(500.0, r.threshold));
    assert!(!r.comparator.evaluate(2000.0, r.threshold));
}

#[tokio::test]
async fn evaluator_supports_gte_for_lag_thresholds() {
    // TOTAL_LAG fires when lag *exceeds* the threshold.
    let r = rule(MonitorKind::TotalLag, Comparator::Gte, 1500.0);
    assert!(r.comparator.evaluate(1500.0, r.threshold));
    assert!(r.comparator.evaluate(2000.0, r.threshold));
    assert!(!r.comparator.evaluate(100.0, r.threshold));
}

#[tokio::test]
async fn static_metrics_source_returns_zero_when_unknown() {
    use monitoring_rules_service::evaluator::MetricsSource;
    let source = StaticMetricsSource::default();
    let r = rule(MonitorKind::IngestRecords, Comparator::Lte, 0.0);
    let observed = source.observe(&r).await.unwrap();
    assert_eq!(observed, 0.0);
}

#[tokio::test]
async fn in_memory_notifier_records_each_fire() {
    let notifier = InMemoryNotifier::default();
    let r = rule(MonitorKind::IngestRecords, Comparator::Lte, 0.0);
    let id = notifier.fire(&r, 0.0, Utc::now()).await.unwrap();
    let id2 = notifier.fire(&r, 0.0, Utc::now()).await.unwrap();
    assert_ne!(id, id2);
    let fired = notifier.fired.lock().await;
    assert_eq!(fired.len(), 2);
}
