package activities

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type recordingAuditClient struct {
	events []AuditEvent
	err    error
}

func (c *recordingAuditClient) AppendEvent(_ context.Context, evt AuditEvent) (map[string]any, error) {
	c.events = append(c.events, evt)
	return map[string]any{"id": "audit-1"}, c.err
}

func TestEmitAuditEventUsesAuditClient(t *testing.T) {
	t.Parallel()

	client := &recordingAuditClient{}
	err := NewWithAuditClient(client).EmitAuditEvent(context.Background(), AuditEvent{
		OccurredAt:         time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
		TenantID:           "tenant-a",
		Actor:              "alice",
		Action:             "approval.approved",
		ResourceType:       "approval_request",
		ResourceID:         "request-1",
		AuditCorrelationID: "wf-approval-1",
		Attributes:         map[string]any{"subject": "deploy"},
	})
	if err != nil {
		t.Fatalf("EmitAuditEvent returned error: %v", err)
	}
	if len(client.events) != 1 {
		t.Fatalf("events len = %d", len(client.events))
	}
	if client.events[0].Action != "approval.approved" {
		t.Fatalf("action = %q", client.events[0].Action)
	}
}

func TestEmitAuditEventPropagatesRecoverablePublisherError(t *testing.T) {
	t.Parallel()

	client := &recordingAuditClient{err: errors.New("temporary audit outage")}
	err := NewWithAuditClient(client).EmitAuditEvent(context.Background(), AuditEvent{
		TenantID:     "tenant-a",
		Action:       "approval.approved",
		ResourceType: "approval_request",
		ResourceID:   "request-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "temporary audit outage") {
		t.Fatalf("error = %v", err)
	}
}

func TestEmitAuditEventAppendsToAuditCompliance(t *testing.T) {
	t.Parallel()

	var seenBody map[string]any
	client := &HTTPAuditClient{
		BaseURL:     "http://audit-compliance-service:50115",
		BearerToken: "Bearer audit-token",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s", r.Method)
			}
			if r.URL.Path != "/api/v1/audit/events" {
				t.Fatalf("path = %s", r.URL.Path)
			}
			if got := r.Header.Get("authorization"); got != "Bearer audit-token" {
				t.Fatalf("authorization = %q", got)
			}
			if got := r.Header.Get("x-audit-correlation-id"); got != "wf-approval-1" {
				t.Fatalf("audit header = %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(`{"id":"audit-1","sequence":1}`)),
			}, nil
		})},
	}

	err := NewWithAuditClient(client).EmitAuditEvent(context.Background(), AuditEvent{
		OccurredAt:         time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
		TenantID:           "tenant-a",
		Actor:              "alice",
		Action:             "approval.approved",
		ResourceType:       "approval_request",
		ResourceID:         "request-1",
		AuditCorrelationID: "wf-approval-1",
		Attributes:         map[string]any{"subject": "deploy"},
	})
	if err != nil {
		t.Fatalf("EmitAuditEvent returned error: %v", err)
	}
	if seenBody["source_service"] != "approvals-worker" {
		t.Fatalf("source_service = %v", seenBody["source_service"])
	}
	if seenBody["action"] != "approval.approved" {
		t.Fatalf("action = %v", seenBody["action"])
	}
	if seenBody["status"] != "success" {
		t.Fatalf("status = %v", seenBody["status"])
	}
	metadata, ok := seenBody["metadata"].(map[string]any)
	if !ok || metadata["tenant_id"] != "tenant-a" {
		t.Fatalf("metadata = %#v", seenBody["metadata"])
	}
}

func TestAuditClientMapsRejectedApprovalToDenied(t *testing.T) {
	t.Parallel()

	var seenBody map[string]any
	client := &HTTPAuditClient{
		BaseURL: "http://audit-compliance-service:50115",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})},
	}

	_, err := client.AppendEvent(context.Background(), AuditEvent{
		Action:       "approval.rejected",
		ResourceType: "approval_request",
		ResourceID:   "request-2",
	})
	if err != nil {
		t.Fatalf("AppendEvent returned error: %v", err)
	}
	if seenBody["status"] != "denied" {
		t.Fatalf("status = %v", seenBody["status"])
	}
	if seenBody["severity"] != "medium" {
		t.Fatalf("severity = %v", seenBody["severity"])
	}
}

func TestAuditClientReturnsNonRetryableForBadRequest(t *testing.T) {
	t.Parallel()

	client := &HTTPAuditClient{
		BaseURL: "http://audit-compliance-service:50115",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"error":"action is required"}`)),
			}, nil
		})},
	}

	_, err := client.AppendEvent(context.Background(), AuditEvent{
		Action:       "approval.approved",
		ResourceType: "approval_request",
		ResourceID:   "request-3",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "audit append returned 400: action is required") {
		t.Fatalf("error = %v", err)
	}
}
