// Package branching ports the pure-logic step-routing helpers from
// `services/workflow-automation-service/src/domain/branching.rs`.
//
// Used by future per-step execution paths to choose the next step
// based on the current run context. Today the FASE 5 condition
// consumer drives single-step executions, so this is unused at
// runtime — but it ships verbatim so multi-step workflows can wire
// in without re-porting.
package branching

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

// ResolveNextStep ports `domain::branching::resolve_next_step`.
//
// First branch with a satisfied condition wins. Falls through to
// `step.next_step_id` when no branch matches.
func ResolveNextStep(step models.WorkflowStep, context json.RawMessage) *string {
	for i := range step.Branches {
		if EvaluateCondition(step.Branches[i].Condition, context) {
			next := step.Branches[i].NextStepID
			return &next
		}
	}
	if step.NextStepID == nil {
		return nil
	}
	out := *step.NextStepID
	return &out
}

// EvaluateCondition ports `domain::branching::evaluate_condition`.
func EvaluateCondition(condition models.WorkflowBranchCondition, context json.RawMessage) bool {
	current, ok := valueForPath(context, condition.Field)
	if !ok {
		return false
	}
	switch condition.Operator {
	case "eq":
		return rawEqual(current, condition.Value)
	case "ne":
		return !rawEqual(current, condition.Value)
	case "contains":
		left, lok := stringValue(current)
		right, rok := stringValue(condition.Value)
		return lok && rok && strings.Contains(left, right)
	case "gt":
		return numberValue(current) > numberValue(condition.Value)
	case "gte":
		return numberValue(current) >= numberValue(condition.Value)
	case "lt":
		return numberValue(current) < numberValue(condition.Value)
	case "lte":
		return numberValue(current) <= numberValue(condition.Value)
	default:
		return false
	}
}

func valueForPath(context json.RawMessage, path string) (json.RawMessage, bool) {
	current := context
	for _, segment := range strings.Split(path, ".") {
		var holder map[string]json.RawMessage
		if err := json.Unmarshal(current, &holder); err != nil {
			return nil, false
		}
		next, ok := holder[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func rawEqual(left, right json.RawMessage) bool {
	// Round-trip both sides through json.Unmarshal/Marshal so two
	// equivalent JSON encodings (different whitespace, key order)
	// compare equal — same semantics as serde_json::Value PartialEq.
	var lv, rv any
	if err := json.Unmarshal(left, &lv); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rv); err != nil {
		return false
	}
	lb, err := json.Marshal(lv)
	if err != nil {
		return false
	}
	rb, err := json.Marshal(rv)
	if err != nil {
		return false
	}
	return bytes.Equal(lb, rb)
}

func stringValue(v json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", false
	}
	return s, true
}

func numberValue(v json.RawMessage) float64 {
	var n float64
	if err := json.Unmarshal(v, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		if parsed, err := strconv.ParseFloat(s, 64); err == nil {
			return parsed
		}
	}
	return 0
}
