package branching

import (
	"encoding/json"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

func ptr[T any](v T) *T { return &v }

func TestEvaluateConditionEq(t *testing.T) {
	t.Parallel()
	c := models.WorkflowBranchCondition{Field: "status", Operator: "eq", Value: json.RawMessage(`"ready"`)}
	if !EvaluateCondition(c, json.RawMessage(`{"status":"ready"}`)) {
		t.Fatal("eq match failed")
	}
	if EvaluateCondition(c, json.RawMessage(`{"status":"pending"}`)) {
		t.Fatal("eq mismatch should not match")
	}
}

func TestEvaluateConditionNumberCompare(t *testing.T) {
	t.Parallel()
	c := models.WorkflowBranchCondition{Field: "score", Operator: "gt", Value: json.RawMessage(`5`)}
	if !EvaluateCondition(c, json.RawMessage(`{"score":10}`)) {
		t.Fatal("10 > 5 should match")
	}
	if EvaluateCondition(c, json.RawMessage(`{"score":3}`)) {
		t.Fatal("3 > 5 should not match")
	}
}

func TestEvaluateConditionContains(t *testing.T) {
	t.Parallel()
	c := models.WorkflowBranchCondition{Field: "label", Operator: "contains", Value: json.RawMessage(`"prio"`)}
	if !EvaluateCondition(c, json.RawMessage(`{"label":"high-priority"}`)) {
		t.Fatal("contains should match")
	}
}

func TestEvaluateConditionNestedPath(t *testing.T) {
	t.Parallel()
	c := models.WorkflowBranchCondition{Field: "trigger.type", Operator: "eq", Value: json.RawMessage(`"webhook"`)}
	if !EvaluateCondition(c, json.RawMessage(`{"trigger":{"type":"webhook"}}`)) {
		t.Fatal("nested path should match")
	}
}

func TestEvaluateConditionMissingFieldFalse(t *testing.T) {
	t.Parallel()
	c := models.WorkflowBranchCondition{Field: "missing", Operator: "eq", Value: json.RawMessage(`"x"`)}
	if EvaluateCondition(c, json.RawMessage(`{}`)) {
		t.Fatal("missing field should never match")
	}
}

func TestResolveNextStepBranchWins(t *testing.T) {
	t.Parallel()
	step := models.WorkflowStep{
		ID:         "step-1",
		NextStepID: ptr("default-next"),
		Branches: []models.WorkflowBranch{
			{
				Condition:  models.WorkflowBranchCondition{Field: "status", Operator: "eq", Value: json.RawMessage(`"failed"`)},
				NextStepID: "rollback",
			},
		},
	}
	if got := ResolveNextStep(step, json.RawMessage(`{"status":"failed"}`)); got == nil || *got != "rollback" {
		t.Fatalf("expected rollback, got %v", got)
	}
	if got := ResolveNextStep(step, json.RawMessage(`{"status":"ok"}`)); got == nil || *got != "default-next" {
		t.Fatalf("expected default-next, got %v", got)
	}
}

func TestResolveNextStepNoBranchAndNoDefaultReturnsNil(t *testing.T) {
	t.Parallel()
	step := models.WorkflowStep{ID: "leaf"}
	if got := ResolveNextStep(step, json.RawMessage(`{}`)); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}
