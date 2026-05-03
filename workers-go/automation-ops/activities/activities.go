// Package activities contains the side-effecting work for
// automation-ops workflows. The worker does not write runtime state
// directly; it calls `automation-operations-service` (which owns the
// operational projection) over HTTP REST + JSON with a bearer token
// and the `x-audit-correlation-id` header.
//
// Wire-format decision (HTTP, not gRPC bindings from `proto/`) and
// rationale are documented in ADR-0021 §Wire format and migration-
// plan task S2.7.
package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"

	"github.com/open-foundry/open-foundry/workers-go/automation-ops/internal/contract"
)

type RunInput struct {
	TaskID             string         `json:"task_id"`
	TenantID           string         `json:"tenant_id"`
	TaskType           string         `json:"task_type"`
	Payload            map[string]any `json:"payload,omitempty"`
	AuditCorrelationID string         `json:"audit_correlation_id"`
}

type RunResult struct {
	TaskID string         `json:"task_id"`
	RunID  string         `json:"run_id,omitempty"`
	Status string         `json:"status"`
	Run    map[string]any `json:"run,omitempty"`
}

type AutomationOpsClient interface {
	RecordRun(ctx context.Context, in RunInput) (RunResult, error)
}

type HTTPAutomationOpsClient struct {
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client
}

type Activities struct {
	client AutomationOpsClient
}

func New() *Activities {
	return &Activities{client: NewHTTPAutomationOpsClientFromEnv()}
}

func NewWithClient(client AutomationOpsClient) *Activities {
	return &Activities{client: client}
}

func NewHTTPAutomationOpsClientFromEnv() *HTTPAutomationOpsClient {
	baseURL := firstEnv(
		"OF_AUTOMATION_OPS_URL",
		"AUTOMATION_OPERATIONS_SERVICE_URL",
		"OF_AUTOMATION_OPS_GRPC_ADDR",
	)
	return &HTTPAutomationOpsClient{
		BaseURL:     normalizeBaseURL(defaultString(baseURL, "automation-operations-service:50116")),
		BearerToken: normalizeBearerToken(firstEnv("OF_AUTOMATION_OPS_BEARER_TOKEN", "AUTOMATION_OPS_BEARER_TOKEN")),
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// ExecuteTask records the concrete automation run in automation-operations-
// service. That service owns the read-side projection used by operators during
// the Temporal cutover.
func (a *Activities) ExecuteTask(ctx context.Context, input contract.AutomationOpsInput) (RunResult, error) {
	if strings.TrimSpace(input.TaskID) == "" {
		return RunResult{}, nonRetryable("invalid_automation_ops_input", errors.New("task_id is required"))
	}
	if strings.TrimSpace(input.TaskType) == "" {
		return RunResult{}, nonRetryable("invalid_automation_ops_input", errors.New("task_type is required"))
	}
	client := a.client
	if client == nil {
		client = NewHTTPAutomationOpsClientFromEnv()
	}
	return client.RecordRun(ctx, RunInput{
		TaskID:             input.TaskID,
		TenantID:           input.TenantID,
		TaskType:           input.TaskType,
		Payload:            input.Payload,
		AuditCorrelationID: input.TaskID,
	})
}

func (c *HTTPAutomationOpsClient) RecordRun(ctx context.Context, in RunInput) (RunResult, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return RunResult{}, nonRetryable("automation_ops_config", errors.New("automation-operations service URL is required"))
	}
	body := map[string]any{
		"payload": map[string]any{
			"task_id":              in.TaskID,
			"task_type":            in.TaskType,
			"tenant_id":            in.TenantID,
			"input":                valueOrDefault(in.Payload, map[string]any{}),
			"audit_correlation_id": in.AuditCorrelationID,
			"worker":               "automation-ops",
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return RunResult{}, nonRetryable("invalid_automation_ops_input", err)
	}
	path := "/api/v1/automations/" + url.PathEscape(in.TaskID) + "/runs"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(encoded))
	if err != nil {
		return RunResult{}, nonRetryable("automation_ops_request", err)
	}
	req.Header.Set("content-type", "application/json")
	if c.BearerToken != "" {
		req.Header.Set("authorization", c.BearerToken)
	}
	if in.AuditCorrelationID != "" {
		req.Header.Set(contract.HeaderAuditCorrelation, in.AuditCorrelationID)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return RunResult{}, fmt.Errorf("automation-ops request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return RunResult{}, fmt.Errorf("automation-ops response read failed: %w", err)
	}
	payload := decodeJSONBody(responseBody)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return RunResult{
			TaskID: in.TaskID,
			RunID:  stringField(payload, "id"),
			Status: "completed",
			Run:    payload,
		}, nil
	}
	message := responseMessage(payload, responseBody)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return RunResult{}, nonRetryable("automation_ops_client_error", fmt.Errorf("automation-ops returned %d: %s", resp.StatusCode, message))
	}
	return RunResult{}, fmt.Errorf("automation-ops returned %d: %s", resp.StatusCode, message)
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

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
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
