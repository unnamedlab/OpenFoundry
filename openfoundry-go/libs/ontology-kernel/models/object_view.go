package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ObjectViewResponse mirrors `struct ObjectViewResponse` in
// `libs/ontology-kernel/src/models/object_view.rs`.
type ObjectViewResponse struct {
	Object             json.RawMessage     `json:"object"`
	Summary            json.RawMessage     `json:"summary"`
	Neighbors          []json.RawMessage   `json:"neighbors"`
	Graph              GraphResponse       `json:"graph"`
	ApplicableActions  []ActionType        `json:"applicable_actions"`
	MatchingRules      []RuleMatchResponse `json:"matching_rules"`
	RecentRuleRuns     []OntologyRuleRun   `json:"recent_rule_runs"`
	Timeline           []json.RawMessage   `json:"timeline"`
}

// ObjectSimulationRequest mirrors `struct ObjectSimulationRequest`.
// `action_parameters` and `properties_patch` are `#[serde(default)]`
// `Value`, so they decode to `null` when missing.
type ObjectSimulationRequest struct {
	ActionID         *uuid.UUID      `json:"action_id,omitempty"`
	ActionParameters json.RawMessage `json:"action_parameters"`
	PropertiesPatch  json.RawMessage `json:"properties_patch"`
	Depth            *int            `json:"depth,omitempty"`
}

// ObjectSimulationImpactSummary mirrors
// `struct ObjectSimulationImpactSummary`.
type ObjectSimulationImpactSummary struct {
	Scope                string   `json:"scope"`
	ActionKind           string   `json:"action_kind"`
	PredictedDelete      bool     `json:"predicted_delete"`
	ImpactedObjectCount  int      `json:"impacted_object_count"`
	ImpactedTypeCount    int      `json:"impacted_type_count"`
	ImpactedTypes        []string `json:"impacted_types"`
	DirectNeighbors      int      `json:"direct_neighbors"`
	MaxHopsReached       int      `json:"max_hops_reached"`
	BoundaryCrossings    int      `json:"boundary_crossings"`
	SensitiveObjects     int      `json:"sensitive_objects"`
	SensitiveMarkings    []string `json:"sensitive_markings"`
	MatchingRules        int      `json:"matching_rules"`
	ChangedProperties    []string `json:"changed_properties"`
}

// ObjectSimulationResponse mirrors `struct ObjectSimulationResponse`.
type ObjectSimulationResponse struct {
	Before         json.RawMessage               `json:"before"`
	After          json.RawMessage               `json:"after"`
	Deleted        bool                          `json:"deleted"`
	ActionPreview  json.RawMessage               `json:"action_preview"`
	MatchingRules  []RuleMatchResponse           `json:"matching_rules"`
	Graph          GraphResponse                 `json:"graph"`
	ImpactSummary  ObjectSimulationImpactSummary `json:"impact_summary"`
	ImpactedObjects []uuid.UUID                  `json:"impacted_objects"`
	Timeline       []json.RawMessage             `json:"timeline"`
}

// ScenarioSimulationOperation mirrors `struct ScenarioSimulationOperation`.
type ScenarioSimulationOperation struct {
	Label            *string         `json:"label"`
	TargetObjectID   *uuid.UUID      `json:"target_object_id"`
	ActionID         *uuid.UUID      `json:"action_id"`
	ActionParameters json.RawMessage `json:"action_parameters"`
	PropertiesPatch  json.RawMessage `json:"properties_patch"`
}

// ScenarioSimulationCandidate mirrors `struct ScenarioSimulationCandidate`.
type ScenarioSimulationCandidate struct {
	Name        string                        `json:"name"`
	Description *string                       `json:"description"`
	Operations  []ScenarioSimulationOperation `json:"operations"`
}

// UnmarshalJSON applies `#[serde(default)]` to `operations`.
func (c *ScenarioSimulationCandidate) UnmarshalJSON(b []byte) error {
	type alias ScenarioSimulationCandidate
	if err := json.Unmarshal(b, (*alias)(c)); err != nil {
		return err
	}
	if c.Operations == nil {
		c.Operations = []ScenarioSimulationOperation{}
	}
	return nil
}

// ScenarioMetricSpec mirrors `struct ScenarioMetricSpec`.
type ScenarioMetricSpec struct {
	Name       string          `json:"name"`
	Metric     string          `json:"metric"`
	Comparator string          `json:"comparator"`
	Target     json.RawMessage `json:"target"`
	Config     json.RawMessage `json:"config"`
}

// ScenarioGoalSpec mirrors `struct ScenarioGoalSpec`. `weight`
// defaults to 1.0 (`default_goal_weight`).
type ScenarioGoalSpec struct {
	Name       string          `json:"name"`
	Metric     string          `json:"metric"`
	Comparator string          `json:"comparator"`
	Target     json.RawMessage `json:"target"`
	Config     json.RawMessage `json:"config"`
	Weight     float64         `json:"weight"`
}

// UnmarshalJSON applies `default_goal_weight() = 1.0` for missing key.
func (g *ScenarioGoalSpec) UnmarshalJSON(b []byte) error {
	type alias ScenarioGoalSpec
	a := alias{Weight: 1.0}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if _, ok := raw["weight"]; !ok {
		a.Weight = 1.0
	}
	*g = ScenarioGoalSpec(a)
	return nil
}

// ObjectScenarioSimulationRequest mirrors
// `struct ObjectScenarioSimulationRequest`. `include_baseline`
// defaults to `true` (`default_include_baseline`).
type ObjectScenarioSimulationRequest struct {
	Scenarios       []ScenarioSimulationCandidate `json:"scenarios"`
	Constraints     []ScenarioMetricSpec          `json:"constraints"`
	Goals           []ScenarioGoalSpec            `json:"goals"`
	Depth           *int                          `json:"depth,omitempty"`
	MaxIterations   *int                          `json:"max_iterations,omitempty"`
	IncludeBaseline bool                          `json:"include_baseline"`
}

// UnmarshalJSON applies serde(default) for each Vec field and
// `default_include_baseline() = true`.
func (r *ObjectScenarioSimulationRequest) UnmarshalJSON(b []byte) error {
	type alias ObjectScenarioSimulationRequest
	a := alias{IncludeBaseline: true}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if a.Scenarios == nil {
		a.Scenarios = []ScenarioSimulationCandidate{}
	}
	if a.Constraints == nil {
		a.Constraints = []ScenarioMetricSpec{}
	}
	if a.Goals == nil {
		a.Goals = []ScenarioGoalSpec{}
	}
	if _, ok := raw["include_baseline"]; !ok {
		a.IncludeBaseline = true
	}
	*r = ObjectScenarioSimulationRequest(a)
	return nil
}

// ScenarioObjectChange mirrors `struct ScenarioObjectChange`.
type ScenarioObjectChange struct {
	ObjectID          uuid.UUID       `json:"object_id"`
	ObjectTypeID      uuid.UUID       `json:"object_type_id"`
	ObjectTypeLabel   string          `json:"object_type_label"`
	Deleted           bool            `json:"deleted"`
	ChangedProperties []string        `json:"changed_properties"`
	Sources           []string        `json:"sources"`
	Before            json.RawMessage `json:"before"`
	After             json.RawMessage `json:"after"`
}

// ScenarioRuleOutcome mirrors `struct ScenarioRuleOutcome`.
type ScenarioRuleOutcome struct {
	ObjectID         uuid.UUID       `json:"object_id"`
	RuleID           uuid.UUID       `json:"rule_id"`
	RuleName         string          `json:"rule_name"`
	RuleDisplayName  string          `json:"rule_display_name"`
	EvaluationMode   string          `json:"evaluation_mode"`
	Matched          bool            `json:"matched"`
	AutoApplied      bool            `json:"auto_applied"`
	TriggerPayload   json.RawMessage `json:"trigger_payload"`
	EffectPreview    json.RawMessage `json:"effect_preview"`
}

// ScenarioLinkPreview mirrors `struct ScenarioLinkPreview`.
type ScenarioLinkPreview struct {
	SourceObjectID *uuid.UUID      `json:"source_object_id"`
	TargetObjectID *uuid.UUID      `json:"target_object_id"`
	LinkTypeID     *uuid.UUID      `json:"link_type_id"`
	Preview        json.RawMessage `json:"preview"`
}

// ScenarioMetricEvaluation mirrors `struct ScenarioMetricEvaluation`.
type ScenarioMetricEvaluation struct {
	Name       string          `json:"name"`
	Metric     string          `json:"metric"`
	Comparator string          `json:"comparator"`
	Target     json.RawMessage `json:"target"`
	Observed   json.RawMessage `json:"observed"`
	Passed     bool            `json:"passed"`
	Score      *float64        `json:"score"`
	Message    string          `json:"message"`
}

// ScenarioSummary mirrors `struct ScenarioSummary`.
type ScenarioSummary struct {
	ImpactedObjectCount        int      `json:"impacted_object_count"`
	ChangedObjectCount         int      `json:"changed_object_count"`
	DeletedObjectCount         int      `json:"deleted_object_count"`
	AutomaticRuleMatches       int      `json:"automatic_rule_matches"`
	AutomaticRuleApplications  int      `json:"automatic_rule_applications"`
	AdvisoryRuleMatches        int      `json:"advisory_rule_matches"`
	ScheduleCount              int      `json:"schedule_count"`
	ImpactedTypes              []string `json:"impacted_types"`
	ChangedProperties          []string `json:"changed_properties"`
	BoundaryCrossings          int      `json:"boundary_crossings"`
	SensitiveObjects           int      `json:"sensitive_objects"`
	FailedConstraints          int      `json:"failed_constraints"`
	AchievedGoals              int      `json:"achieved_goals"`
	TotalGoals                 int      `json:"total_goals"`
	GoalScore                  float64  `json:"goal_score"`
}

// ScenarioSummaryDelta mirrors `struct ScenarioSummaryDelta`.
type ScenarioSummaryDelta struct {
	ImpactedObjectCount       int64   `json:"impacted_object_count"`
	ChangedObjectCount        int64   `json:"changed_object_count"`
	DeletedObjectCount        int64   `json:"deleted_object_count"`
	AutomaticRuleMatches      int64   `json:"automatic_rule_matches"`
	AutomaticRuleApplications int64   `json:"automatic_rule_applications"`
	AdvisoryRuleMatches       int64   `json:"advisory_rule_matches"`
	ScheduleCount             int64   `json:"schedule_count"`
	FailedConstraints         int64   `json:"failed_constraints"`
	GoalScore                 float64 `json:"goal_score"`
}

// ScenarioSimulationResult mirrors `struct ScenarioSimulationResult`.
type ScenarioSimulationResult struct {
	ScenarioID         string                     `json:"scenario_id"`
	Name               string                     `json:"name"`
	Description        *string                    `json:"description"`
	Graph              GraphResponse              `json:"graph"`
	ObjectChanges      []ScenarioObjectChange     `json:"object_changes"`
	RuleOutcomes       []ScenarioRuleOutcome      `json:"rule_outcomes"`
	LinkPreviews       []ScenarioLinkPreview      `json:"link_previews"`
	Constraints        []ScenarioMetricEvaluation `json:"constraints"`
	Goals              []ScenarioMetricEvaluation `json:"goals"`
	Summary            ScenarioSummary            `json:"summary"`
	DeltaFromBaseline  *ScenarioSummaryDelta      `json:"delta_from_baseline"`
}

// ObjectScenarioSimulationResponse mirrors
// `struct ObjectScenarioSimulationResponse`.
type ObjectScenarioSimulationResponse struct {
	RootObjectID uuid.UUID                  `json:"root_object_id"`
	RootTypeID   uuid.UUID                  `json:"root_type_id"`
	ComparedAt   time.Time                  `json:"compared_at"`
	Baseline     *ScenarioSimulationResult  `json:"baseline"`
	Scenarios    []ScenarioSimulationResult `json:"scenarios"`
}
