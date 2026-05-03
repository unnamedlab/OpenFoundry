use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct PrimaryItem {
    pub id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreatePrimaryRequest {
    pub payload: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct SecondaryItem {
    pub id: Uuid,
    pub parent_id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateSecondaryRequest {
    pub payload: serde_json::Value,
}

// ---------------------------------------------------------------------------
// TASK F — Action monitoring rule contract.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum ActionRuleKind {
    ActionDurationP95,
    ActionFailuresInWindow,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateActionRuleRequest {
    pub kind: ActionRuleKind,
    pub action_id: Uuid,
    pub window: String,
    #[serde(default)]
    pub threshold_ms: Option<f64>,
    #[serde(default)]
    pub threshold_count: Option<i64>,
    #[serde(default)]
    pub failure_type: Option<String>,
    #[serde(default = "default_severity")]
    pub severity: String,
}

fn default_severity() -> String {
    "medium".to_string()
}

impl CreateActionRuleRequest {
    pub fn validate(&self) -> Result<(), String> {
        if self.window.trim().is_empty() {
            return Err("window must not be empty".into());
        }
        match self.kind {
            ActionRuleKind::ActionDurationP95 => {
                if self.threshold_ms.is_none() {
                    return Err("threshold_ms is required when kind = action_duration_p95".into());
                }
            }
            ActionRuleKind::ActionFailuresInWindow => {
                if self.threshold_count.is_none() {
                    return Err(
                        "threshold_count is required when kind = action_failures_in_window".into(),
                    );
                }
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn duration_rule_requires_threshold_ms() {
        let req = CreateActionRuleRequest {
            kind: ActionRuleKind::ActionDurationP95,
            action_id: Uuid::nil(),
            window: "1h".into(),
            threshold_ms: None,
            threshold_count: None,
            failure_type: None,
            severity: "medium".into(),
        };
        assert!(req.validate().is_err());
    }

    #[test]
    fn failures_rule_requires_threshold_count() {
        let req = CreateActionRuleRequest {
            kind: ActionRuleKind::ActionFailuresInWindow,
            action_id: Uuid::nil(),
            window: "1h".into(),
            threshold_ms: Some(100.0),
            threshold_count: None,
            failure_type: None,
            severity: "medium".into(),
        };
        assert!(req.validate().is_err());
    }

    #[test]
    fn well_formed_rules_validate() {
        let p95 = CreateActionRuleRequest {
            kind: ActionRuleKind::ActionDurationP95,
            action_id: Uuid::nil(),
            window: "30d".into(),
            threshold_ms: Some(500.0),
            threshold_count: None,
            failure_type: None,
            severity: "high".into(),
        };
        assert!(p95.validate().is_ok());
        let failures = CreateActionRuleRequest {
            kind: ActionRuleKind::ActionFailuresInWindow,
            action_id: Uuid::nil(),
            window: "1h".into(),
            threshold_ms: None,
            threshold_count: Some(5),
            failure_type: Some("invalid_parameter".into()),
            severity: "high".into(),
        };
        assert!(failures.validate().is_ok());
    }
}
