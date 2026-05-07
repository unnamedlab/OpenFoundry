//! Outbound HTTP client to `notification-alerting-service`. Used by
//! the auto-pause supervisor to surface a `schedule_auto_paused`
//! notification to the schedule's owner.
//!
//! The trait is the seam tests use to stub the call without standing
//! up the notification service. The production impl posts to
//! `POST /v1/notifications` with the typed payload that service
//! exposes via `models::notification::SendNotificationRequest`.

use async_trait::async_trait;
use serde::Serialize;
use serde_json::json;
use uuid::Uuid;

#[async_trait]
pub trait NotificationClient: Send + Sync {
    async fn send_auto_paused(&self, alert: AutoPausedAlert);
}

/// Payload the supervisor hands to [`NotificationClient::send_auto_paused`].
/// Carries everything the email/in-app template renders.
#[derive(Debug, Clone)]
pub struct AutoPausedAlert {
    pub user_id: Option<Uuid>,
    pub schedule_rid: String,
    pub schedule_name: String,
    pub last_failure_reason: Option<String>,
    pub failed_run_rids: Vec<String>,
    pub link: String,
}

#[derive(Debug, Clone, Serialize)]
struct SendNotificationBody {
    user_id: Option<Uuid>,
    title: String,
    body: String,
    severity: Option<String>,
    category: Option<String>,
    channels: Option<Vec<String>>,
    metadata: Option<serde_json::Value>,
}

/// Production HTTP client. Falls back to a tracing warn on any
/// transport error — auto-pause is always best-effort and never blocks
/// the pause itself from happening.
#[derive(Clone)]
pub struct HttpNotificationClient {
    base_url: String,
    inner: reqwest::Client,
    auth_header: Option<String>,
}

impl HttpNotificationClient {
    pub fn new(base_url: impl Into<String>, inner: reqwest::Client) -> Self {
        Self {
            base_url: base_url.into(),
            inner,
            auth_header: None,
        }
    }

    pub fn with_bearer(mut self, token: impl Into<String>) -> Self {
        self.auth_header = Some(format!("Bearer {}", token.into()));
        self
    }
}

#[async_trait]
impl NotificationClient for HttpNotificationClient {
    async fn send_auto_paused(&self, alert: AutoPausedAlert) {
        let body = SendNotificationBody {
            user_id: alert.user_id,
            title: format!("Schedule auto-paused: {}", alert.schedule_name),
            body: format!(
                "The schedule `{}` was auto-paused after consecutive failures. \
                 Last failure reason: {}.",
                alert.schedule_rid,
                alert.last_failure_reason.as_deref().unwrap_or("unknown"),
            ),
            severity: Some("warning".to_string()),
            category: Some("schedule".to_string()),
            channels: Some(vec!["in_app".to_string(), "email".to_string()]),
            metadata: Some(json!({
                "template": "schedule_auto_paused",
                "schedule_rid": alert.schedule_rid,
                "failed_run_rids": alert.failed_run_rids,
                "link": alert.link,
            })),
        };
        let url = format!("{}/v1/notifications", self.base_url.trim_end_matches('/'));
        let mut req = self.inner.post(&url).json(&body);
        if let Some(header) = &self.auth_header {
            req = req.header("Authorization", header);
        }
        if let Err(error) = req.send().await {
            tracing::warn!(
                schedule_rid = %alert.schedule_rid,
                ?error,
                "auto-paused notification send failed"
            );
        }
    }
}

/// In-process no-op implementation. Used by `main.rs` when the
/// `NOTIFICATION_SERVICE_URL` env var is unset (single-binary smoke
/// runs) and by tests that don't care about delivery.
#[derive(Clone, Default)]
pub struct LoggingNotificationClient;

#[async_trait]
impl NotificationClient for LoggingNotificationClient {
    async fn send_auto_paused(&self, alert: AutoPausedAlert) {
        tracing::info!(
            schedule_rid = %alert.schedule_rid,
            failure_reason = ?alert.last_failure_reason,
            "schedule auto-paused (notification logged, not sent)"
        );
    }
}
