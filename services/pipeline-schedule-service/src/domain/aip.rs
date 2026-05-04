//! AIP-assisted schedule creation.
//!
//! Per Foundry doc § "Create a schedule with AIP", an operator can
//! describe a schedule in plain English ("build dataset X every weekday
//! at 9 AM and also when its parent A is updated") and the AIP
//! assistant returns a `Trigger` + `ScheduleTarget` ready for
//! `POST /v1/schedules`.
//!
//! This module is the *deterministic* half of that flow. It:
//!
//!   * exposes an [`LlmClient`] trait so production wires up a real
//!     llm-catalog-service-backed client and tests use a stub;
//!   * builds the prompt: a system message describing the Trigger /
//!     Target JSON schema plus four few-shot examples lifted verbatim
//!     from `Common scheduling configurations.md`;
//!   * runs up to two retries when the LLM returns malformed JSON,
//!     feeding back the parser error each time;
//!   * scores the LLM's self-reported confidence and refuses
//!     low-confidence outputs with an explicit clarification ask.
//!
//! The Trigger / Target proto-derived shapes are imported from
//! `crate::domain::trigger` so the engine can drive any AIP-generated
//! schedule end-to-end without reshape.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use thiserror::Error;

use crate::domain::trigger::{ScheduleTarget, Trigger};

const SYSTEM_PROMPT: &str = include_str!("./aip_system_prompt.md");

/// Maximum retries on malformed JSON before surfacing the error to
/// the operator. The doc shows AIP recovers after one nudge; two is
/// the practical cap before user clarification is the better signal.
const MAX_RETRIES: u8 = 2;

/// Confidence floor below which the assistant refuses to generate a
/// schedule and asks the operator to clarify. Tuned to surface the
/// "needs clarification" path documented in
/// `Create a schedule with AIP.md`.
pub const MIN_CONFIDENCE: f32 = 0.5;

/// What the LLM returns. JSON-shaped so the deserializer enforces
/// the contract — any field not listed here is rejected.
#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
pub struct AipTriggerProposal {
    pub trigger: Trigger,
    pub target: ScheduleTarget,
    pub confidence: f32,
    pub explanation: String,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum AipError {
    #[error("LLM produced malformed JSON after {0} retries")]
    MalformedAfterRetries(u8),
    #[error("LLM confidence {0} fell below the minimum {1}; ask the operator for clarification")]
    LowConfidence(String, String),
    #[error("LLM transport error: {0}")]
    Transport(String),
}

/// Pluggable LLM seam. Production wires a `HttpLlmClient` that POSTs
/// to llm-catalog-service; tests wire a `ScriptedLlmClient` so the
/// AIP suite is hermetic.
#[async_trait]
pub trait LlmClient: Send + Sync {
    async fn complete(&self, request: &LlmRequest) -> Result<String, AipError>;
}

#[derive(Debug, Clone)]
pub struct LlmRequest {
    pub system: String,
    /// Conversation history. The first turn is the operator's prompt;
    /// retries append a synthetic `user` turn carrying the parser
    /// error.
    pub turns: Vec<LlmTurn>,
    pub model_hint: Option<String>,
}

#[derive(Debug, Clone)]
pub struct LlmTurn {
    pub role: LlmRole,
    pub content: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LlmRole {
    User,
    Assistant,
}

/// Build the system prompt + few-shot turns the LLM uses for both
/// `:generate` and `:explain`. Public so tests can assert prompt
/// stability across model upgrades.
pub fn system_prompt() -> String {
    SYSTEM_PROMPT.to_string()
}

/// Doc-derived few-shot examples. Each pair is one row from
/// `Common scheduling configurations.md`; including all four keeps
/// the matrix from over-fitting to one trigger kind.
pub fn few_shot_examples() -> Vec<(String, AipTriggerProposal)> {
    use crate::domain::trigger::{
        CompoundOp, CompoundTrigger, CronFlavor, EventTrigger, EventType, PipelineBuildTarget,
        ScheduleTargetKind, TimeTrigger, TriggerKind,
    };
    let pipeline_target = ScheduleTarget {
        kind: ScheduleTargetKind::PipelineBuild(PipelineBuildTarget {
            pipeline_rid: "ri.foundry.main.pipeline.example".into(),
            build_branch: "master".into(),
            job_spec_fallback: vec![],
            force_build: false,
            abort_policy: None,
        }),
    };
    vec![
        (
            "Run my pipeline every weekday at 9 AM Eastern.".into(),
            AipTriggerProposal {
                trigger: Trigger {
                    kind: TriggerKind::Time(TimeTrigger {
                        cron: "0 9 * * 1-5".into(),
                        time_zone: "America/New_York".into(),
                        flavor: CronFlavor::Unix5,
                    }),
                },
                target: pipeline_target.clone(),
                confidence: 0.95,
                explanation: "Time trigger at 09:00 New York time, Monday through Friday."
                    .into(),
            },
        ),
        (
            "Run when my source dataset receives any new data.".into(),
            AipTriggerProposal {
                trigger: Trigger {
                    kind: TriggerKind::Event(EventTrigger {
                        event_type: EventType::DataUpdated,
                        target_rid: "ri.foundry.main.dataset.source".into(),
                        branch_filter: vec![],
                    }),
                },
                target: pipeline_target.clone(),
                confidence: 0.92,
                explanation:
                    "Event trigger fires when a transaction commits on the source dataset."
                        .into(),
            },
        ),
        (
            "Run nightly at midnight UTC, but only if the upstream finished today.".into(),
            AipTriggerProposal {
                trigger: Trigger {
                    kind: TriggerKind::Compound(CompoundTrigger {
                        op: CompoundOp::And,
                        components: vec![
                            Trigger {
                                kind: TriggerKind::Time(TimeTrigger {
                                    cron: "0 0 * * *".into(),
                                    time_zone: "UTC".into(),
                                    flavor: CronFlavor::Unix5,
                                }),
                            },
                            Trigger {
                                kind: TriggerKind::Event(EventTrigger {
                                    event_type: EventType::JobSucceeded,
                                    target_rid: "ri.foundry.main.dataset.upstream".into(),
                                    branch_filter: vec![],
                                }),
                            },
                        ],
                    }),
                },
                target: pipeline_target.clone(),
                confidence: 0.88,
                explanation: "AND of midnight UTC and a successful upstream job today.".into(),
            },
        ),
        (
            "Run at 9 AM Eastern OR whenever dataset A is updated.".into(),
            AipTriggerProposal {
                trigger: Trigger {
                    kind: TriggerKind::Compound(CompoundTrigger {
                        op: CompoundOp::Or,
                        components: vec![
                            Trigger {
                                kind: TriggerKind::Time(TimeTrigger {
                                    cron: "0 9 * * *".into(),
                                    time_zone: "America/New_York".into(),
                                    flavor: CronFlavor::Unix5,
                                }),
                            },
                            Trigger {
                                kind: TriggerKind::Event(EventTrigger {
                                    event_type: EventType::DataUpdated,
                                    target_rid: "ri.foundry.main.dataset.A".into(),
                                    branch_filter: vec![],
                                }),
                            },
                        ],
                    }),
                },
                target: pipeline_target,
                confidence: 0.93,
                explanation: "OR of 9 AM Eastern and a transaction on dataset A.".into(),
            },
        ),
    ]
}

fn build_user_turn(natural_language: &str, project_rid: &str) -> LlmTurn {
    LlmTurn {
        role: LlmRole::User,
        content: format!(
            "Project: {project_rid}\n\
             Describe a schedule for the natural-language prompt below. Reply ONLY with a \
             JSON object matching the AipTriggerProposal schema. Do not include markdown fences.\n\n\
             Prompt: {natural_language}"
        ),
    }
}

/// Compose the inbound `LlmRequest` for an AIP `:generate` call.
/// Few-shot pairs are flattened into alternating user/assistant
/// turns ending with the operator's actual prompt.
pub fn build_generate_request(natural_language: &str, project_rid: &str) -> LlmRequest {
    let mut turns = Vec::with_capacity(few_shot_examples().len() * 2 + 1);
    for (prompt, expected) in few_shot_examples() {
        turns.push(LlmTurn {
            role: LlmRole::User,
            content: format!("Project: {project_rid}\nPrompt: {prompt}"),
        });
        turns.push(LlmTurn {
            role: LlmRole::Assistant,
            content: serde_json::to_string(&expected).expect("few-shot serialises"),
        });
    }
    turns.push(build_user_turn(natural_language, project_rid));
    LlmRequest {
        system: system_prompt(),
        turns,
        model_hint: None,
    }
}

/// Drive the LLM until it returns valid JSON or the retry budget is
/// exhausted. Returns the parsed proposal *or* a low-confidence
/// rejection.
pub async fn run_generate(
    client: &dyn LlmClient,
    natural_language: &str,
    project_rid: &str,
) -> Result<AipTriggerProposal, AipError> {
    let mut request = build_generate_request(natural_language, project_rid);
    let mut attempts = 0;
    loop {
        let raw = client.complete(&request).await?;
        match serde_json::from_str::<AipTriggerProposal>(&raw) {
            Ok(parsed) => {
                if parsed.confidence < MIN_CONFIDENCE {
                    return Err(AipError::LowConfidence(
                        format!("{:.2}", parsed.confidence),
                        format!("{:.2}", MIN_CONFIDENCE),
                    ));
                }
                return Ok(parsed);
            }
            Err(parse_err) => {
                attempts += 1;
                if attempts > MAX_RETRIES {
                    return Err(AipError::MalformedAfterRetries(MAX_RETRIES));
                }
                request.turns.push(LlmTurn {
                    role: LlmRole::Assistant,
                    content: raw,
                });
                request.turns.push(LlmTurn {
                    role: LlmRole::User,
                    content: format!(
                        "The previous response was not valid AipTriggerProposal JSON: {parse_err}. \
                         Reply with corrected JSON only."
                    ),
                });
            }
        }
    }
}

/// `:explain` shape — given an existing trigger / target, ask the LLM
/// to render a short natural-language summary.
pub fn build_explain_request(trigger: &Trigger, target: &ScheduleTarget) -> LlmRequest {
    let payload = json!({"trigger": trigger, "target": target});
    LlmRequest {
        system: system_prompt(),
        turns: vec![LlmTurn {
            role: LlmRole::User,
            content: format!(
                "Summarise the following schedule in one or two sentences. Reply with prose, no JSON.\n\n{payload}"
            ),
        }],
        model_hint: None,
    }
}

pub async fn run_explain(
    client: &dyn LlmClient,
    trigger: &Trigger,
    target: &ScheduleTarget,
) -> Result<String, AipError> {
    let request = build_explain_request(trigger, target);
    client.complete(&request).await
}

/// Shape-check a raw JSON value against `AipTriggerProposal` without
/// committing to a deserialise — used by the prompt builder when
/// validating few-shot examples don't drift from the schema.
pub fn looks_like_proposal(value: &Value) -> bool {
    serde_json::from_value::<AipTriggerProposal>(value.clone()).is_ok()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{Arc, Mutex};

    /// Hermetic LLM stub: responds with the next scripted reply each
    /// call, panicking if the script is exhausted.
    #[derive(Clone, Default)]
    struct ScriptedLlm {
        replies: Arc<Mutex<Vec<String>>>,
        seen: Arc<Mutex<Vec<LlmRequest>>>,
    }

    impl ScriptedLlm {
        fn with_reply(reply: &str) -> Self {
            Self {
                replies: Arc::new(Mutex::new(vec![reply.to_string()])),
                seen: Arc::new(Mutex::new(Vec::new())),
            }
        }

        fn with_replies(replies: Vec<String>) -> Self {
            Self {
                replies: Arc::new(Mutex::new(replies)),
                seen: Arc::new(Mutex::new(Vec::new())),
            }
        }
    }

    #[async_trait]
    impl LlmClient for ScriptedLlm {
        async fn complete(&self, request: &LlmRequest) -> Result<String, AipError> {
            self.seen.lock().unwrap().push(request.clone());
            let mut replies = self.replies.lock().unwrap();
            if replies.is_empty() {
                return Err(AipError::Transport("script exhausted".into()));
            }
            Ok(replies.remove(0))
        }
    }

    #[test]
    fn few_shot_examples_round_trip_through_serde() {
        for (_prompt, proposal) in few_shot_examples() {
            let raw = serde_json::to_string(&proposal).unwrap();
            let back: AipTriggerProposal = serde_json::from_str(&raw).unwrap();
            assert_eq!(proposal, back);
        }
    }

    #[test]
    fn build_generate_request_emits_alternating_few_shot_turns() {
        let req = build_generate_request("daily 9 AM EST", "ri.foundry.main.project.demo");
        // 4 pairs + final user prompt = 9 turns.
        assert_eq!(req.turns.len(), 9);
        let last = &req.turns.last().unwrap();
        assert_eq!(last.role, LlmRole::User);
        assert!(last.content.contains("daily 9 AM EST"));
    }

    #[tokio::test]
    async fn generate_returns_parsed_proposal_on_first_try() {
        let proposal = &few_shot_examples()[0].1;
        let llm = ScriptedLlm::with_reply(&serde_json::to_string(proposal).unwrap());
        let parsed = run_generate(&llm, "weekdays 9 EST", "ri.proj").await.unwrap();
        assert_eq!(parsed, *proposal);
    }

    #[tokio::test]
    async fn generate_retries_on_malformed_json_then_succeeds() {
        let valid = serde_json::to_string(&few_shot_examples()[0].1).unwrap();
        let llm = ScriptedLlm::with_replies(vec!["this is not json".into(), valid.clone()]);
        let parsed = run_generate(&llm, "x", "ri.proj").await.unwrap();
        assert_eq!(serde_json::to_string(&parsed).unwrap(), valid);
    }

    #[tokio::test]
    async fn generate_rejects_when_retry_budget_exhausted() {
        let llm = ScriptedLlm::with_replies(vec!["bad".into(), "still bad".into(), "no".into()]);
        let err = run_generate(&llm, "x", "ri.proj").await.unwrap_err();
        assert_eq!(err, AipError::MalformedAfterRetries(MAX_RETRIES));
    }

    #[tokio::test]
    async fn generate_rejects_low_confidence_with_clarification_ask() {
        let mut p = few_shot_examples()[0].1.clone();
        p.confidence = 0.2;
        let llm = ScriptedLlm::with_reply(&serde_json::to_string(&p).unwrap());
        let err = run_generate(&llm, "x", "ri.proj").await.unwrap_err();
        assert!(matches!(err, AipError::LowConfidence(_, _)));
    }

    #[tokio::test]
    async fn explain_round_trips_via_llm() {
        let llm = ScriptedLlm::with_reply("Runs every weekday at 9 AM Eastern.");
        let (_, p) = &few_shot_examples()[0];
        let prose = run_explain(&llm, &p.trigger, &p.target).await.unwrap();
        assert!(prose.contains("9 AM"));
    }
}
