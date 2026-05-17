package handlers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func TestValidateCreateRestrictedViewRequestMatchesRustContract(t *testing.T) {
	t.Parallel()

	enabled := true
	body := &models.CreateRestrictedViewRequest{
		Name:            " confidential redaction ",
		Resource:        " datasets ",
		Action:          " read ",
		HiddenColumns:   json.RawMessage(`["ssn"]`),
		AllowedOrgIDs:   json.RawMessage(`["` + uuid.NewString() + `"]`),
		AllowedMarkings: json.RawMessage(`["public","pii"]`),
		Enabled:         &enabled,
	}

	require.NoError(t, validateCreateRestrictedViewRequest(body))
	require.Equal(t, "confidential redaction", body.Name)
	require.Equal(t, "datasets", body.Resource)
	require.Equal(t, "read", body.Action)
}

func TestValidateRestrictedViewRejectsInvalidHiddenColumn(t *testing.T) {
	t.Parallel()

	enabled := true
	body := &models.CreateRestrictedViewRequest{
		Name:          "redaction",
		Resource:      "datasets",
		Action:        "read",
		HiddenColumns: json.RawMessage(`["ssn", " "]`),
		Enabled:       &enabled,
	}

	require.ErrorContains(t, validateCreateRestrictedViewRequest(body), "hidden_columns cannot contain empty values")
}

func TestValidateRestrictedViewRejectsInvalidMarking(t *testing.T) {
	t.Parallel()

	body := &models.UpdateRestrictedViewRequest{
		AllowedMarkings: json.RawMessage(`["secret"]`),
	}

	require.ErrorContains(t, validateUpdateRestrictedViewRequest(body, false), "invalid marking 'secret'")
	require.ErrorContains(t, validateUpdateRestrictedViewRequest(body, false), "expected a marking UUID")
}

func TestValidateRestrictedViewRejectsInvalidAllowedOrgID(t *testing.T) {
	t.Parallel()

	body := &models.UpdateRestrictedViewRequest{
		AllowedOrgIDs: json.RawMessage(`["not-a-uuid"]`),
	}

	require.ErrorContains(t, validateUpdateRestrictedViewRequest(body, false), "allowed_org_ids must be an array of UUIDs")
}

func TestValidateRestrictedViewPUTRequiresFullRustUpsertShape(t *testing.T) {
	t.Parallel()

	body := &models.UpdateRestrictedViewRequest{}

	require.ErrorContains(t, validateUpdateRestrictedViewRequest(body, true), "name, resource, action and enabled required")
}

func TestValidateRestrictedViewAcceptsMarkingColumnsWithSchemaHint(t *testing.T) {
	t.Parallel()

	enabled := true
	body := &models.CreateRestrictedViewRequest{
		Name:                 "case access",
		Resource:             "datasets",
		Action:               "read",
		MarkingColumns:       json.RawMessage(`["data_markings"]`),
		AllowedMarkings:      json.RawMessage(`["` + uuid.NewString() + `"]`),
		BackingDatasetSchema: json.RawMessage(`{"fieldSchemaList":[{"name":"data_markings","type":"ARRAY","arraySubtype":{"type":"STRING"},"customMetadata":{"typeclasses":["marking_type.mandatory"]}}]}`),
		Enabled:              &enabled,
	}

	require.NoError(t, validateCreateRestrictedViewRequest(body))
}

func TestValidateRestrictedViewRejectsUnsupportedMarkingColumnType(t *testing.T) {
	t.Parallel()

	body := &models.UpdateRestrictedViewRequest{
		MarkingColumns:       json.RawMessage(`["data_markings"]`),
		BackingDatasetSchema: json.RawMessage(`{"fields":[{"name":"data_markings","type":"STRING","customMetadata":{"typeclasses":["marking_type.mandatory"]}}]}`),
	}

	require.ErrorContains(t, validateUpdateRestrictedViewRequest(body, false), `marking column "data_markings" must be ARRAY<STRING>`)
}

// ─── Tenant isolation (no DB) ───────────────────────────────────────

func TestTenantFromRequestRequiresAuthentication(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/restricted-views", nil)
	w := httptest.NewRecorder()
	id, ok := tenantFromRequest(w, r)
	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, id)
	assert.Equal(t, 401, w.Code)
}

func TestTenantFromRequestRejectsTenantlessSubject(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequest("GET", "/restricted-views", nil)
	r = r.WithContext(authmw.ContextWithClaims(r.Context(), &authmw.Claims{Sub: uuid.New()}))
	w := httptest.NewRecorder()
	id, ok := tenantFromRequest(w, r)
	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, id)
	assert.Equal(t, 403, w.Code)
}

func TestTenantFromRequestReturnsOrgIDForAuthenticatedSubject(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	r := httptest.NewRequest("GET", "/restricted-views", nil)
	r = r.WithContext(authmw.ContextWithClaims(r.Context(), &authmw.Claims{Sub: uuid.New(), OrgID: &org}))
	w := httptest.NewRecorder()
	id, ok := tenantFromRequest(w, r)
	assert.True(t, ok)
	assert.Equal(t, org, id)
}

// TestCreateRestrictedViewRejectsTenantlessCaller pins the wire
// contract: a JWT without an org_id claim cannot author a
// restricted_view, otherwise the row would land on the all-zero
// sentinel tenant and effectively be invisible to every legitimate
// caller (default-deny on read).
func TestCreateRestrictedViewRejectsTenantlessCaller(t *testing.T) {
	t.Parallel()

	h := &RestrictedViews{}
	body := strings.NewReader(`{"name":"x","resource":"r","action":"a","enabled":true}`)
	r := httptest.NewRequest("POST", "/restricted-views", body)
	r = r.WithContext(authmw.ContextWithClaims(r.Context(), &authmw.Claims{Sub: uuid.New()}))
	w := httptest.NewRecorder()
	h.Create(w, r)
	assert.Equal(t, 403, w.Code)
}

// Sanity check that we exercise the authmw.ContextWithClaims + FromContext
// pair the way restricted-view handlers do at runtime.
func TestAuthmwContextRoundTrip(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	claims := &authmw.Claims{Sub: uuid.New(), OrgID: &org}
	ctx := authmw.ContextWithClaims(context.Background(), claims)
	got, ok := authmw.FromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, claims.Sub, got.Sub)
	assert.Equal(t, &org, got.OrgID)

	enabled := true
	require.NoError(t, validateCreateRestrictedViewRequest(&models.CreateRestrictedViewRequest{
		Name: "n", Resource: "r", Action: "a", Enabled: &enabled,
		HiddenColumns: json.RawMessage(`[]`),
	}))
}
