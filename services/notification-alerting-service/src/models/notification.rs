use chrono::{DateTime, Utc};
use event_bus_control::contracts::NotificationEvent as SharedNotificationEvent;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct NotificationRecord {
    pub id: Uuid,
    pub user_id: Option<Uuid>,
    pub title: String,
    pub body: String,
    pub category: String,
    pub severity: String,
    pub status: String,
    pub channels: Value,
    pub metadata: Value,
    pub created_at: DateTime<Utc>,
    pub read_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct NotificationDelivery {
    pub id: Uuid,
    pub notification_id: Uuid,
    pub channel: String,
    pub status: String,
    pub response: Option<String>,
    pub created_at: DateTime<Utc>,
}

pub type NotificationEvent = SharedNotificationEvent<NotificationRecord>;

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct SendNotificationRequest {
    pub user_id: Option<Uuid>,
    pub title: String,
    pub body: String,
    pub severity: Option<String>,
    pub category: Option<String>,
    pub channels: Option<Vec<String>>,
    pub metadata: Option<Value>,
}

#[derive(Debug, Deserialize)]
pub struct ListNotificationsQuery {
    pub status: Option<String>,
    pub limit: Option<i64>,
}

#[derive(Debug, Deserialize)]
pub struct WebSocketQuery {
    pub ticket: String,
}

#[derive(Debug, Serialize)]
pub struct WebSocketTicketResponse {
    pub ticket: String,
    pub expires_in: i64,
}
