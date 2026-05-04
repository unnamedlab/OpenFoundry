//! Production HTTP-backed [`LlmClient`] that POSTs to llm-catalog-service.
//!
//! The wire shape is deliberately tiny: we only need a `complete`
//! endpoint that takes `{system, turns, model_hint?}` and returns the
//! assistant's reply as a string. Anything richer (token usage,
//! safety scores) is opaque to the schedule plane.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::domain::aip::{AipError, LlmClient, LlmRequest, LlmRole};

#[derive(Clone)]
pub struct HttpLlmClient {
    base_url: String,
    inner: reqwest::Client,
    auth_header: Option<String>,
}

impl HttpLlmClient {
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

#[derive(Serialize)]
struct WireRequest<'a> {
    system: &'a str,
    messages: Vec<WireMessage<'a>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    model_hint: Option<&'a str>,
}

#[derive(Serialize)]
struct WireMessage<'a> {
    role: &'a str,
    content: &'a str,
}

#[derive(Deserialize)]
struct WireResponse {
    /// `content` is the assistant's reply text. Other fields the
    /// catalog service may return are ignored.
    content: String,
}

#[async_trait]
impl LlmClient for HttpLlmClient {
    async fn complete(&self, request: &LlmRequest) -> Result<String, AipError> {
        let messages: Vec<WireMessage<'_>> = request
            .turns
            .iter()
            .map(|t| WireMessage {
                role: match t.role {
                    LlmRole::User => "user",
                    LlmRole::Assistant => "assistant",
                },
                content: &t.content,
            })
            .collect();
        let wire = WireRequest {
            system: &request.system,
            messages,
            model_hint: request.model_hint.as_deref(),
        };
        let url = format!("{}/v1/complete", self.base_url.trim_end_matches('/'));
        let mut req = self.inner.post(&url).json(&wire);
        if let Some(h) = &self.auth_header {
            req = req.header("Authorization", h);
        }
        let resp = req
            .send()
            .await
            .map_err(|e| AipError::Transport(format!("transport error: {e}")))?;
        if !resp.status().is_success() {
            let status = resp.status().as_u16();
            return Err(AipError::Transport(format!("llm-catalog-service status {status}")));
        }
        let parsed: WireResponse = resp
            .json()
            .await
            .map_err(|e| AipError::Transport(format!("response parse: {e}")))?;
        Ok(parsed.content)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::aip::{LlmRequest, LlmTurn};

    #[test]
    fn wire_serialisation_uses_user_assistant_roles() {
        let request = LlmRequest {
            system: "system".into(),
            turns: vec![
                LlmTurn {
                    role: LlmRole::User,
                    content: "hi".into(),
                },
                LlmTurn {
                    role: LlmRole::Assistant,
                    content: "hello".into(),
                },
            ],
            model_hint: None,
        };
        let messages: Vec<WireMessage<'_>> = request
            .turns
            .iter()
            .map(|t| WireMessage {
                role: match t.role {
                    LlmRole::User => "user",
                    LlmRole::Assistant => "assistant",
                },
                content: &t.content,
            })
            .collect();
        let wire = WireRequest {
            system: &request.system,
            messages,
            model_hint: None,
        };
        let raw = serde_json::to_string(&wire).unwrap();
        assert!(raw.contains("\"role\":\"user\""));
        assert!(raw.contains("\"role\":\"assistant\""));
    }
}
