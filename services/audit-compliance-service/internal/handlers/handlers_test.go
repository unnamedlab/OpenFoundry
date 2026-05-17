package handlers_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// Wire-format pinning: AuditEvent has the full hash-chain field set.
func TestAuditEventJSONShape(t *testing.T) {
	t.Parallel()
	subj := "subject-1"
	e := models.AuditEvent{
		ID: uuid.New(), EventID: uuid.New(), LogEntryID: uuid.New(), Sequence: 42,
		PreviousHash: "AUD-prev", EntryHash: "AUD-curr",
		SourceService: "gateway", Product: "gateway", ProducerType: "SERVER",
		Channel: "http", Actor: "user:x", ActorID: "user:x", ActorType: "user",
		Action: "read", Categories: []string{"dataLoad"},
		ResourceType: "dataset", ResourceID: "ds-1",
		Entities: json.RawMessage(`[{"kind":"dataset","id":"ds-1"}]`),
		Origins:  []string{"10.0.0.1"}, Status: "success", Outcome: "success",
		Severity: "low", Classification: "public", SubjectID: &subj,
		Metadata: json.RawMessage(`{}`), ErrorMetadata: json.RawMessage(`{}`),
		RequestFields: json.RawMessage(`{}`), ResultFields: json.RawMessage(`{}`),
		Labels: json.RawMessage(`[]`), InitiatorType: "user", AuditAccessTier: "security_sensitive",
		RetentionUntil: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		OccurredAt:     time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		IngestedAt:     time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(e)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "sequence", "previous_hash", "entry_hash", "source_service",
		"event_id", "log_entry_id", "product", "producer_type", "channel",
		"actor", "actor_id", "actor_type", "action", "categories",
		"resource_type", "resource_id", "entities", "origins", "status",
		"outcome", "severity", "classification", "subject_id", "ip_address",
		"location", "metadata", "error_metadata", "request_fields",
		"result_fields", "labels", "initiator_type", "audit_access_tier", "retention_until",
		"occurred_at", "ingested_at",
	} {
		assert.Contains(t, view, k)
	}
}

// RetentionPolicy carries is_system + grace + selector/criteria from the
// 0005_retention_system_policies migration.
func TestRetentionPolicyJSONShape(t *testing.T) {
	t.Parallel()
	p := models.RetentionPolicy{
		ID: uuid.New(), Name: "p", Scope: "system",
		TargetKind: "transaction", RetentionDays: 0, LegalHold: false,
		PurgeMode: "hard-delete-after-ttl",
		Rules:     json.RawMessage(`["x"]`),
		UpdatedBy: "system", Active: true, IsSystem: true,
		Selector:           json.RawMessage(`{"all_datasets":true}`),
		Criteria:           json.RawMessage(`{"transaction_state":"ABORTED"}`),
		GracePeriodMinutes: 15,
		CreatedAt:          time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(p)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"is_system", "selector", "criteria", "grace_period_minutes",
		"last_applied_at", "next_run_at",
	} {
		assert.Contains(t, view, k)
	}
}

// Validation paths (no DB).

func TestCreateRetentionPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/retention-policies",
		strings.NewReader(`{"name":"x","target_kind":"dataset","purge_mode":"archive"}`))
	rec := httptest.NewRecorder()
	h.CreateRetentionPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateRetentionPolicyRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/retention-policies",
		strings.NewReader(`{"name":"","target_kind":"","purge_mode":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateRetentionPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "name, target_kind, purge_mode required")
}

func TestCreateRetentionPolicyRejectsNegativeDays(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/retention-policies",
		strings.NewReader(`{"name":"x","target_kind":"d","purge_mode":"archive","retention_days":-1}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateRetentionPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestCreateLineageDeletionRequiresDatasetID(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/lineage-deletion-requests",
		strings.NewReader(`{}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateLineageDeletionRequest(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "dataset_id required")
}

func TestListAuditEventsRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/audit-events", nil)
	rec := httptest.NewRecorder()
	h.ListAuditEvents(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestListAuditEventsRequiresDedicatedAuditPermission(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"viewer"}}
	req := httptest.NewRequest("GET", "/audit-events", nil)
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.ListAuditEvents(rec, req)
	assert.Equal(t, 403, rec.Code)
	assert.Contains(t, rec.Body.String(), "audit-logs:view")
}

func TestAppendEventRejectsInvalidSG16JSON(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/audit/events",
		strings.NewReader(`{"action":"dataset.read","metadata":[]}`))
	rec := httptest.NewRecorder()
	h.AppendEvent(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "metadata")
}

func TestCreateAuditDeliveryDestinationRequiresManagePermission(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Permissions: []string{"audit-logs:view"}}
	req := httptest.NewRequest("POST", "/audit/delivery/destinations",
		strings.NewReader(`{"name":"Security SIEM","destination_type":"siem_api","endpoint_url":"https://siem.example.invalid/audit"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateAuditDeliveryDestination(rec, req)
	assert.Equal(t, 403, rec.Code)
	assert.Contains(t, rec.Body.String(), "audit-delivery:manage")
}

func TestCreateAuditDeliveryDestinationRejectsNonObjectMetadata(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	req := httptest.NewRequest("POST", "/audit/delivery/destinations",
		strings.NewReader(`{"name":"Security SIEM","destination_type":"siem_api","endpoint_url":"https://siem.example.invalid/audit","metadata":[]}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateAuditDeliveryDestination(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "metadata must be a JSON object")
}
