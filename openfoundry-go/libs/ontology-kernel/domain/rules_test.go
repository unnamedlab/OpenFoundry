package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// sampleObject mirrors the Rust unit test's `sample_object`.
func sampleObject() *ObjectInstance {
	props, _ := json.Marshal(map[string]any{
		"status":     "pending",
		"risk_score": 0.92,
	})
	now := time.Now().UTC()
	return &ObjectInstance{
		ID:           uuid.Nil,
		ObjectTypeID: uuid.Nil,
		Properties:   props,
		CreatedBy:    uuid.Nil,
		Marking:      "confidential",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func marshalJSONForTest(t *testing.T, v any) json.RawMessage {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

// Mirrors `matches_rule_with_numeric_and_equals_conditions` in
// `libs/ontology-kernel/src/domain/rules.rs::tests`.
func TestEvaluateRuleAgainstObject_MatchesNumericAndEquals(t *testing.T) {
	t.Parallel()
	rule := &models.OntologyRule{
		ID:             uuid.New(),
		Name:           "high_risk_case",
		DisplayName:    "High risk case",
		ObjectTypeID:   uuid.Nil,
		EvaluationMode: models.RuleEvaluationModeAdvisory,
		TriggerSpec: models.RuleTriggerSpec{
			Equals:     map[string]json.RawMessage{"status": marshalJSONForTest(t, "pending")},
			NumericGte: map[string]float64{"risk_score": 0.8},
			NumericLte: map[string]float64{},
			Markings:   []string{"confidential"},
		},
		EffectSpec: models.RuleEffectSpec{
			ObjectPatch: marshalJSONForTest(t, map[string]any{"priority": "high"}),
		},
	}

	result := EvaluateRuleAgainstObject(rule, sampleObject(), nil)
	if !result.Matched {
		t.Fatalf("expected matched=true, got %+v", result)
	}
	var preview map[string]any
	if err := json.Unmarshal(result.EffectPreview, &preview); err != nil {
		t.Fatalf("preview not JSON: %v", err)
	}
	patch, ok := preview["object_patch"].(map[string]any)
	if !ok {
		t.Fatalf("object_patch missing or wrong shape: %v", preview["object_patch"])
	}
	priorityRaw, ok := patch["priority"]
	if !ok {
		t.Fatalf("priority missing")
	}
	// JSON round-trip turns json.RawMessage into a string.
	switch v := priorityRaw.(type) {
	case string:
		if v != "high" {
			t.Fatalf("priority drift: %q", v)
		}
	default:
		// json.RawMessage marshals as opaque bytes inside the map.
		got, _ := json.Marshal(v)
		if !strings.Contains(string(got), "high") {
			t.Fatalf("priority drift: %s", got)
		}
	}
}

func TestEvaluateRuleAgainstObject_MissingNumericPropertyFails(t *testing.T) {
	t.Parallel()
	rule := &models.OntologyRule{
		ID:           uuid.New(),
		ObjectTypeID: uuid.Nil,
		TriggerSpec: models.RuleTriggerSpec{
			NumericGte: map[string]float64{"missing_field": 1.0},
		},
		EffectSpec: models.RuleEffectSpec{Alert: &models.RuleAlertSpec{Severity: "low", Title: "x"}},
	}
	result := EvaluateRuleAgainstObject(rule, sampleObject(), nil)
	if result.Matched {
		t.Fatal("expected matched=false when numeric property is missing")
	}
}

func TestEvaluateRuleAgainstObject_MarkingMismatchFails(t *testing.T) {
	t.Parallel()
	rule := &models.OntologyRule{
		ID:           uuid.New(),
		ObjectTypeID: uuid.Nil,
		TriggerSpec:  models.RuleTriggerSpec{Markings: []string{"public"}},
		EffectSpec:   models.RuleEffectSpec{Alert: &models.RuleAlertSpec{Severity: "low", Title: "x"}},
	}
	result := EvaluateRuleAgainstObject(rule, sampleObject(), nil)
	if result.Matched {
		t.Fatal("expected matched=false: object marking is confidential, rule wants public only")
	}
}

func TestEvaluateRuleAgainstObject_ChangedPropertyTrigger(t *testing.T) {
	t.Parallel()
	rule := &models.OntologyRule{
		ID:           uuid.New(),
		ObjectTypeID: uuid.Nil,
		TriggerSpec: models.RuleTriggerSpec{
			ChangedProperties: []string{"status"},
		},
		EffectSpec: models.RuleEffectSpec{Alert: &models.RuleAlertSpec{Severity: "low", Title: "x"}},
	}
	// Patch flips `status` → triggers.
	patch := map[string]json.RawMessage{"status": marshalJSONForTest(t, "approved")}
	result := EvaluateRuleAgainstObject(rule, sampleObject(), patch)
	if !result.Matched {
		t.Fatalf("expected matched=true on changed property: %+v", result)
	}
	// Empty patch → not triggered.
	result = EvaluateRuleAgainstObject(rule, sampleObject(), nil)
	if result.Matched {
		t.Fatal("expected matched=false when no changed properties")
	}
}

func TestDeriveChangedProperties_DetectsAddRemoveModify(t *testing.T) {
	t.Parallel()
	props, _ := json.Marshal(map[string]any{"a": 1, "b": "x"})
	before := &ObjectInstance{Properties: props}
	after := map[string]json.RawMessage{
		"a": marshalJSONForTest(t, 2),    // modified
		"c": marshalJSONForTest(t, true), // added
		// "b" removed
	}
	got := DeriveChangedProperties(before, after)
	want := map[string]bool{"a": true, "b": true, "c": true}
	if len(got) != len(want) {
		t.Fatalf("expected %d changes, got %v", len(want), got)
	}
	for _, k := range got {
		if !want[k] {
			t.Errorf("unexpected change key %q", k)
		}
	}
}

func TestDynamicPressureFromQueueRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		queue, overdue int
		want           string
	}{
		{0, 0, "low"},
		{2, 0, "low"},
		{3, 0, "medium"},
		{7, 0, "medium"},
		{8, 0, "high"},
		{0, 1, "high"},
	}
	for _, tc := range cases {
		if got := dynamicPressureFromQueue(tc.queue, tc.overdue); got != tc.want {
			t.Errorf("dynamicPressureFromQueue(%d,%d) = %s, want %s",
				tc.queue, tc.overdue, got, tc.want)
		}
	}
}

func TestApplyRuleEffectReturnsErrApplyEffectNotWired(t *testing.T) {
	t.Parallel()
	_, err := ApplyRuleEffect(nil, nil, nil, sampleObject(), json.RawMessage("null"))
	if err == nil {
		t.Fatal("expected ErrApplyEffectNotWired sentinel")
	}
	if err != ErrApplyEffectNotWired { //nolint:errorlint
		t.Fatalf("expected ErrApplyEffectNotWired, got %v", err)
	}
}

func TestBuildRuleEffectPreviewIncludesObjectPatchAndSchedule(t *testing.T) {
	t.Parallel()
	priority := int32(80)
	effect := models.RuleEffectSpec{
		ObjectPatch: marshalJSONForTest(t, map[string]any{"priority": "high"}),
		Schedule: &models.RuleScheduleSpec{
			PropertyName:  "deadline_at",
			OffsetHours:   24,
			PriorityScore: &priority,
		},
	}
	out := BuildRuleEffectPreview(effect, sampleObject())

	var preview map[string]any
	if err := json.Unmarshal(out, &preview); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := preview["object_patch"].(map[string]any); !ok {
		t.Fatalf("object_patch must be a JSON object: %v", preview["object_patch"])
	}
	if _, ok := preview["schedule"].(map[string]any); !ok {
		t.Fatalf("schedule preview missing: %v", preview["schedule"])
	}
}
