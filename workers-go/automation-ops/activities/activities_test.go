package activities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/open-foundry/open-foundry/workers-go/automation-ops/internal/contract"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestExecuteTaskRecordsAutomationRun(t *testing.T) {
	t.Parallel()

	taskID := "018f8f8f-8f8f-7000-8000-000000000001"
	var seenBody map[string]any
	client := &HTTPAutomationOpsClient{
		BaseURL:     "http://automation-operations-service:50116",
		BearerToken: "Bearer ops-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if r.URL.Path != "/api/v1/automations/"+taskID+"/runs" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			if got := r.Header.Get("authorization"); got != "Bearer ops-token" {
				t.Fatalf("authorization = %q", got)
			}
			if got := r.Header.Get(contract.HeaderAuditCorrelation); got != taskID {
				t.Fatalf("audit header = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(`{"id":"run-1","parent_id":"018f8f8f-8f8f-7000-8000-000000000001"}`)),
			}, nil
		})},
	}

	result, err := NewWithClient(client).ExecuteTask(context.Background(), contract.AutomationOpsInput{
		TaskID:   taskID,
		TenantID: "tenant-a",
		TaskType: "retention_sweep",
		Payload:  map[string]any{"scope": "datasets"},
	})
	if err != nil {
		t.Fatalf("ExecuteTask returned error: %v", err)
	}
	if result.Status != "completed" || result.RunID != "run-1" {
		t.Fatalf("result = %#v", result)
	}
	payload, ok := seenBody["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v", seenBody["payload"])
	}
	if payload["task_type"] != "retention_sweep" {
		t.Fatalf("task_type = %v", payload["task_type"])
	}
	input, ok := payload["input"].(map[string]any)
	if !ok || input["scope"] != "datasets" {
		t.Fatalf("input = %#v", payload["input"])
	}
}

func TestExecuteTaskRejectsMissingTaskType(t *testing.T) {
	t.Parallel()

	_, err := NewWithClient(nil).ExecuteTask(context.Background(), contract.AutomationOpsInput{
		TaskID: "task-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "task_type is required") {
		t.Fatalf("error = %v", err)
	}
}
