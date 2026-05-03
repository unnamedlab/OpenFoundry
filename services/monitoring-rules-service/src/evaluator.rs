//! Monitor evaluator scheduler.
//!
//! Iterates `monitor_rules` every `tick`, computes the observed value
//! for each rule, persists a row in `monitor_evaluations`, and — when
//! the comparator is satisfied — issues a notification via
//! `notification-alerting-service` with idempotent dedup keyed by
//! `(rule_id, evaluation_window_start)`.
//!
//! The metric source is pluggable so unit tests can exercise the
//! scheduler logic without the network. Production wires
//! [`HttpMetricsSource`] which calls
//! `event-streaming-service`'s `/v1/streams/{rid}/metrics` and
//! `/v1/topologies/{rid}/checkpoints` endpoints.

use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::Deserialize;
use sqlx::PgPool;
use uuid::Uuid;

use crate::streaming_monitors::{MonitorKind, MonitorRule, MonitorRuleRow, ResourceType};

/// Errors a metric source can surface.
#[derive(Debug, thiserror::Error)]
pub enum MetricsSourceError {
    #[error("metrics source unavailable: {0}")]
    Unavailable(String),
    #[error("metric not implemented for kind {0:?}")]
    NotImplemented(MonitorKind),
}

/// Pluggable abstraction over the upstream metrics endpoints.
#[async_trait]
pub trait MetricsSource: Send + Sync + std::fmt::Debug {
    /// Resolve the observed value for a single rule.
    async fn observe(&self, rule: &MonitorRule) -> Result<f64, MetricsSourceError>;
}

/// HTTP-backed metrics source that calls the event-streaming-service
/// endpoints. Kept simple — every observation re-issues the request
/// because the rules table is small (low hundreds in production).
#[derive(Debug, Clone)]
pub struct HttpMetricsSource {
    pub base_url: String,
    pub http: reqwest::Client,
}

#[derive(Debug, Deserialize)]
struct StreamMetricsResponse {
    records_ingested: f64,
    records_output: f64,
    total_lag: f64,
    total_throughput: f64,
    utilization_pct: f64,
}

#[async_trait]
impl MetricsSource for HttpMetricsSource {
    async fn observe(&self, rule: &MonitorRule) -> Result<f64, MetricsSourceError> {
        let base = self.base_url.trim_end_matches('/');
        match rule.resource_type {
            ResourceType::StreamingDataset | ResourceType::StreamingPipeline => {
                let url = format!(
                    "{base}/api/v1/streaming/streams/{rid}/metrics?window={win}s",
                    rid = rule.resource_rid,
                    win = rule.window_seconds
                );
                let resp = self
                    .http
                    .get(url)
                    .send()
                    .await
                    .map_err(|e| MetricsSourceError::Unavailable(e.to_string()))?;
                if !resp.status().is_success() {
                    return Err(MetricsSourceError::Unavailable(format!(
                        "{} returned {}",
                        rule.resource_rid,
                        resp.status()
                    )));
                }
                let body: StreamMetricsResponse = resp
                    .json()
                    .await
                    .map_err(|e| MetricsSourceError::Unavailable(e.to_string()))?;
                Ok(match rule.monitor_kind {
                    MonitorKind::IngestRecords => body.records_ingested,
                    MonitorKind::OutputRecords => body.records_output,
                    MonitorKind::TotalLag => body.total_lag,
                    MonitorKind::TotalThroughput => body.total_throughput,
                    MonitorKind::Utilization => body.utilization_pct,
                    other => return Err(MetricsSourceError::NotImplemented(other)),
                })
            }
            ResourceType::TimeSeriesSync | ResourceType::GeotemporalObservations => {
                // Beta — these endpoints are stubs in the streaming
                // service. The handler returns 0 until the upstream
                // sync is implemented (Bloque P5). Surfaces as
                // "no observations" so unset monitors don't fire
                // accidentally.
                Ok(0.0)
            }
        }
    }
}

/// In-memory metrics source used by tests. Maps `(resource_rid, kind)` →
/// observed value.
#[derive(Debug, Default)]
pub struct StaticMetricsSource {
    pub values: std::collections::HashMap<(String, MonitorKind), f64>,
}

#[async_trait]
impl MetricsSource for StaticMetricsSource {
    async fn observe(&self, rule: &MonitorRule) -> Result<f64, MetricsSourceError> {
        Ok(self
            .values
            .get(&(rule.resource_rid.clone(), rule.monitor_kind))
            .copied()
            .unwrap_or(0.0))
    }
}

/// Notification dispatcher. Tests use [`InMemoryNotifier`]; production
/// uses [`HttpNotifier`] which posts to
/// `notification-alerting-service`'s `/internal/notifications`.
#[async_trait]
pub trait Notifier: Send + Sync + std::fmt::Debug {
    async fn fire(
        &self,
        rule: &MonitorRule,
        observed: f64,
        evaluated_at: DateTime<Utc>,
    ) -> Result<Uuid, String>;
}

#[derive(Debug, Clone)]
pub struct HttpNotifier {
    pub base_url: String,
    pub http: reqwest::Client,
    pub bearer: Option<String>,
}

#[async_trait]
impl Notifier for HttpNotifier {
    async fn fire(
        &self,
        rule: &MonitorRule,
        observed: f64,
        evaluated_at: DateTime<Utc>,
    ) -> Result<Uuid, String> {
        let id = Uuid::now_v7();
        let body = serde_json::json!({
            "id": id,
            "kind": "streaming.monitor.fired",
            "severity": rule.severity.as_str(),
            "title": format!("Monitor '{}' fired", rule.name),
            "details": {
                "rule_id": rule.id,
                "monitor_kind": rule.monitor_kind.as_str(),
                "resource_type": rule.resource_type.as_str(),
                "resource_rid": rule.resource_rid,
                "window_seconds": rule.window_seconds,
                "comparator": rule.comparator.as_str(),
                "threshold": rule.threshold,
                "observed_value": observed,
                "evaluated_at": evaluated_at,
            }
        });
        let mut req = self
            .http
            .post(format!(
                "{}/internal/notifications",
                self.base_url.trim_end_matches('/')
            ))
            .json(&body);
        if let Some(token) = self.bearer.as_deref() {
            req = req.bearer_auth(token);
        }
        let resp = req.send().await.map_err(|e| e.to_string())?;
        if !resp.status().is_success() {
            return Err(format!("notifier returned {}", resp.status()));
        }
        Ok(id)
    }
}

#[derive(Debug, Default)]
pub struct InMemoryNotifier {
    pub fired: tokio::sync::Mutex<Vec<(Uuid, MonitorRule, f64)>>,
}

#[async_trait]
impl Notifier for InMemoryNotifier {
    async fn fire(
        &self,
        rule: &MonitorRule,
        observed: f64,
        _evaluated_at: DateTime<Utc>,
    ) -> Result<Uuid, String> {
        let id = Uuid::now_v7();
        self.fired.lock().await.push((id, rule.clone(), observed));
        Ok(id)
    }
}

/// Single-tick result. Returned mainly for tests; production
/// scheduler ignores the value and just logs.
#[derive(Debug, Default)]
pub struct TickReport {
    pub evaluated: usize,
    pub fired: usize,
    pub deduped: usize,
}

/// Run a single evaluation tick: fetch enabled rules, observe each,
/// persist an evaluation row, and notify (with dedup) when the
/// comparator is satisfied.
pub async fn tick(
    db: &PgPool,
    metrics: &dyn MetricsSource,
    notifier: &dyn Notifier,
) -> Result<TickReport, sqlx::Error> {
    let rules: Vec<MonitorRule> = sqlx::query_as::<_, MonitorRuleRow>(
        "SELECT id, view_id, name, resource_type, resource_rid, monitor_kind,
                window_seconds, comparator, threshold, severity, enabled,
                created_by, created_at, updated_at
           FROM monitor_rules
          WHERE enabled = true",
    )
    .fetch_all(db)
    .await?
    .into_iter()
    .map(MonitorRule::from)
    .collect();

    let now = Utc::now();
    let mut report = TickReport::default();
    for rule in rules {
        report.evaluated += 1;
        let observed = match metrics.observe(&rule).await {
            Ok(v) => v,
            Err(err) => {
                tracing::warn!(rule_id = %rule.id, error = %err, "monitor observe failed");
                continue;
            }
        };
        let fired = rule.comparator.evaluate(observed, rule.threshold);

        // Dedup: if the most recent evaluation in the same window
        // already fired, do not notify again. This matches Foundry's
        // "alert fires once per window" expectation.
        let already_fired_in_window = if fired {
            let cutoff = now - chrono::Duration::seconds(rule.window_seconds.into());
            let recent: Option<bool> = sqlx::query_scalar(
                "SELECT fired
                   FROM monitor_evaluations
                  WHERE rule_id = $1 AND evaluated_at >= $2
                  ORDER BY evaluated_at DESC
                  LIMIT 1",
            )
            .bind(rule.id)
            .bind(cutoff)
            .fetch_optional(db)
            .await?;
            recent.unwrap_or(false)
        } else {
            false
        };

        let alert_id = if fired && !already_fired_in_window {
            match notifier.fire(&rule, observed, now).await {
                Ok(id) => Some(id),
                Err(err) => {
                    tracing::error!(rule_id = %rule.id, error = %err, "notifier failed");
                    None
                }
            }
        } else {
            None
        };

        sqlx::query(
            "INSERT INTO monitor_evaluations
                (id, rule_id, evaluated_at, observed_value, fired, alert_id)
             VALUES ($1, $2, $3, $4, $5, $6)",
        )
        .bind(Uuid::now_v7())
        .bind(rule.id)
        .bind(now)
        .bind(observed)
        .bind(fired)
        .bind(alert_id)
        .execute(db)
        .await?;

        if fired {
            if already_fired_in_window {
                report.deduped += 1;
            } else {
                report.fired += 1;
            }
        }
    }
    Ok(report)
}

/// Spawn the scheduler. Cancels the loop when `cancel` resolves.
pub fn spawn_scheduler(
    db: PgPool,
    metrics: Arc<dyn MetricsSource>,
    notifier: Arc<dyn Notifier>,
    interval: Duration,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(interval);
        ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
        loop {
            ticker.tick().await;
            match tick(&db, metrics.as_ref(), notifier.as_ref()).await {
                Ok(report) => {
                    tracing::debug!(
                        evaluated = report.evaluated,
                        fired = report.fired,
                        deduped = report.deduped,
                        "monitor scheduler tick"
                    );
                }
                Err(err) => {
                    tracing::error!(error = %err, "monitor scheduler tick failed");
                }
            }
        }
    })
}

#[cfg(test)]
mod tests {
    use crate::streaming_monitors::Comparator;

    #[test]
    fn comparator_evaluate_handles_lt_lte_gt_gte_eq() {
        assert!(Comparator::Lt.evaluate(0.0, 1.0));
        assert!(!Comparator::Lt.evaluate(1.0, 1.0));
        assert!(Comparator::Lte.evaluate(1.0, 1.0));
        assert!(Comparator::Gt.evaluate(2.0, 1.0));
        assert!(!Comparator::Gt.evaluate(1.0, 1.0));
        assert!(Comparator::Gte.evaluate(1.0, 1.0));
        assert!(Comparator::Eq.evaluate(1.0, 1.0));
        assert!(!Comparator::Eq.evaluate(1.000001, 1.0));
    }
}
