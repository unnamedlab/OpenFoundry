// Package activities hosts the activity implementations called by
// workflows in this module. Activities are **thin HTTP/JSON clients**
// of Rust services — they never touch Cassandra/Postgres directly.
// The authoritative wire-format decision lives in
// docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md
// (§Wire format) and migration-plan task S2.3.c.
//
// Why HTTP REST and not gRPC bindings generated from `proto/`:
//   - The Rust services on the receiving end expose REST handlers,
//     not gRPC servers. Going gRPC would require a parallel,
//     larger migration on the Rust side with no functional payoff
//     for the in-cluster activity case.
//   - `buf.gen.yaml` only emits Rust + TypeScript bindings. Adding a
//     Go target would force every proto change to regenerate and
//     commit `proto/gen/go` plus a Dockerfile dance per worker.
//   - Activities are thin enough (a JSON encode + an HTTP POST) that
//     the bindings would not earn their keep; in-cluster latency is
//     dominated by Temporal orchestration anyway.
//
// Each activity:
//
//   - Reads its target service URL from an env var
//     (`OF_<SERVICE>_URL`; the legacy `OF_<SERVICE>_GRPC_ADDR` name
//     is still accepted for backward compatibility).
//   - Sends `Authorization: Bearer <service-token>` from
//     `OF_<SERVICE>_BEARER_TOKEN` and propagates the audit
//     correlation ID from the workflow context as the
//     `x-audit-correlation-id` HTTP header.
//   - Maps 4xx (except 429) to a non-retryable Temporal error so the
//     workflow does not bounce on bad input; 5xx and 429 fall through
//     to the workflow's RetryPolicy.
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

	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/internal/contract"
	"go.temporal.io/sdk/temporal"
)

// Activities groups every activity wired into the worker. The struct
// is registered as a single bundle so methods become activity names
// of the form `Activities.<MethodName>`.
type Activities struct {
	OntologyActions OntologyActionsClient
}

type OntologyActionsClient interface {
	Execute(ctx context.Context, req OntologyActionRequest) (map[string]any, error)
}

type OntologyActionRequest struct {
	ActionID           string
	TargetObjectID     string
	Parameters         map[string]any
	Justification      string
	AuditCorrelationID string
}

type HTTPOntologyActionsClient struct {
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client
}

// ExecuteOntologyAction is the canonical activity that translates an
// automation step into a call against `ontology-actions-service`.
// The current repo contract for that service is HTTP:
// `POST /api/v1/ontology/actions/{id}/execute`.
func (a *Activities) ExecuteOntologyAction(
	ctx context.Context,
	input contract.AutomationRunInput,
) (map[string]any, error) {
	req, err := ontologyActionRequestFromInput(input)
	if err != nil {
		return nil, nonRetryable("invalid_ontology_action_input", err)
	}
	client := a.OntologyActions
	if client == nil {
		client, err = NewHTTPOntologyActionsClientFromEnv()
		if err != nil {
			return nil, nonRetryable("ontology_actions_config", err)
		}
	}
	result, err := client.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "completed",
		"action_id":        req.ActionID,
		"target_object_id": emptyStringAsNil(req.TargetObjectID),
		"response":         result,
	}, nil
}

func NewHTTPOntologyActionsClientFromEnv() (*HTTPOntologyActionsClient, error) {
	baseURL := firstEnv(
		"OF_ONTOLOGY_ACTIONS_URL",
		"ONTOLOGY_ACTIONS_SERVICE_URL",
		"ONTOLOGY_SERVICE_URL",
		"OF_ONTOLOGY_ACTIONS_GRPC_ADDR",
	)
	if baseURL == "" {
		return nil, errors.New("ontology-actions service URL is not configured")
	}
	token := firstEnv("OF_ONTOLOGY_ACTIONS_BEARER_TOKEN", "ONTOLOGY_ACTIONS_BEARER_TOKEN")
	if token == "" {
		return nil, errors.New("ontology-actions bearer token is not configured")
	}
	return &HTTPOntologyActionsClient{
		BaseURL:     normalizeBaseURL(baseURL),
		BearerToken: normalizeBearerToken(token),
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *HTTPOntologyActionsClient) Execute(ctx context.Context, req OntologyActionRequest) (map[string]any, error) {
	if strings.TrimSpace(req.ActionID) == "" {
		return nil, nonRetryable("invalid_ontology_action_input", errors.New("action_id is required"))
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, nonRetryable("ontology_actions_config", errors.New("base URL is required"))
	}
	if strings.TrimSpace(c.BearerToken) == "" {
		return nil, nonRetryable("ontology_actions_config", errors.New("bearer token is required"))
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	payload := map[string]any{
		"parameters": req.Parameters,
	}
	if req.TargetObjectID != "" {
		payload["target_object_id"] = req.TargetObjectID
	}
	if req.Justification != "" {
		payload["justification"] = req.Justification
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nonRetryable("invalid_ontology_action_input", err)
	}

	endpoint := strings.TrimRight(c.BaseURL, "/") +
		"/api/v1/ontology/actions/" + url.PathEscape(req.ActionID) + "/execute"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, nonRetryable("ontology_actions_request", err)
	}
	httpReq.Header.Set("authorization", normalizeBearerToken(c.BearerToken))
	httpReq.Header.Set("content-type", "application/json")
	if req.AuditCorrelationID != "" {
		httpReq.Header.Set(contract.HeaderAuditCorrelation, req.AuditCorrelationID)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ontology action request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ontology action response read failed: %w", err)
	}
	responsePayload := decodeJSONBody(responseBody)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return responsePayload, nil
	}

	message := responseMessage(responsePayload, responseBody)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return nil, nonRetryable(
			"ontology_actions_client_error",
			fmt.Errorf("ontology action returned %d: %s", resp.StatusCode, message),
		)
	}
	return nil, fmt.Errorf("ontology action returned %d: %s", resp.StatusCode, message)
}

func ontologyActionRequestFromInput(input contract.AutomationRunInput) (OntologyActionRequest, error) {
	payload := input.TriggerPayload
	if nested, ok := payload["ontology_action"].(map[string]any); ok {
		payload = nested
	}
	actionID := stringField(payload, "action_id")
	if actionID == "" {
		return OntologyActionRequest{}, errors.New("trigger_payload.action_id is required")
	}
	req := OntologyActionRequest{
		ActionID:           actionID,
		TargetObjectID:     stringField(payload, "target_object_id"),
		Parameters:         mapField(payload, "parameters"),
		Justification:      stringField(payload, "justification"),
		AuditCorrelationID: input.RunID,
	}
	return req, nil
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
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
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return value
	}
	return "Bearer " + value
}

func stringField(payload map[string]any, key string) string {
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

func mapField(payload map[string]any, key string) map[string]any {
	value, ok := payload[key]
	if !ok || value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{"value": value}
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

func emptyStringAsNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nonRetryable(kind string, err error) error {
	return temporal.NewNonRetryableApplicationError(err.Error(), kind, err)
}
