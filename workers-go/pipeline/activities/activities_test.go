package activities

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestBuildPipelineCompilesInlineGraph(t *testing.T) {
	t.Parallel()

	var seenBody map[string]any
	client := &HTTPPipelineServicesClient{
		AuthoringURL: "http://pipeline-authoring-service:50052",
		BearerToken:  "pipeline-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if r.URL.Path != "/api/v1/data-integration/pipelines/_compile" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			if got := r.Header.Get("authorization"); got != "Bearer pipeline-token" {
				t.Fatalf("authorization = %q", got)
			}
			if got := r.Header.Get("x-audit-correlation-id"); got != "audit-1" {
				t.Fatalf("audit header = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return jsonResponse(http.StatusOK, `{"plan":{"node_order":["extract","load"],"mode":"sequential"}}`), nil
		})},
	}

	result, err := NewWithClient(client).BuildPipeline(context.Background(), BuildInput{
		PipelineID:         "pipe-1",
		TenantID:           "tenant-a",
		AuditCorrelationID: "audit-1",
		Parameters: map[string]any{
			"status":          "active",
			"nodes":           []any{map[string]any{"id": "extract"}, map[string]any{"id": "load", "depends_on": []any{"extract"}}},
			"schedule_config": map[string]any{},
			"retry_policy":    map[string]any{"max_attempts": 1},
		},
	})
	if err != nil {
		t.Fatalf("BuildPipeline returned error: %v", err)
	}
	if result.Status != "compiled" {
		t.Fatalf("status = %q", result.Status)
	}
	if result.Plan["pipeline_id"] != "pipe-1" {
		t.Fatalf("plan pipeline_id = %v", result.Plan["pipeline_id"])
	}
	if seenBody["status"] != "active" {
		t.Fatalf("compile request status = %v", seenBody["status"])
	}
}

func TestExecutePipelineTriggersBuildRun(t *testing.T) {
	t.Parallel()

	var seenBody map[string]any
	client := &HTTPPipelineServicesClient{
		BuildURL:    "http://pipeline-build-service:50053",
		BearerToken: "pipeline-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if r.URL.Path != "/api/v1/data-integration/pipelines/pipe-1/runs" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			if got := r.Header.Get("authorization"); got != "Bearer pipeline-token" {
				t.Fatalf("authorization = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			return jsonResponse(http.StatusCreated, `{"id":"run-1","pipeline_id":"pipe-1","status":"completed"}`), nil
		})},
	}

	result, err := NewWithClient(client).ExecutePipeline(context.Background(), ExecuteInput{
		PipelineID:         "pipe-1",
		TenantID:           "tenant-a",
		AuditCorrelationID: "audit-2",
		Plan: map[string]any{
			"node_order":      []any{"extract"},
			"start_from_node": "extract",
		},
	})
	if err != nil {
		t.Fatalf("ExecutePipeline returned error: %v", err)
	}
	if result.Status != "completed" || result.RunID != "run-1" {
		t.Fatalf("result = %#v", result)
	}
	if seenBody["from_node_id"] != "extract" {
		t.Fatalf("from_node_id = %v", seenBody["from_node_id"])
	}
	contextBody, ok := seenBody["context"].(map[string]any)
	if !ok {
		t.Fatalf("context = %#v", seenBody["context"])
	}
	if contextBody["tenant_id"] != "tenant-a" {
		t.Fatalf("tenant_id = %v", contextBody["tenant_id"])
	}
}

func TestExecutePipelineReturnsRetryableFailure(t *testing.T) {
	t.Parallel()

	client := &HTTPPipelineServicesClient{
		BuildURL: "http://pipeline-build-service:50053",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("temporary network split")
		})},
	}

	_, err := NewWithClient(client).ExecutePipeline(context.Background(), ExecuteInput{
		PipelineID:         "pipe-1",
		TenantID:           "tenant-a",
		AuditCorrelationID: "audit-3",
		Plan:               map[string]any{"node_order": []any{"extract"}},
	})
	if err == nil {
		t.Fatal("expected retryable error")
	}
	if !strings.Contains(err.Error(), "temporary network split") {
		t.Fatalf("error = %v", err)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"content-type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
