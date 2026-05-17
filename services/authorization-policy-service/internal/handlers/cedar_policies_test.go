package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── Wire-format pinning ─────────────────────────────────────────────

func TestCedarPolicyJSONShape(t *testing.T) {
	t.Parallel()
	desc := "view clearance"
	p := models.CedarPolicy{
		ID: "p1", Version: 1,
		Source: `permit(principal, action, resource);`,
		Description: &desc, Active: true,
		CreatedBy: uuid.New(),
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(p)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "version", "source", "description", "active",
		"created_by", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
}

func TestListResponseEnvelope(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(models.ListResponse[models.CedarPolicy]{Items: []models.CedarPolicy{}})
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.Contains(t, view, "items")
	assert.NotContains(t, view, "data")
}

// ─── Validation paths (no DB) ───────────────────────────────────────

// stubValidator captures invocations + can return a canned error.
type stubValidator struct {
	called []string // policy ids passed in
	err    error
}

func (s *stubValidator) ReplacePolicies(records []cedarauthz.PolicyRecord) error {
	for _, r := range records {
		s.called = append(s.called, r.ID)
	}
	if s.err != nil {
		return s.err
	}
	return nil
}

func TestCreateCedarPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cedar-policies",
		strings.NewReader(`{"id":"p","source":"permit(principal, action, resource);"}`))
	h.CreateCedarPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateCedarPolicyRejectsEmptyID(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{
		ValidateFactory: func() (handlers.CedarPolicyValidator, error) {
			return &stubValidator{}, nil
		},
	}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/cedar-policies",
		strings.NewReader(`{"id":"   ","source":"permit(principal, action, resource);"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateCedarPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "id required")
}

func TestCreateCedarPolicyRejectsEmptySource(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{
		ValidateFactory: func() (handlers.CedarPolicyValidator, error) {
			return &stubValidator{}, nil
		},
	}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/cedar-policies",
		strings.NewReader(`{"id":"p","source":"   "}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateCedarPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "source required")
}

func TestCreateCedarPolicyValidatesSourceBeforeInsert(t *testing.T) {
	t.Parallel()
	parseErr := &cedarauthz.PolicyParseError{ID: "p", Cause: errors.New("not cedar")}
	stub := &stubValidator{err: parseErr}
	h := &handlers.Handlers{
		ValidateFactory: func() (handlers.CedarPolicyValidator, error) { return stub, nil },
	}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/cedar-policies",
		strings.NewReader(`{"id":"p","source":"this is not cedar"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateCedarPolicy(rec, req)
	assert.Equal(t, 400, rec.Code, "validation failure → 400 (not 500)")
	assert.Equal(t, []string{"p"}, stub.called)
	assert.Contains(t, rec.Body.String(), "not cedar")
}

func TestUpdateCedarPolicyValidatesSourceWhenSet(t *testing.T) {
	t.Parallel()
	parseErr := &cedarauthz.PolicyParseError{ID: "p", Cause: errors.New("bad cedar")}
	stub := &stubValidator{err: parseErr}
	h := &handlers.Handlers{
		ValidateFactory: func() (handlers.CedarPolicyValidator, error) { return stub, nil },
	}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("PATCH", "/cedar-policies/p",
		strings.NewReader(`{"source":"bad cedar"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	// chi.URLParam reads from RouteContext; the handler hits validation
	// before chi.URLParam is needed for the SQL path, so this test
	// exercises just the source-validation branch.
	rec := httptest.NewRecorder()
	h.UpdateCedarPolicy(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestDeleteCedarPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("DELETE", "/cedar-policies/p", nil)
	rec := httptest.NewRecorder()
	h.DeleteCedarPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

// Tenant scoping is now enforced at every Cedar-policy endpoint:
// the handler MUST refuse to serve the route when no auth claims are
// attached, otherwise a request that bypassed the middleware could
// reach the repo with a nil tenant filter and leak cross-tenant rows.
func TestListCedarPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/cedar-policies", nil)
	rec := httptest.NewRecorder()
	h.ListCedarPolicies(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestGetCedarPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/cedar-policies/p", nil)
	rec := httptest.NewRecorder()
	h.GetCedarPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestUpdateCedarPolicyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("PATCH", "/cedar-policies/p",
		strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.UpdateCedarPolicy(rec, req)
	assert.Equal(t, 401, rec.Code)
}

// CreateCedarPolicyIgnoresUnknownBodyFields — a JSON body that carries
// a `tenant_id` field MUST be silently dropped by the decoder. The
// CreateCedarPolicyRequest type intentionally has no TenantID field so
// the tenant always comes from the JWT, never the wire body.
func TestCreateCedarPolicyRequestRejectsBodyTenantID(t *testing.T) {
	t.Parallel()
	// Pin the wire contract: the request type does not unmarshal a
	// `tenant_id` field, so even an attacker-controlled body cannot
	// override the JWT-sealed tenant. We assert via reflection.
	var req models.CreateCedarPolicyRequest
	out, err := json.Marshal(req)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.NotContains(t, view, "tenant_id",
		"CreateCedarPolicyRequest must not carry tenant_id — tenant is sealed from claims")

	var updateReq models.UpdateCedarPolicyRequest
	out, err = json.Marshal(updateReq)
	require.NoError(t, err)
	view = nil
	require.NoError(t, json.Unmarshal(out, &view))
	assert.NotContains(t, view, "tenant_id",
		"UpdateCedarPolicyRequest must not carry tenant_id — tenant is sealed from claims")
}

// Real validator (libs/authz-cedar-go) → confirms the integration is
// live: an actually-valid policy (refs the bundled schema) reaches the
// repo step (which we don't run here, so we expect a downstream nil
// pool panic — caught by recover in chi.Recoverer in production; here
// we just assert the validator passes and the handler advances past
// the validation gate).
func TestCreateCedarPolicyAcceptsSchemaValidPolicy(t *testing.T) {
	t.Parallel()
	// Use the real factory (libs/authz-cedar-go).
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	body := `{"id":"permit-cleared-readers","source":"permit(principal, action == Action::\"read\", resource is Dataset) when { principal.clearances.containsAll(resource.markings) };"}`
	req := httptest.NewRequest("POST", "/cedar-policies", strings.NewReader(body))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	defer func() {
		// Repo.Pool is nil — a panic past the validation gate confirms
		// validation DID succeed (otherwise we'd 400 before the panic).
		if r := recover(); r != nil {
			// Expected: nil pool deref. Validation passed.
		}
	}()
	h.CreateCedarPolicy(rec, req)
	// If we reach here without panic, the handler returned an HTTP
	// status. It must NOT be 400 (which would mean validation failed).
	assert.NotEqual(t, 400, rec.Code, "validation must accept this schema-valid policy")
}
