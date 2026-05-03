use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WindowDefinition {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub status: String,
    pub window_type: String,
    pub duration_seconds: i32,
    pub slide_seconds: i32,
    pub session_gap_seconds: i32,
    pub allowed_lateness_seconds: i32,
    pub aggregation_keys: Vec<String>,
    pub measure_fields: Vec<String>,
    /// Bloque P6 — Foundry "Streaming keys" + "Stateful transforms".
    /// When true, the operator runs `key_by(key_columns)` before
    /// windowing. The runtime applies an operator-state TTL of
    /// `state_ttl_seconds` (0 disables TTL).
    #[serde(default)]
    pub keyed: bool,
    #[serde(default)]
    pub key_columns: Vec<String>,
    #[serde(default)]
    pub state_ttl_seconds: i32,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateWindowRequest {
    pub name: String,
    pub description: Option<String>,
    pub status: Option<String>,
    pub window_type: Option<String>,
    pub duration_seconds: Option<i32>,
    pub slide_seconds: Option<i32>,
    pub session_gap_seconds: Option<i32>,
    pub allowed_lateness_seconds: Option<i32>,
    pub aggregation_keys: Vec<String>,
    pub measure_fields: Vec<String>,
    #[serde(default)]
    pub keyed: Option<bool>,
    #[serde(default)]
    pub key_columns: Option<Vec<String>>,
    #[serde(default)]
    pub state_ttl_seconds: Option<i32>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateWindowRequest {
    pub name: Option<String>,
    pub description: Option<String>,
    pub status: Option<String>,
    pub window_type: Option<String>,
    pub duration_seconds: Option<i32>,
    pub slide_seconds: Option<i32>,
    pub session_gap_seconds: Option<i32>,
    pub allowed_lateness_seconds: Option<i32>,
    pub aggregation_keys: Option<Vec<String>>,
    pub measure_fields: Option<Vec<String>>,
    #[serde(default)]
    pub keyed: Option<bool>,
    #[serde(default)]
    pub key_columns: Option<Vec<String>>,
    #[serde(default)]
    pub state_ttl_seconds: Option<i32>,
}

#[derive(Debug, Clone, FromRow)]
pub struct WindowRow {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub status: String,
    pub window_type: String,
    pub duration_seconds: i32,
    pub slide_seconds: i32,
    pub session_gap_seconds: i32,
    pub allowed_lateness_seconds: i32,
    pub aggregation_keys: SqlJson<Vec<String>>,
    pub measure_fields: SqlJson<Vec<String>>,
    pub keyed: bool,
    pub key_columns: SqlJson<Vec<String>>,
    pub state_ttl_seconds: i32,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<WindowRow> for WindowDefinition {
    fn from(value: WindowRow) -> Self {
        Self {
            id: value.id,
            name: value.name,
            description: value.description,
            status: value.status,
            window_type: value.window_type,
            duration_seconds: value.duration_seconds,
            slide_seconds: value.slide_seconds,
            session_gap_seconds: value.session_gap_seconds,
            allowed_lateness_seconds: value.allowed_lateness_seconds,
            aggregation_keys: value.aggregation_keys.0,
            measure_fields: value.measure_fields.0,
            keyed: value.keyed,
            key_columns: value.key_columns.0,
            state_ttl_seconds: value.state_ttl_seconds,
            created_at: value.created_at,
            updated_at: value.updated_at,
        }
    }
}

/// Pure helper used by the runtime: turns a window definition into the
/// per-key state key prefix Flink will use. Mirrors the Foundry doc:
/// "Each unique value of `key_columns` maps to its own state slice."
pub fn key_prefix_for(window: &WindowDefinition, record: &serde_json::Value) -> Option<String> {
    if !window.keyed || window.key_columns.is_empty() {
        return None;
    }
    let parts: Vec<String> = window
        .key_columns
        .iter()
        .map(|col| {
            record
                .get(col)
                .map(|v| match v {
                    serde_json::Value::Null => String::new(),
                    serde_json::Value::String(s) => s.clone(),
                    other => other.to_string(),
                })
                .unwrap_or_default()
        })
        .collect();
    Some(parts.join("|"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;
    use serde_json::json;

    fn def(keyed: bool, columns: Vec<&str>) -> WindowDefinition {
        WindowDefinition {
            id: Uuid::nil(),
            name: "w".into(),
            description: "".into(),
            status: "active".into(),
            window_type: "tumbling".into(),
            duration_seconds: 60,
            slide_seconds: 60,
            session_gap_seconds: 0,
            allowed_lateness_seconds: 0,
            aggregation_keys: vec![],
            measure_fields: vec![],
            keyed,
            key_columns: columns.into_iter().map(String::from).collect(),
            state_ttl_seconds: 3600,
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    #[test]
    fn key_prefix_emits_per_key_state() {
        let w = def(true, vec!["customer_id", "country"]);
        let r = json!({"customer_id": "c-1", "country": "US"});
        assert_eq!(key_prefix_for(&w, &r), Some("c-1|US".to_string()));
    }

    #[test]
    fn key_prefix_returns_none_when_not_keyed() {
        let w = def(false, vec![]);
        let r = json!({"customer_id": "c-1"});
        assert_eq!(key_prefix_for(&w, &r), None);
    }

    #[test]
    fn key_prefix_handles_missing_columns() {
        let w = def(true, vec!["customer_id", "country"]);
        let r = json!({"customer_id": "c-1"});
        assert_eq!(key_prefix_for(&w, &r), Some("c-1|".to_string()));
    }
}
