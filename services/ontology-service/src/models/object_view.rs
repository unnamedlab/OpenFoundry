use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

use crate::models::{
    action_type::ActionType,
    graph::GraphResponse,
    rule::{OntologyRuleRun, RuleMatchResponse},
};

#[derive(Debug, Serialize)]
pub struct ObjectViewResponse {
    pub object: Value,
    pub summary: Value,
    pub neighbors: Vec<Value>,
    pub graph: GraphResponse,
    pub applicable_actions: Vec<ActionType>,
    pub matching_rules: Vec<RuleMatchResponse>,
    pub recent_rule_runs: Vec<OntologyRuleRun>,
    pub timeline: Vec<Value>,
}

#[derive(Debug, Deserialize)]
pub struct ObjectSimulationRequest {
    pub action_id: Option<Uuid>,
    #[serde(default)]
    pub action_parameters: Value,
    #[serde(default)]
    pub properties_patch: Value,
    pub depth: Option<usize>,
}

#[derive(Debug, Serialize)]
pub struct ObjectSimulationImpactSummary {
    pub scope: String,
    pub action_kind: String,
    pub predicted_delete: bool,
    pub impacted_object_count: usize,
    pub impacted_type_count: usize,
    pub impacted_types: Vec<String>,
    pub direct_neighbors: usize,
    pub max_hops_reached: usize,
    pub boundary_crossings: usize,
    pub sensitive_objects: usize,
    pub sensitive_markings: Vec<String>,
    pub matching_rules: usize,
    pub changed_properties: Vec<String>,
}

#[derive(Debug, Serialize)]
pub struct ObjectSimulationResponse {
    pub before: Value,
    pub after: Option<Value>,
    pub deleted: bool,
    pub action_preview: Value,
    pub matching_rules: Vec<RuleMatchResponse>,
    pub graph: GraphResponse,
    pub impact_summary: ObjectSimulationImpactSummary,
    pub impacted_objects: Vec<Uuid>,
    pub timeline: Vec<Value>,
}

fn default_include_baseline() -> bool {
    true
}

fn default_goal_weight() -> f64 {
    1.0
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScenarioSimulationOperation {
    pub label: Option<String>,
    pub target_object_id: Option<Uuid>,
    pub action_id: Option<Uuid>,
    #[serde(default)]
    pub action_parameters: Value,
    #[serde(default)]
    pub properties_patch: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScenarioSimulationCandidate {
    pub name: String,
    pub description: Option<String>,
    #[serde(default)]
    pub operations: Vec<ScenarioSimulationOperation>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScenarioMetricSpec {
    pub name: String,
    pub metric: String,
    pub comparator: String,
    #[serde(default)]
    pub target: Value,
    #[serde(default)]
    pub config: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScenarioGoalSpec {
    pub name: String,
    pub metric: String,
    pub comparator: String,
    #[serde(default)]
    pub target: Value,
    #[serde(default)]
    pub config: Value,
    #[serde(default = "default_goal_weight")]
    pub weight: f64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ObjectScenarioSimulationRequest {
    #[serde(default)]
    pub scenarios: Vec<ScenarioSimulationCandidate>,
    #[serde(default)]
    pub constraints: Vec<ScenarioMetricSpec>,
    #[serde(default)]
    pub goals: Vec<ScenarioGoalSpec>,
    pub depth: Option<usize>,
    pub max_iterations: Option<usize>,
    #[serde(default = "default_include_baseline")]
    pub include_baseline: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioObjectChange {
    pub object_id: Uuid,
    pub object_type_id: Uuid,
    pub object_type_label: String,
    pub deleted: bool,
    pub changed_properties: Vec<String>,
    pub sources: Vec<String>,
    pub before: Value,
    pub after: Option<Value>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioRuleOutcome {
    pub object_id: Uuid,
    pub rule_id: Uuid,
    pub rule_name: String,
    pub rule_display_name: String,
    pub evaluation_mode: String,
    pub matched: bool,
    pub auto_applied: bool,
    pub trigger_payload: Value,
    pub effect_preview: Value,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioLinkPreview {
    pub source_object_id: Option<Uuid>,
    pub target_object_id: Option<Uuid>,
    pub link_type_id: Option<Uuid>,
    pub preview: Value,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioMetricEvaluation {
    pub name: String,
    pub metric: String,
    pub comparator: String,
    pub target: Value,
    pub observed: Value,
    pub passed: bool,
    pub score: Option<f64>,
    pub message: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioSummary {
    pub impacted_object_count: usize,
    pub changed_object_count: usize,
    pub deleted_object_count: usize,
    pub automatic_rule_matches: usize,
    pub automatic_rule_applications: usize,
    pub advisory_rule_matches: usize,
    pub schedule_count: usize,
    pub impacted_types: Vec<String>,
    pub changed_properties: Vec<String>,
    pub boundary_crossings: usize,
    pub sensitive_objects: usize,
    pub failed_constraints: usize,
    pub achieved_goals: usize,
    pub total_goals: usize,
    pub goal_score: f64,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioSummaryDelta {
    pub impacted_object_count: i64,
    pub changed_object_count: i64,
    pub deleted_object_count: i64,
    pub automatic_rule_matches: i64,
    pub automatic_rule_applications: i64,
    pub advisory_rule_matches: i64,
    pub schedule_count: i64,
    pub failed_constraints: i64,
    pub goal_score: f64,
}

#[derive(Debug, Clone, Serialize)]
pub struct ScenarioSimulationResult {
    pub scenario_id: String,
    pub name: String,
    pub description: Option<String>,
    pub graph: GraphResponse,
    pub object_changes: Vec<ScenarioObjectChange>,
    pub rule_outcomes: Vec<ScenarioRuleOutcome>,
    pub link_previews: Vec<ScenarioLinkPreview>,
    pub constraints: Vec<ScenarioMetricEvaluation>,
    pub goals: Vec<ScenarioMetricEvaluation>,
    pub summary: ScenarioSummary,
    pub delta_from_baseline: Option<ScenarioSummaryDelta>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ObjectScenarioSimulationResponse {
    pub root_object_id: Uuid,
    pub root_type_id: Uuid,
    pub compared_at: DateTime<Utc>,
    pub baseline: Option<ScenarioSimulationResult>,
    pub scenarios: Vec<ScenarioSimulationResult>,
}
