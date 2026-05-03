// Package activities holds Temporal activities for the approvals
// worker. The activity emits the audit event for an approval decision
// to `audit-compliance-service` over HTTP REST + JSON with bearer
// token and `x-audit-correlation-id`; that service anchors the audit
// hash chain and publishes downstream to Kafka `audit.events` via
// the Postgres outbox (ADR-0022).
//
// Wire-format decision (HTTP, not gRPC bindings from `proto/`) and
// rationale are documented in ADR-0021 §Wire format and migration-
// plan task S2.5.c.
package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"go.temporal.io/sdk/temporal"
)

// AuditEvent is the cross-language audit envelope emitted after every
// approval decision or expiry.
type AuditEvent struct {
	OccurredAt         time.Time      `json:"occurred_at"`
	TenantID           string         `json:"tenant_id"`
	Actor              string         `json:"actor"`
	Action             string         `json:"action"`
	ResourceType       string         `json:"resource_type"`
	ResourceID         string         `json:"resource_id"`
	AuditCorrelationID string         `json:"audit_correlation_id"`
	Attributes         map[string]any `json:"attributes,omitempty"`
}

type AuditClient interface {
	AppendEvent(ctx context.Context, evt AuditEvent) (map[string]any, error)
}

type HTTPAuditClient struct {
	BaseURL     string
	BearerToken string
	HTTPClient  *http.Client
}

// Activities groups the activity implementations so they can be registered
// in one shot via `w.RegisterActivity(activities.New())`.
type Activities struct {
	audit AuditClient
}

func New() *Activities {
	return &Activities{audit: NewHTTPAuditClientFromEnv()}
}

func NewWithAuditClient(client AuditClient) *Activities {
	return &Activities{audit: client}
}

func NewHTTPAuditClientFromEnv() *HTTPAuditClient {
	baseURL := firstEnv(
		"OF_AUDIT_COMPLIANCE_URL",
		"OF_AUDIT_URL",
		"AUDIT_COMPLIANCE_SERVICE_URL",
		"AUDIT_SERVICE_URL",
		"OF_AUDIT_GRPC_ADDR",
	)
	return &HTTPAuditClient{
		BaseURL:     normalizeBaseURL(defaultString(baseURL, "audit-compliance-service:50115")),
		BearerToken: normalizeBearerToken(firstEnv("OF_AUDIT_BEARER_TOKEN", "AUDIT_BEARER_TOKEN")),
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// EmitAuditEvent records the approval decision in audit-compliance-service.
// The service owns hash chaining, classification defaults and durable storage;
// this worker only translates the Temporal decision into that append contract.
func (a *Activities) EmitAuditEvent(ctx context.Context, evt AuditEvent) error {
	if strings.TrimSpace(evt.Action) == "" {
		return nonRetryable("invalid_audit_event", errors.New("action is required"))
	}
	client := a.audit
	if client == nil {
		client = NewHTTPAuditClientFromEnv()
	}
	_, err := client.AppendEvent(ctx, evt)
	return err
}

func (c *HTTPAuditClient) AppendEvent(ctx context.Context, evt AuditEvent) (map[string]any, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, nonRetryable("audit_config", errors.New("audit-compliance-service URL is required"))
	}
	body := map[string]any{
		"source_service": "approvals-worker",
		"channel":        "temporal",
		"actor":          defaultString(evt.Actor, "system"),
		"action":         evt.Action,
		"resource_type":  evt.ResourceType,
		"resource_id":    evt.ResourceID,
		"status":         statusForAction(evt.Action),
		"severity":       severityForAction(evt.Action),
		"classification": "confidential",
		"subject_id":     evt.TenantID,
		"metadata": map[string]any{
			"tenant_id":             evt.TenantID,
			"audit_correlation_id":  evt.AuditCorrelationID,
			"occurred_at":           evt.OccurredAt.UTC().Format(time.RFC3339Nano),
			"approval_attributes":   valueOrDefault(evt.Attributes, map[string]any{}),
			"temporal_activity_src": "approvals.EmitAuditEvent",
		},
		"labels":         []string{"approval", "temporal"},
		"retention_days": 365,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, nonRetryable("invalid_audit_event", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(c.BaseURL, "/")+"/api/v1/audit/events",
		bytes.NewReader(encoded),
	)
	if err != nil {
		return nil, nonRetryable("audit_request", err)
	}
	req.Header.Set("content-type", "application/json")
	if c.BearerToken != "" {
		req.Header.Set("authorization", c.BearerToken)
	}
	if evt.AuditCorrelationID != "" {
		req.Header.Set("x-audit-correlation-id", evt.AuditCorrelationID)
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("audit append request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("audit append response read failed: %w", err)
	}
	payload := decodeJSONBody(responseBody)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return payload, nil
	}
	message := responseMessage(payload, responseBody)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return nil, nonRetryable("audit_client_error", fmt.Errorf("audit append returned %d: %s", resp.StatusCode, message))
	}
	return nil, fmt.Errorf("audit append returned %d: %s", resp.StatusCode, message)
}

func statusForAction(action string) string {
	if strings.Contains(strings.ToLower(action), "rejected") {
		return "denied"
	}
	return "success"
}

func severityForAction(action string) string {
	if strings.Contains(strings.ToLower(action), "rejected") {
		return "medium"
	}
	return "low"
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
