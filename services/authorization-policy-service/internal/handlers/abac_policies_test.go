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
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// claimsWithTenant returns a minimal *Claims tied to a fresh tenant.
// Every CRUD handler now requires a tenant on the JWT; tests that
// exercise validation paths still need to clear the auth gate.
func claimsWithTenant(t *testing.T) (*authmw.Claims, uuid.UUID) {
	t.Helper()
	tenantID := uuid.New()
	return &authmw.Claims{Sub: uuid.New(), OrgID: &tenantID}, tenantID
}

// ─── Wire-format pinning ─────────────────────────────────────────────

func TestABACPolicyJSONShape(t *testing.T) {
	t.Parallel()
	desc := "deny PII rows"
	rowFilter := "tenant = $caller_tenant"
	cb := uuid.New()
	p := models.ABACPolicy{
		ID:       uuid.New(),
		TenantID: uuid.New(),
		Name:     "deny-pii",
		Description: &desc, Effect: "deny",
		Resource: "datasets", Action: "read",
		Conditions: json.RawMessage(`{"marking":"pii"}`),
		RowFilter:  &rowFilter, Enabled: true,
		CreatedBy: &cb,
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(p)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "tenant_id", "name", "description", "effect", "resource", "action",
		"conditions", "row_filter", "enabled", "created_by",
		"created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "deny", view["effect"])
}

// ─── Validation paths (no DB) ───────────────────────────────────────

func TestCreateABACPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/abac-policies",
		strings.NewReader(`{"name":"x","effect":"allow","resource":"r","action":"a"}`))
	rec := httptest.NewRecorder()
	h.CreateABACPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateABACPolicyRequiresTenant(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	// Authenticated but no OrgID on the claims — handler must 403.
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/abac-policies",
		strings.NewReader(`{"name":"x","effect":"allow","resource":"r","action":"a"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateABACPolicy(rec, req)
	assert.Equal(t, 403, rec.Code)
	assert.Contains(t, rec.Body.String(), "tenant scope required")
}

func TestCreateABACPolicyRejectsBadEffect(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c, _ := claimsWithTenant(t)
	req := httptest.NewRequest("POST", "/abac-policies",
		strings.NewReader(`{"name":"x","effect":"maybe","resource":"r","action":"a"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateABACPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "effect must be 'allow' or 'deny'")
}

func TestCreateABACPolicyRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c, _ := claimsWithTenant(t)
	req := httptest.NewRequest("POST", "/abac-policies",
		strings.NewReader(`{"name":"","effect":"allow","resource":"r","action":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateABACPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "name, resource, and action required")
}

func TestCreateABACPolicyRejectsInvalidConditionsJSON(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c, _ := claimsWithTenant(t)
	// `conditions` is a json.RawMessage in Go — sending a string with invalid JSON via the
	// conditions field surfaces a 400 from the handler.
	req := httptest.NewRequest("POST", "/abac-policies",
		strings.NewReader(`{"name":"x","effect":"allow","resource":"r","action":"a","conditions":{"open":}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateABACPolicy(rec, req)
	// The decode itself fails on malformed JSON → 400 invalid body.
	assert.Equal(t, 400, rec.Code)
}

func TestUpdateABACPolicyRejectsBadEffect(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c, _ := claimsWithTenant(t)
	req := httptest.NewRequest("PATCH", "/abac-policies/"+uuid.New().String(),
		strings.NewReader(`{"effect":"hack"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.UpdateABACPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestDeleteABACPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("DELETE", "/abac-policies/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	h.DeleteABACPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestListABACPoliciesRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/abac-policies", nil)
	rec := httptest.NewRecorder()
	h.ListABACPolicies(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestListABACPoliciesRequiresTenant(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("GET", "/abac-policies", nil)
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.ListABACPolicies(rec, req)
	assert.Equal(t, 403, rec.Code)
	assert.Contains(t, rec.Body.String(), "tenant scope required")
}
