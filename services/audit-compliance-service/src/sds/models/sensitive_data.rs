use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SensitiveDataFinding {
    pub kind: String,
    pub value: String,
    pub redacted: String,
    pub match_count: usize,
    pub severity: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SensitiveDataScope {
    Record,
    Dataset,
    File,
    Prompt,
    Message,
}

impl SensitiveDataScope {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Record => "record",
            Self::Dataset => "dataset",
            Self::File => "file",
            Self::Prompt => "prompt",
            Self::Message => "message",
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct SensitiveDataScanRequest {
    pub content: String,
    #[serde(default = "default_redact")]
    pub redact: bool,
    #[serde(default)]
    pub scope: SensitiveDataScope,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SensitiveDataScanResponse {
    pub findings: Vec<SensitiveDataFinding>,
    pub redacted_content: String,
    pub risk_score: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RunSensitiveDataScanRequest {
    pub target_name: String,
    pub content: String,
    #[serde(default = "default_redact")]
    pub redact: bool,
    #[serde(default)]
    pub scope: SensitiveDataScope,
    pub requested_by: Option<Uuid>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SensitiveDataScanJob {
    pub id: Uuid,
    pub target_name: String,
    pub scope: SensitiveDataScope,
    pub status: String,
    pub risk_score: u32,
    pub findings: Vec<SensitiveDataFinding>,
    pub issue_count: i32,
    pub redacted_content: String,
    pub remediations: Vec<String>,
    pub requested_by: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum IssueStatus {
    Open,
    Resolved,
    Suppressed,
}

impl IssueStatus {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Open => "open",
            Self::Resolved => "resolved",
            Self::Suppressed => "suppressed",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SensitiveDataIssue {
    pub id: Uuid,
    pub job_id: Uuid,
    pub kind: String,
    pub severity: String,
    pub status: IssueStatus,
    pub matched_value: String,
    pub redacted_value: String,
    pub match_count: usize,
    pub markings: Vec<String>,
    pub remediation_actions: Vec<String>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct MarkSensitiveIssueRequest {
    #[serde(default)]
    pub markings: Vec<String>,
    #[serde(default)]
    pub remediation_actions: Vec<String>,
    #[serde(default)]
    pub resolve: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MatchCondition {
    pub field: String,
    pub operator: String,
    pub value: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RemediationRule {
    pub id: Uuid,
    pub name: String,
    pub scope: String,
    pub match_conditions: Vec<MatchCondition>,
    pub remediation_actions: Vec<String>,
    pub updated_by: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateRemediationRuleRequest {
    pub name: String,
    pub scope: String,
    #[serde(default)]
    pub match_conditions: Vec<MatchCondition>,
    #[serde(default)]
    pub remediation_actions: Vec<String>,
    pub updated_by: Option<Uuid>,
}

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct SensitiveDataScanJobRow {
    pub id: Uuid,
    pub target_name: String,
    pub scope: String,
    pub status: String,
    pub risk_score: i32,
    pub findings: Value,
    pub issue_count: i32,
    pub redacted_content: String,
    pub remediations: Value,
    pub requested_by: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, sqlx::FromRow)]
pub struct SensitiveDataIssueRow {
    pub id: Uuid,
    pub job_id: Uuid,
    pub kind: String,
    pub severity: String,
    pub status: String,
    pub matched_value: String,
    pub redacted_value: String,
    pub match_count: i32,
    pub markings: Value,
    pub remediation_actions: Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl Default for SensitiveDataScope {
    fn default() -> Self {
        Self::Record
    }
}

fn default_redact() -> bool {
    true
}
