// Package activities holds Temporal activities for the pipeline worker.
// The activities are thin HTTP/JSON clients of the Rust services that
// own pipeline authoring and execution state — `pipeline-authoring-
// service` for compile, `pipeline-build-service` for run.
//
// Wire format is HTTP REST + JSON with bearer token and the
// `x-audit-correlation-id` header. The decision and its rationale
// (in-cluster latency acceptable, avoids regenerating proto/gen/go on
// every change, `proto/` stays the source-of-truth for Rust + TS) are
// documented in ADR-0021 §Wire format and migration-plan task S2.6.
package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"

	"github.com/open-foundry/open-foundry/workers-go/pipeline/internal/contract"
)

// BuildInput selects the pipeline revision to compile. The Plan
// returned by the activity is opaque to the worker and is forwarded to
// ExecutePipeline.
type BuildInput struct {
	PipelineID         string         `json:"pipeline_id"`
	TenantID           string         `json:"tenant_id"`
	Revision           string         `json:"revision,omitempty"`
	Parameters         map[string]any `json:"parameters,omitempty"`
	AuditCorrelationID string         `json:"audit_correlation_id"`
}

type BuildResult struct {
	PipelineID string         `json:"pipeline_id"`
	Status     string         `json:"status"` // "compiled" | "failed"
	Plan       map[string]any `json:"plan,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type ExecuteInput struct {
	PipelineID         string         `json:"pipeline_id"`
	TenantID           string         `json:"tenant_id"`
	Plan               map[string]any `json:"plan"`
	AuditCorrelationID string         `json:"audit_correlation_id"`
}

type ExecuteResult struct {
	PipelineID string         `json:"pipeline_id"`
	Status     string         `json:"status"` // "completed" | "failed" | "running"
	RunID      string         `json:"run_id,omitempty"`
	Run        map[string]any `json:"run,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type PipelineServicesClient interface {
	Build(ctx context.Context, in BuildInput) (BuildResult, error)
	Execute(ctx context.Context, in ExecuteInput) (ExecuteResult, error)
}

type HTTPPipelineServicesClient struct {
	AuthoringURL string
	BuildURL     string
	BearerToken  string
	HTTPClient   *http.Client
}

type Activities struct {
	client PipelineServicesClient
	logger *slog.Logger
}

func New() *Activities {
	return &Activities{
		client: NewHTTPPipelineServicesClientFromEnv(),
		logger: slog.Default(),
	}
}

func NewWithClient(client PipelineServicesClient) *Activities {
	return &Activities{client: client, logger: slog.Default()}
}

func NewHTTPPipelineServicesClientFromEnv() *HTTPPipelineServicesClient {
	authoringURL := firstEnv(
		"OF_PIPELINE_AUTHORING_URL",
		"PIPELINE_AUTHORING_SERVICE_URL",
		"OF_PIPELINE_BUILD_GRPC_ADDR",
	)
	buildURL := firstEnv(
		"OF_PIPELINE_BUILD_URL",
		"PIPELINE_BUILD_SERVICE_URL",
		"OF_PIPELINE_EXEC_URL",
		"OF_PIPELINE_EXEC_GRPC_ADDR",
	)
	return &HTTPPipelineServicesClient{
		AuthoringURL: normalizeBaseURL(defaultString(authoringURL, "pipeline-authoring-service:50080")),
		BuildURL:     normalizeBaseURL(defaultString(buildURL, "pipeline-build-service:50081")),
		BearerToken:  normalizeBearerToken(firstEnv("OF_PIPELINE_BEARER_TOKEN", "PIPELINE_BEARER_TOKEN")),
		HTTPClient:   &http.Client{Timeout: 60 * time.Second},
	}
}

// BuildPipeline compiles the current pipeline definition through
// pipeline-authoring-service. If Parameters already contains an
// in-flight compile request, it is used directly; otherwise the activity
// fetches the persisted pipeline and compiles that graph.
func (a *Activities) BuildPipeline(ctx context.Context, in BuildInput) (BuildResult, error) {
	client := a.client
	if client == nil {
		client = NewHTTPPipelineServicesClientFromEnv()
	}
	result, err := client.Build(ctx, in)
	if err != nil {
		return BuildResult{PipelineID: in.PipelineID, Status: "failed", Error: err.Error()}, err
	}
	if result.Status == "" {
		result.Status = "compiled"
	}
	result.PipelineID = defaultString(result.PipelineID, in.PipelineID)
	return result, nil
}

// ExecutePipeline triggers a real run in pipeline-build-service using
// the compiled plan metadata produced by BuildPipeline.
func (a *Activities) ExecutePipeline(ctx context.Context, in ExecuteInput) (ExecuteResult, error) {
	client := a.client
	if client == nil {
		client = NewHTTPPipelineServicesClientFromEnv()
	}
	result, err := client.Execute(ctx, in)
	if err != nil {
		return ExecuteResult{PipelineID: in.PipelineID, Status: "failed", Error: err.Error()}, err
	}
	result.PipelineID = defaultString(result.PipelineID, in.PipelineID)
	return result, nil
}

func (c *HTTPPipelineServicesClient) Build(ctx context.Context, in BuildInput) (BuildResult, error) {
	if strings.TrimSpace(in.PipelineID) == "" {
		return BuildResult{}, nonRetryable("invalid_pipeline_build_input", errors.New("pipeline_id is required"))
	}
	if strings.TrimSpace(c.AuthoringURL) == "" {
		return BuildResult{}, nonRetryable("pipeline_build_config", errors.New("authoring URL is required"))
	}
	compileReq, err := c.compileRequest(ctx, in)
	if err != nil {
		return BuildResult{}, err
	}
	payload, err := c.postJSON(ctx, c.AuthoringURL, "/api/v1/data-integration/pipelines/_compile", compileReq, in.AuditCorrelationID)
	if err != nil {
		return BuildResult{}, err
	}
	plan, ok := payload["plan"].(map[string]any)
	if !ok {
		return BuildResult{}, nonRetryable("pipeline_compile_response", errors.New("compile response did not include plan object"))
	}
	plan["pipeline_id"] = in.PipelineID
	plan["tenant_id"] = in.TenantID
	if in.Revision != "" {
		plan["revision"] = in.Revision
	}
	return BuildResult{
		PipelineID: in.PipelineID,
		Status:     "compiled",
		Plan:       plan,
	}, nil
}

func (c *HTTPPipelineServicesClient) Execute(ctx context.Context, in ExecuteInput) (ExecuteResult, error) {
	if strings.TrimSpace(in.PipelineID) == "" {
		return ExecuteResult{}, nonRetryable("invalid_pipeline_execute_input", errors.New("pipeline_id is required"))
	}
	if strings.TrimSpace(c.BuildURL) == "" {
		return ExecuteResult{}, nonRetryable("pipeline_execute_config", errors.New("build URL is required"))
	}
	body := map[string]any{
		"context": map[string]any{
			"trigger": map[string]any{
				"type": "temporal",
			},
			"tenant_id":             in.TenantID,
			"compiled_plan":         in.Plan,
			"audit_correlation_id":  in.AuditCorrelationID,
			"temporal_task_queue":   contract.TaskQueue,
			"temporal_workflow_ref": in.AuditCorrelationID,
		},
		"skip_unchanged": true,
	}
	if startFrom := stringField(in.Plan, "start_from_node"); startFrom != "" {
		body["from_node_id"] = startFrom
	}
	path := "/api/v1/data-integration/pipelines/" + url.PathEscape(in.PipelineID) + "/runs"
	payload, err := c.postJSON(ctx, c.BuildURL, path, body, in.AuditCorrelationID)
	if err != nil {
		return ExecuteResult{}, err
	}
	status := stringField(payload, "status")
	if status == "" {
		status = "running"
	}
	return ExecuteResult{
		PipelineID: in.PipelineID,
		Status:     normalizeRunStatus(status),
		RunID:      stringField(payload, "id"),
		Run:        payload,
	}, nil
}

func (c *HTTPPipelineServicesClient) compileRequest(ctx context.Context, in BuildInput) (map[string]any, error) {
	if isCompileRequest(in.Parameters) {
		req := cloneMap(in.Parameters)
		return req, nil
	}
	pipeline, err := c.getJSON(
		ctx,
		c.AuthoringURL,
		"/api/v1/data-integration/pipelines/"+url.PathEscape(in.PipelineID),
		in.AuditCorrelationID,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":                   defaultString(stringField(pipeline, "status"), "active"),
		"nodes":                    valueOrDefault(pipeline["dag"], []any{}),
		"schedule_config":          valueOrDefault(pipeline["schedule_config"], map[string]any{}),
		"retry_policy":             valueOrDefault(pipeline["retry_policy"], map[string]any{}),
		"start_from_node":          stringField(in.Parameters, "start_from_node"),
		"distributed_worker_count": intField(in.Parameters, "distributed_worker_count", 1),
	}, nil
}

func (c *HTTPPipelineServicesClient) getJSON(ctx context.Context, baseURL, path, auditID string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return nil, nonRetryable("pipeline_request", err)
	}
	c.decorate(req, auditID)
	return c.do(req)
}

func (c *HTTPPipelineServicesClient) postJSON(ctx context.Context, baseURL, path string, body map[string]any, auditID string) (map[string]any, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, nonRetryable("pipeline_request", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, nonRetryable("pipeline_request", err)
	}
	c.decorate(req, auditID)
	req.Header.Set("content-type", "application/json")
	return c.do(req)
}

func (c *HTTPPipelineServicesClient) decorate(req *http.Request, auditID string) {
	if c.BearerToken != "" {
		req.Header.Set("authorization", normalizeBearerToken(c.BearerToken))
	}
	if auditID != "" {
		req.Header.Set(contract.HeaderAuditCorrelation, auditID)
	}
}

func (c *HTTPPipelineServicesClient) do(req *http.Request) (map[string]any, error) {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pipeline service request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("pipeline service response read failed: %w", err)
	}
	payload := decodeJSONBody(body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return payload, nil
	}
	message := responseMessage(payload, body)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return nil, nonRetryable("pipeline_client_error", fmt.Errorf("pipeline service returned %d: %s", resp.StatusCode, message))
	}
	return nil, fmt.Errorf("pipeline service returned %d: %s", resp.StatusCode, message)
}

func isCompileRequest(params map[string]any) bool {
	if params == nil {
		return false
	}
	_, hasNodes := params["nodes"]
	_, hasPipeline := params["pipeline"]
	return hasNodes || hasPipeline
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func normalizeBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if strings.Contains(value, "://") {
		return value
	}
	return "http://" + value
}

func normalizeBearerToken(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return value
	}
	return "Bearer " + value
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intField(payload map[string]any, key string, fallback int) int {
	if payload == nil {
		return fallback
	}
	switch typed := payload[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func normalizeRunStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "cancelled", "aborted":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "running"
	}
}

func decodeJSONBody(body []byte) map[string]any {
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err == nil {
		return decoded
	}
	return map[string]any{"raw": string(body)}
}

func responseMessage(payload map[string]any, body []byte) string {
	for _, key := range []string{"error", "message", "details"} {
		if value, ok := payload[key]; ok && value != nil {
			return fmt.Sprint(value)
		}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return http.StatusText(http.StatusInternalServerError)
	}
	return string(body)
}

func nonRetryable(kind string, err error) error {
	return temporal.NewNonRetryableApplicationError(err.Error(), kind, err)
}
