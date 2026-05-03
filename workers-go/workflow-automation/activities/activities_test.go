package activities

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/internal/contract"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestExecuteOntologyActionCallsService(t *testing.T) {
	var seenBody map[string]any
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ontology/actions/action-123/execute" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get(contract.HeaderAuditCorrelation); got != "run-777" {
			t.Fatalf("audit header = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"content-type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"result":{"ok":true},"deleted":false}`)),
		}, nil
	})
	client := &HTTPOntologyActionsClient{
		BaseURL:     "http://ontology-actions-service:50106",
		BearerToken: "test-token",
		HTTPClient:  &http.Client{Transport: transport},
	}

	result, err := (&Activities{OntologyActions: client}).ExecuteOntologyAction(context.Background(), contract.AutomationRunInput{
		RunID: "run-777",
		TriggerPayload: map[string]any{
			"action_id":         "action-123",
			"target_object_id":  "object-456",
			"parameters":        map[string]any{"status": "approved"},
			"justification":     "policy automation",
			"unrelated_context": "kept out of request",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteOntologyAction returned error: %v", err)
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %v", result["status"])
	}
	if seenBody["target_object_id"] != "object-456" {
		t.Fatalf("target_object_id = %v", seenBody["target_object_id"])
	}
	params, ok := seenBody["parameters"].(map[string]any)
	if !ok || params["status"] != "approved" {
		t.Fatalf("parameters = %#v", seenBody["parameters"])
	}
	if seenBody["justification"] != "policy automation" {
		t.Fatalf("justification = %v", seenBody["justification"])
	}
}

func TestExecuteOntologyActionAcceptsNestedPayload(t *testing.T) {
	t.Parallel()

	req, err := ontologyActionRequestFromInput(contract.AutomationRunInput{
		RunID: "run-1",
		TriggerPayload: map[string]any{
			"ontology_action": map[string]any{
				"action_id": "nested-action",
				"parameters": map[string]any{
					"priority": "high",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ontologyActionRequestFromInput returned error: %v", err)
	}
	if req.ActionID != "nested-action" {
		t.Fatalf("ActionID = %q", req.ActionID)
	}
	if req.Parameters["priority"] != "high" {
		t.Fatalf("Parameters = %#v", req.Parameters)
	}
}

func TestExecuteOntologyActionRejectsMissingActionID(t *testing.T) {
	t.Parallel()

	_, err := (&Activities{}).ExecuteOntologyAction(context.Background(), contract.AutomationRunInput{
		RunID:          "run-1",
		TriggerPayload: map[string]any{"parameters": map[string]any{"x": 1}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "action_id is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestOntologyActionClientMapsHTTPClientErrors(t *testing.T) {
	t.Parallel()

	client := &HTTPOntologyActionsClient{
		BaseURL:     "http://ontology-actions-service:50106",
		BearerToken: "Bearer test-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"error":"bad action"}`)),
			}, nil
		})},
	}
	_, err := client.Execute(context.Background(), OntologyActionRequest{
		ActionID:           "action-1",
		AuditCorrelationID: "run-1",
		Parameters:         map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ontology action returned 400: bad action") {
		t.Fatalf("error = %v", err)
	}
}
