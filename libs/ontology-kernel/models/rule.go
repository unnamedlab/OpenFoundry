package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RuleTriggerSpec mirrors `libs/ontology-kernel/src/models/rule.rs`
// `struct RuleTriggerSpec`. Every field carries `#[serde(default)]`,
// so missing JSON keys decode to empty maps / slices.
type RuleTriggerSpec struct {
	Equals             map[string]json.RawMessage `json:"equals"`
	NumericGte         map[string]float64         `json:"numeric_gte"`
	NumericLte         map[string]float64         `json:"numeric_lte"`
	Exists             []string                   `json:"exists"`
	ChangedProperties  []string                   `json:"changed_properties"`
	Markings           []string                   `json:"markings"`
}

// UnmarshalJSON applies the Rust serde(default) defaults so absent
// keys decode to `{}` / `[]` rather than nil.
func (r *RuleTriggerSpec) UnmarshalJSON(b []byte) error {
	type alias RuleTriggerSpec
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	if r.Equals == nil {
		r.Equals = map[string]json.RawMessage{}
	}
	if r.NumericGte == nil {
		r.NumericGte = map[string]float64{}
	}
	if r.NumericLte == nil {
		r.NumericLte = map[string]float64{}
	}
	if r.Exists == nil {
		r.Exists = []string{}
	}
	if r.ChangedProperties == nil {
		r.ChangedProperties = []string{}
	}
	if r.Markings == nil {
		r.Markings = []string{}
	}
	return nil
}

// MarshalJSON forces `[]` / `{}` for nil zero-values to match serde
// for `Vec<_>` / `HashMap<_, _>`.
func (r RuleTriggerSpec) MarshalJSON() ([]byte, error) {
	type alias RuleTriggerSpec
	cp := alias(r)
	if cp.Equals == nil {
		cp.Equals = map[string]json.RawMessage{}
	}
	if cp.NumericGte == nil {
		cp.NumericGte = map[string]float64{}
	}
	if cp.NumericLte == nil {
		cp.NumericLte = map[string]float64{}
	}
	if cp.Exists == nil {
		cp.Exists = []string{}
	}
	if cp.ChangedProperties == nil {
		cp.ChangedProperties = []string{}
	}
	if cp.Markings == nil {
		cp.Markings = []string{}
	}
	return json.Marshal(cp)
}

// RuleScheduleSpec mirrors `struct RuleScheduleSpec`.
type RuleScheduleSpec struct {
	PropertyName             string   `json:"property_name"`
	OffsetHours              int64    `json:"offset_hours"`
	PriorityScore            *int32   `json:"priority_score"`
	EstimatedDurationMinutes *int32   `json:"estimated_duration_minutes"`
	RequiredCapability       *string  `json:"required_capability"`
	ConstraintTags           []string `json:"constraint_tags"`
	HardDeadlineHours        *int64   `json:"hard_deadline_hours"`
}

// RuleAlertSpec mirrors `struct RuleAlertSpec`.
type RuleAlertSpec struct {
	Severity string  `json:"severity"`
	Title    string  `json:"title"`
	Message  *string `json:"message"`
}

// RuleEffectSpec mirrors `struct RuleEffectSpec`.
type RuleEffectSpec struct {
	ObjectPatch json.RawMessage   `json:"object_patch"`
	Schedule    *RuleScheduleSpec `json:"schedule"`
	Alert       *RuleAlertSpec    `json:"alert"`
}

// RuleEvaluationMode mirrors `enum RuleEvaluationMode` —
// `#[serde(rename_all = "snake_case")]` and matching `sqlx::Type`.
type RuleEvaluationMode string

const (
	RuleEvaluationModeAdvisory  RuleEvaluationMode = "advisory"
	RuleEvaluationModeAutomatic RuleEvaluationMode = "automatic"
)

// String mirrors `impl Display for RuleEvaluationMode`.
func (m RuleEvaluationMode) String() string { return string(m) }

// OntologyRuleRow mirrors `struct OntologyRuleRow`. JSONB columns
// retained as raw bytes.
type OntologyRuleRow struct {
	ID             uuid.UUID       `db:"id"`
	Name           string          `db:"name"`
	DisplayName    string          `db:"display_name"`
	Description    string          `db:"description"`
	ObjectTypeID   uuid.UUID       `db:"object_type_id"`
	EvaluationMode string          `db:"evaluation_mode"`
	TriggerSpec    json.RawMessage `db:"trigger_spec"`
	EffectSpec     json.RawMessage `db:"effect_spec"`
	OwnerID        uuid.UUID       `db:"owner_id"`
	CreatedAt      time.Time       `db:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

// OntologyRule mirrors `struct OntologyRule`.
type OntologyRule struct {
	ID             uuid.UUID          `json:"id"`
	Name           string             `json:"name"`
	DisplayName    string             `json:"display_name"`
	Description    string             `json:"description"`
	ObjectTypeID   uuid.UUID          `json:"object_type_id"`
	EvaluationMode RuleEvaluationMode `json:"evaluation_mode"`
	TriggerSpec    RuleTriggerSpec    `json:"trigger_spec"`
	EffectSpec     RuleEffectSpec     `json:"effect_spec"`
	OwnerID        uuid.UUID          `json:"owner_id"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// IntoRule mirrors `TryFrom<OntologyRuleRow> for OntologyRule`. Per
// Rust, evaluation_mode falls back to `Advisory` on parse failure;
// trigger_spec / effect_spec fall back to default on failure
// (`unwrap_or_default`).
func (row OntologyRuleRow) IntoRule() (OntologyRule, error) {
	mode := RuleEvaluationModeAdvisory
	switch row.EvaluationMode {
	case "advisory":
		mode = RuleEvaluationModeAdvisory
	case "automatic":
		mode = RuleEvaluationModeAutomatic
	}
	var trigger RuleTriggerSpec
	if len(row.TriggerSpec) > 0 {
		_ = json.Unmarshal(row.TriggerSpec, &trigger)
	}
	if trigger.Equals == nil {
		// Apply defaults on the fallback path too.
		trigger = RuleTriggerSpec{
			Equals: map[string]json.RawMessage{}, NumericGte: map[string]float64{},
			NumericLte: map[string]float64{}, Exists: []string{},
			ChangedProperties: []string{}, Markings: []string{},
		}
	}
	var effect RuleEffectSpec
	if len(row.EffectSpec) > 0 {
		_ = json.Unmarshal(row.EffectSpec, &effect)
	}
	return OntologyRule{
		ID:             row.ID,
		Name:           row.Name,
		DisplayName:    row.DisplayName,
		Description:    row.Description,
		ObjectTypeID:   row.ObjectTypeID,
		EvaluationMode: mode,
		TriggerSpec:    trigger,
		EffectSpec:     effect,
		OwnerID:        row.OwnerID,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

// CreateRuleRequest mirrors `struct CreateRuleRequest`.
type CreateRuleRequest struct {
	Name           string              `json:"name"`
	DisplayName    *string             `json:"display_name,omitempty"`
	Description    *string             `json:"description,omitempty"`
	ObjectTypeID   uuid.UUID           `json:"object_type_id"`
	EvaluationMode *RuleEvaluationMode `json:"evaluation_mode,omitempty"`
	TriggerSpec    *RuleTriggerSpec    `json:"trigger_spec,omitempty"`
	EffectSpec     *RuleEffectSpec     `json:"effect_spec,omitempty"`
}

// UpdateRuleRequest mirrors `struct UpdateRuleRequest`.
type UpdateRuleRequest struct {
	DisplayName    *string             `json:"display_name,omitempty"`
	Description    *string             `json:"description,omitempty"`
	EvaluationMode *RuleEvaluationMode `json:"evaluation_mode,omitempty"`
	TriggerSpec    *RuleTriggerSpec    `json:"trigger_spec,omitempty"`
	EffectSpec     *RuleEffectSpec     `json:"effect_spec,omitempty"`
}

// ListRulesQuery mirrors `struct ListRulesQuery`.
type ListRulesQuery struct {
	ObjectTypeID *uuid.UUID `json:"object_type_id,omitempty"`
	Page         *int64     `json:"page,omitempty"`
	PerPage      *int64     `json:"per_page,omitempty"`
	Search       *string    `json:"search,omitempty"`
}

// ListRulesResponse mirrors `struct ListRulesResponse`.
type ListRulesResponse struct {
	Data    []OntologyRule `json:"data"`
	Total   int64          `json:"total"`
	Page    int64          `json:"page"`
	PerPage int64          `json:"per_page"`
}

// RuleEvaluateRequest mirrors `struct RuleEvaluateRequest`.
// `properties_patch` is `#[serde(default)]` — null when missing.
type RuleEvaluateRequest struct {
	ObjectID        uuid.UUID       `json:"object_id"`
	PropertiesPatch json.RawMessage `json:"properties_patch"`
}

// RuleMatchResponse mirrors `struct RuleMatchResponse`.
type RuleMatchResponse struct {
	RuleID         uuid.UUID       `json:"rule_id"`
	Matched        bool            `json:"matched"`
	TriggerPayload json.RawMessage `json:"trigger_payload"`
	EffectPreview  json.RawMessage `json:"effect_preview"`
}

// RuleEvaluateResponse mirrors `struct RuleEvaluateResponse`.
type RuleEvaluateResponse struct {
	Rule           OntologyRule    `json:"rule"`
	Matched        bool            `json:"matched"`
	TriggerPayload json.RawMessage `json:"trigger_payload"`
	EffectPreview  json.RawMessage `json:"effect_preview"`
	Object         json.RawMessage `json:"object"`
}

// OntologyRuleRun mirrors `struct OntologyRuleRun`.
type OntologyRuleRun struct {
	ID             uuid.UUID       `json:"id"             db:"id"`
	RuleID         uuid.UUID       `json:"rule_id"        db:"rule_id"`
	ObjectID       uuid.UUID       `json:"object_id"      db:"object_id"`
	Matched        bool            `json:"matched"        db:"matched"`
	Simulated      bool            `json:"simulated"      db:"simulated"`
	TriggerPayload json.RawMessage `json:"trigger_payload" db:"trigger_payload"`
	EffectPreview  json.RawMessage `json:"effect_preview"  db:"effect_preview"`
	CreatedBy      uuid.UUID       `json:"created_by"     db:"created_by"`
	CreatedAt      time.Time       `json:"created_at"     db:"created_at"`
}

// MachineryInsight mirrors `struct MachineryInsight`.
type MachineryInsight struct {
	RuleID                uuid.UUID          `json:"rule_id"`
	Name                  string             `json:"name"`
	DisplayName           string             `json:"display_name"`
	EvaluationMode        RuleEvaluationMode `json:"evaluation_mode"`
	MatchedRuns           int                `json:"matched_runs"`
	TotalRuns             int                `json:"total_runs"`
	PendingSchedules      int                `json:"pending_schedules"`
	OverdueSchedules      int                `json:"overdue_schedules"`
	AvgScheduleLeadHours  *float64           `json:"avg_schedule_lead_hours"`
	DynamicPressure       string             `json:"dynamic_pressure"`
	LastMatchedAt         *time.Time         `json:"last_matched_at"`
	LastObjectID          *uuid.UUID         `json:"last_object_id"`
}

// MachineryInsightsResponse mirrors `struct MachineryInsightsResponse`.
type MachineryInsightsResponse struct {
	ObjectTypeID *uuid.UUID         `json:"object_type_id"`
	Data         []MachineryInsight `json:"data"`
}

// MachineryQueueItem mirrors `struct MachineryQueueItem`.
type MachineryQueueItem struct {
	ID                       uuid.UUID       `json:"id"                          db:"id"`
	RuleID                   uuid.UUID       `json:"rule_id"                     db:"rule_id"`
	RuleRunID                uuid.UUID       `json:"rule_run_id"                 db:"rule_run_id"`
	ObjectID                 uuid.UUID       `json:"object_id"                   db:"object_id"`
	RuleName                 string          `json:"rule_name"                   db:"rule_name"`
	RuleDisplayName          string          `json:"rule_display_name"           db:"rule_display_name"`
	ObjectTypeID             uuid.UUID       `json:"object_type_id"              db:"object_type_id"`
	Status                   string          `json:"status"                      db:"status"`
	ScheduledFor             time.Time       `json:"scheduled_for"               db:"scheduled_for"`
	PriorityScore            int32           `json:"priority_score"              db:"priority_score"`
	EstimatedDurationMinutes int32           `json:"estimated_duration_minutes"  db:"estimated_duration_minutes"`
	RequiredCapability       *string         `json:"required_capability"         db:"required_capability"`
	ConstraintSnapshot       json.RawMessage `json:"constraint_snapshot"         db:"constraint_snapshot"`
	CreatedBy                uuid.UUID       `json:"created_by"                  db:"created_by"`
	CreatedAt                time.Time       `json:"created_at"                  db:"created_at"`
	UpdatedAt                time.Time       `json:"updated_at"                  db:"updated_at"`
	StartedAt                *time.Time      `json:"started_at"                  db:"started_at"`
	CompletedAt              *time.Time      `json:"completed_at"                db:"completed_at"`
}

// MachineryCapabilityLoad mirrors `struct MachineryCapabilityLoad`.
type MachineryCapabilityLoad struct {
	Capability             string `json:"capability"`
	PendingCount           int    `json:"pending_count"`
	TotalEstimatedMinutes  int    `json:"total_estimated_minutes"`
}

// MachineryQueueRecommendation mirrors `struct MachineryQueueRecommendation`.
type MachineryQueueRecommendation struct {
	GeneratedAt           time.Time                 `json:"generated_at"`
	Strategy              string                    `json:"strategy"`
	QueueDepth            int                       `json:"queue_depth"`
	OverdueCount          int                       `json:"overdue_count"`
	TotalEstimatedMinutes int                       `json:"total_estimated_minutes"`
	NextDueAt             *time.Time                `json:"next_due_at"`
	RecommendedOrder      []uuid.UUID               `json:"recommended_order"`
	CapabilityLoad        []MachineryCapabilityLoad `json:"capability_load"`
}

// MachineryQueueResponse mirrors `struct MachineryQueueResponse`.
type MachineryQueueResponse struct {
	ObjectTypeID   *uuid.UUID                   `json:"object_type_id"`
	Data           []MachineryQueueItem         `json:"data"`
	Recommendation MachineryQueueRecommendation `json:"recommendation"`
}

// UpdateMachineryQueueItemRequest mirrors `struct UpdateMachineryQueueItemRequest`.
type UpdateMachineryQueueItemRequest struct {
	Status string `json:"status"`
}
