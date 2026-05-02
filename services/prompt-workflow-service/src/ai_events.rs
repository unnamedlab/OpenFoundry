//! AI-event publisher substrate (S5.3.a) — `prompt-workflow-service`
//! side. Mirrors the constants and envelope from
//! [`agent_runtime_service::ai_events`] without creating a cross-service
//! dependency: both producers must agree on the wire shape so the
//! `ai-sink` consumer has a single decoder, but neither service should
//! depend on the other at the crate level.

use serde::{Deserialize, Serialize};
use uuid::Uuid;

pub const TOPIC: &str = "ai.events.v1";

/// Transactional-id prefix for this producer (separate prefix per
/// service so Kafka can fence them independently).
pub const TXN_ID_PREFIX: &str = "prompt-workflow-";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum AiEventKind {
    Prompt,
    Response,
    Evaluation,
    Trace,
}

impl AiEventKind {
    pub const fn target_table(self) -> &'static str {
        match self {
            AiEventKind::Prompt => "prompts",
            AiEventKind::Response => "responses",
            AiEventKind::Evaluation => "evaluations",
            AiEventKind::Trace => "traces",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AiEventEnvelope {
    pub event_id: Uuid,
    pub at: i64,
    pub kind: AiEventKind,
    pub run_id: Option<Uuid>,
    pub trace_id: Option<String>,
    pub producer: String,
    pub schema_version: u32,
    pub payload: serde_json::Value,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn topic_and_prefix_pinned() {
        assert_eq!(TOPIC, "ai.events.v1");
        assert_eq!(TXN_ID_PREFIX, "prompt-workflow-");
    }
}
