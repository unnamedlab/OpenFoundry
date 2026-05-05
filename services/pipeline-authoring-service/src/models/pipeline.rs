use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

use crate::domain::lifecycle::PipelineLifecycle;
use crate::domain::pipeline_type::{
    ExternalConfig, IncrementalConfig, PipelineType, StreamingConfig,
};

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct Pipeline {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub owner_id: Uuid,
    pub dag: serde_json::Value,
    pub status: String,
    pub schedule_config: Value,
    pub retry_policy: Value,
    pub next_run_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
    // FASE 1 — pipeline kind + release lifecycle. Stored as TEXT with
    // CHECK constraints on the DB side; typed accessors below.
    #[serde(default)]
    pub pipeline_type: String,
    #[serde(default)]
    pub lifecycle: String,
    #[serde(default)]
    pub external_config: Option<Value>,
    #[serde(default)]
    pub incremental_config: Option<Value>,
    #[serde(default)]
    pub streaming_config: Option<Value>,
    #[serde(default)]
    pub compute_profile_id: Option<String>,
    #[serde(default)]
    pub project_id: Option<Uuid>,
}

impl Pipeline {
    pub fn parsed_nodes(&self) -> Result<Vec<PipelineNode>, String> {
        serde_json::from_value(self.dag.clone()).map_err(|error| error.to_string())
    }

    pub fn schedule(&self) -> PipelineScheduleConfig {
        serde_json::from_value(self.schedule_config.clone()).unwrap_or_default()
    }

    pub fn parsed_retry_policy(&self) -> PipelineRetryPolicy {
        serde_json::from_value(self.retry_policy.clone()).unwrap_or_default()
    }

    /// Pipeline Builder "Build settings" surfaced by the UI.
    ///
    /// Stored under `dag.build_settings` so we don't need a fresh
    /// migration. Defaults to `AT_LEAST_ONCE` to match Foundry's
    /// documented streaming-pipeline default.
    pub fn build_settings(&self) -> PipelineBuildSettings {
        self.dag
            .get("build_settings")
            .cloned()
            .map(serde_json::from_value)
            .and_then(Result::ok)
            .unwrap_or_default()
    }

    /// Typed `pipeline_type`. Falls back to `BATCH` when the column is
    /// blank (rows pre-dating FASE 1) or carries an unknown literal —
    /// the DB CHECK guarantees the latter cannot happen for fresh rows,
    /// but we tolerate it defensively.
    pub fn pipeline_kind(&self) -> PipelineType {
        PipelineType::parse(&self.pipeline_type).unwrap_or_default()
    }

    /// Typed `lifecycle`. Defaults to `DRAFT` for the same reason.
    pub fn lifecycle_state(&self) -> PipelineLifecycle {
        PipelineLifecycle::parse(&self.lifecycle).unwrap_or_default()
    }

    pub fn external_config_typed(&self) -> Option<ExternalConfig> {
        self.external_config
            .as_ref()
            .and_then(|v| serde_json::from_value(v.clone()).ok())
    }

    pub fn incremental_config_typed(&self) -> Option<IncrementalConfig> {
        self.incremental_config
            .as_ref()
            .and_then(|v| serde_json::from_value(v.clone()).ok())
    }

    pub fn streaming_config_typed(&self) -> Option<StreamingConfig> {
        self.streaming_config
            .as_ref()
            .and_then(|v| serde_json::from_value(v.clone()).ok())
    }
}

/// Build-time settings configured from the Pipeline Builder. Currently
/// only carries the streaming consistency knob (Foundry docs §"Streaming
/// consistency guarantees"). When the pipeline produces a streaming
/// dataset, the effective consistency is the strongest of:
///   * this `streaming_consistency` setting (operator opt-in), and
///   * the `pipeline_consistency` set on the destination stream.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default)]
pub struct PipelineBuildSettings {
    /// One of `AT_LEAST_ONCE`, `EXACTLY_ONCE`. Matches the proto enum
    /// `openfoundry.streaming.streams.v1.StreamConsistency`.
    pub streaming_consistency: Option<String>,
}

impl PipelineBuildSettings {
    /// Resolve the streaming consistency value, falling back to the
    /// Foundry default of `AT_LEAST_ONCE` when unset.
    pub fn effective_streaming_consistency(&self) -> &str {
        self.streaming_consistency
            .as_deref()
            .unwrap_or("AT_LEAST_ONCE")
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default)]
pub struct PipelineScheduleConfig {
    pub enabled: bool,
    pub cron: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct PipelineRetryPolicy {
    pub max_attempts: u32,
    pub retry_on_failure: bool,
    pub allow_partial_reexecution: bool,
}

impl Default for PipelineRetryPolicy {
    fn default() -> Self {
        Self {
            max_attempts: 1,
            retry_on_failure: false,
            allow_partial_reexecution: true,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PipelineColumnMapping {
    pub source_dataset_id: Option<Uuid>,
    pub source_column: String,
    pub target_column: String,
}

/// A single node in the pipeline DAG.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct PipelineNode {
    pub id: String,
    pub label: String,
    pub transform_type: String, // "sql", "python", "passthrough"
    #[serde(default)]
    pub config: serde_json::Value,
    #[serde(default)]
    pub depends_on: Vec<String>,
    #[serde(default)]
    pub input_dataset_ids: Vec<Uuid>,
    #[serde(default)]
    pub output_dataset_id: Option<Uuid>,
    /// FASE 1 — when an input node is flagged incremental, the badge
    /// and the `replay_on_deploy` propagation cascade through every
    /// downstream node (Foundry "Incremental pipeline" doc).
    #[serde(default)]
    pub incremental_input: bool,
    /// `READY` | `PENDING` | `STALE` — last preview status reported by
    /// the live preview engine. Defaults to empty string when no preview
    /// has been requested yet.
    #[serde(default)]
    pub preview_status: String,
    /// `VALID` | `INVALID` | `PENDING` — type-safe per-node validation.
    #[serde(default)]
    pub validation_status: String,
    #[serde(default)]
    pub validation_errors: Vec<String>,
}

impl PipelineNode {
    pub fn column_mappings(&self) -> Vec<PipelineColumnMapping> {
        self.config
            .get("column_mappings")
            .cloned()
            .and_then(|value| serde_json::from_value(value).ok())
            .unwrap_or_default()
    }
}

#[derive(Debug, Deserialize)]
pub struct CreatePipelineRequest {
    pub name: String,
    pub description: Option<String>,
    pub status: Option<String>,
    #[serde(default)]
    pub nodes: Vec<PipelineNode>,
    pub schedule_config: Option<PipelineScheduleConfig>,
    pub retry_policy: Option<PipelineRetryPolicy>,
    /// P5 — Foundry "Open in Pipeline Builder" entry point. When set
    /// and `nodes` is empty, the handler synthesises a single
    /// passthrough node carrying the dataset RID as a config hint so
    /// the user lands on a pipeline already wired to read from it.
    #[serde(default)]
    pub seed_dataset_rid: Option<String>,
    /// P5 — explicit input list, mirroring Foundry's "Add a dataset
    /// input" payload. Each entry seeds a passthrough node when
    /// `nodes` is left empty.
    #[serde(default)]
    pub inputs: Vec<PipelineInputSeed>,
    /// FASE 1 — pipeline kind (BATCH | FASTER | INCREMENTAL | STREAMING | EXTERNAL).
    /// Defaults to BATCH, the only kind every Foundry user can author.
    #[serde(default)]
    pub pipeline_type: Option<String>,
    #[serde(default)]
    pub external: Option<ExternalConfig>,
    #[serde(default)]
    pub incremental: Option<IncrementalConfig>,
    #[serde(default)]
    pub streaming: Option<StreamingConfig>,
    #[serde(default)]
    pub compute_profile_id: Option<String>,
    #[serde(default)]
    pub project_id: Option<Uuid>,
}

/// Lightweight seed for the `inputs` array on `CreatePipelineRequest`.
/// Mirrors Pipeline Builder's "Add dataset input" panel — only the
/// RID is required for the synthesised passthrough node.
#[derive(Debug, Clone, Deserialize)]
pub struct PipelineInputSeed {
    pub dataset_rid: String,
    #[serde(default)]
    pub label: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdatePipelineRequest {
    pub name: Option<String>,
    pub description: Option<String>,
    pub status: Option<String>,
    pub nodes: Option<Vec<PipelineNode>>,
    pub schedule_config: Option<PipelineScheduleConfig>,
    pub retry_policy: Option<PipelineRetryPolicy>,
    #[serde(default)]
    pub pipeline_type: Option<String>,
    /// Lifecycle target. Validated against the FSM (illegal transitions
    /// rejected with 400). Absence means "leave unchanged".
    #[serde(default)]
    pub lifecycle: Option<String>,
    #[serde(default)]
    pub external: Option<ExternalConfig>,
    #[serde(default)]
    pub incremental: Option<IncrementalConfig>,
    #[serde(default)]
    pub streaming: Option<StreamingConfig>,
    #[serde(default)]
    pub compute_profile_id: Option<String>,
    #[serde(default)]
    pub project_id: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct ListPipelinesQuery {
    pub page: Option<i64>,
    pub per_page: Option<i64>,
    pub search: Option<String>,
    pub status: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ListPipelinesResponse {
    pub data: Vec<Pipeline>,
    pub total: i64,
    pub page: i64,
    pub per_page: i64,
}
