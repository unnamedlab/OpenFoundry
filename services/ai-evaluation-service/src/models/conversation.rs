use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GuardrailFlag {
    pub kind: String,
    pub severity: String,
    pub excerpt: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GuardrailVerdict {
    pub status: String,
    pub redacted_text: String,
    pub blocked: bool,
    pub flags: Vec<GuardrailFlag>,
}

impl Default for GuardrailVerdict {
    fn default() -> Self {
        Self {
            status: "passed".to_string(),
            redacted_text: String::new(),
            blocked: false,
            flags: Vec::new(),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatAttachment {
    #[serde(default = "default_attachment_kind")]
    pub kind: String,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub mime_type: Option<String>,
    #[serde(default)]
    pub url: Option<String>,
    #[serde(default)]
    pub base64_data: Option<String>,
    #[serde(default)]
    pub text: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmUsageSummary {
    pub prompt_tokens: i32,
    pub completion_tokens: i32,
    pub total_tokens: i32,
    pub estimated_cost_usd: f32,
    pub latency_ms: i32,
    pub network_scope: String,
    pub cache_hit: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EvaluateGuardrailsRequest {
    pub content: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EvaluateGuardrailsResponse {
    pub verdict: GuardrailVerdict,
    pub risk_score: f32,
    pub recommendations: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderBenchmarkRequest {
    pub prompt: String,
    pub system_prompt: Option<String>,
    #[serde(default)]
    pub provider_ids: Vec<Uuid>,
    #[serde(default)]
    pub attachments: Vec<ChatAttachment>,
    #[serde(default)]
    pub rubric_keywords: Vec<String>,
    #[serde(default = "default_benchmark_use_case")]
    pub use_case: String,
    #[serde(default = "default_max_tokens")]
    pub max_tokens: i32,
    #[serde(default)]
    pub require_private_network: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderBenchmarkScore {
    pub quality: f32,
    pub latency: f32,
    pub cost: f32,
    pub safety: f32,
    pub overall: f32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderBenchmarkResult {
    pub provider_id: Uuid,
    pub provider_name: String,
    pub network_scope: String,
    pub reply_preview: String,
    pub prompt_tokens: i32,
    pub completion_tokens: i32,
    pub total_tokens: i32,
    pub estimated_cost_usd: f32,
    pub latency_ms: i32,
    pub cache_hit: bool,
    pub guardrail: GuardrailVerdict,
    pub score: ProviderBenchmarkScore,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderBenchmarkResponse {
    pub benchmark_group_id: Uuid,
    pub use_case: String,
    pub prompt_excerpt: String,
    pub required_modalities: Vec<String>,
    pub requested_private_network: bool,
    pub recommended_provider_id: Option<Uuid>,
    pub results: Vec<ProviderBenchmarkResult>,
    pub created_at: DateTime<Utc>,
}

fn default_attachment_kind() -> String {
    "text".to_string()
}

fn default_max_tokens() -> i32 {
    1024
}

fn default_benchmark_use_case() -> String {
    "chat".to_string()
}
