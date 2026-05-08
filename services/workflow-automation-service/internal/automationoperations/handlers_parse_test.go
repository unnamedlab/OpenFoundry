package automationoperations

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParsePayloadRequiresTaskType(t *testing.T) {
	t.Parallel()
	_, err := parsePayload(json.RawMessage(`{"tenant_id":"acme"}`))
	if err == nil || !strings.Contains(err.Error(), "task_type") {
		t.Fatalf("expected task_type required error, got %v", err)
	}
}

func TestParsePayloadDerivesSagaIDWhenAbsent(t *testing.T) {
	t.Parallel()
	req, err := parsePayload(json.RawMessage(`{
		"task_type": "retention.sweep",
		"tenant_id": "acme"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Saga != "retention.sweep" {
		t.Fatalf("saga %s", req.Saga)
	}
	if req.TenantID != "acme" {
		t.Fatalf("tenant_id %s", req.TenantID)
	}
	if req.TriggeredBy != "system" {
		t.Fatalf("triggered_by %s", req.TriggeredBy)
	}
	if string(req.Input) != "null" {
		t.Fatalf("input %s", string(req.Input))
	}

	// Same correlation_id should derive the same saga_id.
	correlationStr := req.CorrelationID.String()
	req2, err := parsePayload(json.RawMessage(`{
		"task_type": "retention.sweep",
		"tenant_id": "acme",
		"audit_correlation_id": "` + correlationStr + `"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.SagaID != req2.SagaID {
		t.Fatalf("expected matching saga_id: %s vs %s", req.SagaID, req2.SagaID)
	}
}

func TestParsePayloadHonoursExplicitTaskID(t *testing.T) {
	t.Parallel()
	taskID := uuid.New()
	body, _ := json.Marshal(map[string]any{
		"task_id":   taskID.String(),
		"task_type": "cleanup.workspace",
		"tenant_id": "acme",
		"input":     map[string]any{"workspace_id": uuid.Nil.String()},
	})
	req, err := parsePayload(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.SagaID != taskID {
		t.Fatalf("saga_id %s want %s", req.SagaID, taskID)
	}
	if req.Saga != "cleanup.workspace" {
		t.Fatalf("saga %s", req.Saga)
	}
}

func TestParsePayloadFallsBackFromInputToPayloadAlias(t *testing.T) {
	t.Parallel()
	req, err := parsePayload(json.RawMessage(`{
		"task_type": "retention.sweep",
		"payload": {"older_than_days": 30}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var holder map[string]any
	if err := json.Unmarshal(req.Input, &holder); err != nil {
		t.Fatal(err)
	}
	if holder["older_than_days"].(float64) != 30 {
		t.Fatalf("older_than_days %v", holder["older_than_days"])
	}
}

func TestParsePayloadRejectsInvalidCorrelationID(t *testing.T) {
	t.Parallel()
	_, err := parsePayload(json.RawMessage(`{
		"task_type": "retention.sweep",
		"audit_correlation_id": "not-a-uuid"
	}`))
	if err == nil || !strings.Contains(err.Error(), "audit_correlation_id") {
		t.Fatalf("expected invalid correlation_id error, got %v", err)
	}
}
