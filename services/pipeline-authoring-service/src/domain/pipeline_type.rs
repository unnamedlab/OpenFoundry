//! Pipeline-type coherence validator.
//!
//! Each `PipelineType` has documented prerequisites that the authoring
//! API must enforce before persistence. References:
//!
//! * `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Types of pipelines.md`
//!   (Ref: Types of pipelines.screenshot.png)
//! * `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Considerations Pipeline Builder and Code Repositories.md`
//!
//! Rules implemented:
//!
//! * `STREAMING` — requires `streaming.input_stream_id` (the upstream
//!   stream the pipeline reads from). Foundry surfaces this as a hard
//!   error in the Builder; without it the runner has nowhere to consume
//!   from.
//! * `EXTERNAL` — requires `external.source_system` set to a recognised
//!   external compute system (Databricks / Snowflake), per the External
//!   pipelines pattern. We accept any non-empty string and let the
//!   downstream connector validate the name against its catalog.
//! * `INCREMENTAL` — `incremental.allowed_transaction_types`, when set,
//!   must be a comma-separated list of recognised values. Empty means
//!   "default to APPEND,UPDATE" so it stays optional.
//! * Per-type forbids: `BATCH`/`FASTER`/`INCREMENTAL`/`STREAMING` cannot
//!   carry `external` config; `EXTERNAL` cannot carry `streaming` config.
//!
//! Returns a flat `Vec<String>` of error messages; empty means valid.
//! Pure logic, no IO — exposed via `lib.rs` for integration tests.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "UPPERCASE")]
pub enum PipelineType {
    Batch,
    Faster,
    Incremental,
    Streaming,
    External,
}

impl PipelineType {
    pub fn as_str(self) -> &'static str {
        match self {
            PipelineType::Batch => "BATCH",
            PipelineType::Faster => "FASTER",
            PipelineType::Incremental => "INCREMENTAL",
            PipelineType::Streaming => "STREAMING",
            PipelineType::External => "EXTERNAL",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "BATCH" => Some(PipelineType::Batch),
            "FASTER" => Some(PipelineType::Faster),
            "INCREMENTAL" => Some(PipelineType::Incremental),
            "STREAMING" => Some(PipelineType::Streaming),
            "EXTERNAL" => Some(PipelineType::External),
            _ => None,
        }
    }
}

impl Default for PipelineType {
    fn default() -> Self {
        PipelineType::Batch
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct ExternalConfig {
    pub source_system: String,
    pub source_id: Option<String>,
    pub compute_profile_id: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct IncrementalConfig {
    pub replay_on_deploy: bool,
    pub watermark_columns: Vec<String>,
    /// Comma-separated subset of `APPEND,UPDATE,DELETE,SNAPSHOT`. When
    /// empty the runner falls back to `APPEND,UPDATE` (Foundry default).
    pub allowed_transaction_types: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
#[serde(default)]
pub struct StreamingConfig {
    pub input_stream_id: Option<String>,
    pub output_stream_id: Option<String>,
    pub streaming_profile_id: Option<String>,
    pub parallelism: u32,
}

const VALID_TX_TYPES: &[&str] = &["APPEND", "UPDATE", "DELETE", "SNAPSHOT"];

/// Coherence validator. Returns the list of human-readable errors.
/// Empty `Vec` means coherent.
pub fn validate_pipeline_type_coherence(
    pipeline_type: PipelineType,
    external: Option<&ExternalConfig>,
    incremental: Option<&IncrementalConfig>,
    streaming: Option<&StreamingConfig>,
) -> Vec<String> {
    let mut errors = Vec::new();

    match pipeline_type {
        PipelineType::Streaming => {
            let has_input = streaming
                .map(|s| s.input_stream_id.as_deref().is_some_and(|v| !v.trim().is_empty()))
                .unwrap_or(false);
            if !has_input {
                errors
                    .push("streaming pipelines require streaming.input_stream_id".to_string());
            }
            if external.is_some() {
                errors
                    .push("streaming pipelines must not carry external config".to_string());
            }
        }
        PipelineType::External => {
            let has_source = external
                .map(|e| !e.source_system.trim().is_empty())
                .unwrap_or(false);
            if !has_source {
                errors.push("external pipelines require external.source_system".to_string());
            }
            if streaming.is_some() {
                errors.push("external pipelines must not carry streaming config".to_string());
            }
        }
        PipelineType::Incremental => {
            if external.is_some() {
                errors
                    .push("incremental pipelines must not carry external config".to_string());
            }
            if let Some(cfg) = incremental {
                let raw = cfg.allowed_transaction_types.trim();
                if !raw.is_empty() {
                    for token in raw.split(',') {
                        let t = token.trim().to_ascii_uppercase();
                        if !VALID_TX_TYPES.contains(&t.as_str()) {
                            errors.push(format!(
                                "incremental.allowed_transaction_types: '{token}' not in APPEND,UPDATE,DELETE,SNAPSHOT"
                            ));
                        }
                    }
                }
            }
        }
        PipelineType::Batch | PipelineType::Faster => {
            if external.is_some() {
                errors.push(format!(
                    "{} pipelines must not carry external config",
                    pipeline_type.as_str().to_lowercase()
                ));
            }
            if streaming.is_some() {
                errors.push(format!(
                    "{} pipelines must not carry streaming config",
                    pipeline_type.as_str().to_lowercase()
                ));
            }
        }
    }

    errors
}
