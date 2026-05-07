package properties

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func sampleClaims() *authmw.Claims {
	return &authmw.Claims{Sub: uuid.Nil, Email: "test@example.com"}
}

func withClaims(claims *authmw.Claims, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := authmw.ContextWithClaims(r.Context(), claims)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// libs/ontology-kernel/src/handlers/properties.rs — every endpoint
// returns 401 when the request carries no Claims.
func TestEndpointsRequireClaims(t *testing.T) {
	state := &ontologykernel.AppState{}
	tid := uuid.New().String()
	pid := uuid.New().String()
	cases := []struct {
		method, path, body string
		fn                 http.HandlerFunc
	}{
		{http.MethodGet, "/ontology/types/" + tid + "/properties", ``, ListProperties(state)},
		{http.MethodPost, "/ontology/types/" + tid + "/properties", `{}`, CreateProperty(state)},
		{http.MethodPatch, "/ontology/types/" + tid + "/properties/" + pid, `{}`, UpdateProperty(state)},
		{http.MethodDelete, "/ontology/types/" + tid + "/properties/" + pid, ``, DeleteProperty(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

// libs/ontology-kernel/src/handlers/properties.rs — Mount registers
// every endpoint at the documented path / verb.
func TestMountRegistersEveryRoute(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	Mount(r, state)

	got := map[string]bool{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	})
	want := []string{
		"GET /ontology/types/{type_id}/properties",
		"POST /ontology/types/{type_id}/properties",
		"PATCH /ontology/types/{type_id}/properties/{property_id}",
		"DELETE /ontology/types/{type_id}/properties/{property_id}",
	}
	for _, key := range want {
		assert.True(t, got[key], "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/properties.rs — pathUUID rejects
// malformed type_id with 400.
func TestPathUUIDRejectsMalformed(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	r.Get("/ontology/types/{type_id}/properties", ListProperties(state))
	req := httptest.NewRequest(http.MethodGet, "/ontology/types/not-a-uuid/properties", nil)
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid type_id")
}

// libs/ontology-kernel/src/handlers/properties.rs `extract_operation_config`
// — when the action config is the new envelope shape (carries
// `operation` or `notification_side_effects`), unwrap to the
// `operation` sub-object; otherwise return as-is.
func TestExtractOperationConfig(t *testing.T) {
	// Old shape: returned as-is.
	old := json.RawMessage(`{"property_mappings":[]}`)
	got := extractOperationConfig(old)
	assert.JSONEq(t, `{"property_mappings":[]}`, string(got))

	// Envelope shape with `operation` key — unwrap.
	envelope := json.RawMessage(`{"operation":{"property_mappings":[{"property_name":"x","input_name":"y"}]},"notification_side_effects":[]}`)
	got = extractOperationConfig(envelope)
	assert.JSONEq(t, `{"property_mappings":[{"property_name":"x","input_name":"y"}]}`, string(got))

	// Envelope shape WITHOUT operation but with notification key —
	// returns null.
	notifOnly := json.RawMessage(`{"notification_side_effects":[]}`)
	got = extractOperationConfig(notifOnly)
	assert.Equal(t, "null", strings.TrimSpace(string(got)))
}

// libs/ontology-kernel/src/handlers/properties.rs
// `enforce_inline_edit_action_envelope` — accepts empty / null
// arrays for both side-effect keys; rejects non-empty.
func TestEnforceInlineEditActionEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantErr string
	}{
		{"no envelope", `{}`, ""},
		{"empty arrays ok", `{"notification_side_effects":[],"webhook_side_effects":[]}`, ""},
		{"nulls ok", `{"notification_side_effects":null,"webhook_side_effects":null}`, ""},
		{"notifications populated rejects", `{"notification_side_effects":[{"x":1}]}`, "side-effect notifications"},
		{"webhooks populated rejects", `{"webhook_side_effects":[{"x":1}]}`, "side-effect webhooks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := enforceInlineEditActionEnvelope(json.RawMessage(tc.config))
			if tc.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErr)
			}
		})
	}
}

// libs/ontology-kernel/src/handlers/properties.rs
// `resolve_inline_edit_input_name` — explicit input_name path: if
// configured input_name doesn't appear in the action's mappings,
// reject; if it does, return it.
func TestResolveInlineEditInputNameExplicit(t *testing.T) {
	in1 := "amount"
	in2 := "value"
	action := models.ActionType{
		Config: json.RawMessage(`{"property_mappings":[{"property_name":"price","input_name":"amount"},{"property_name":"price","input_name":"value"}]}`),
	}
	cfg := &models.PropertyInlineEditConfig{InputName: &in1}
	got, err := resolveInlineEditInputName(action, "price", cfg)
	assert.NoError(t, err)
	assert.Equal(t, "amount", got)

	missing := "ghost"
	cfg = &models.PropertyInlineEditConfig{InputName: &missing}
	_, err = resolveInlineEditInputName(action, "price", cfg)
	assert.ErrorContains(t, err, "does not map property 'price' from input 'ghost'")

	// No InputName + 2 unique mappings → ambiguous reject.
	cfg = &models.PropertyInlineEditConfig{}
	_, err = resolveInlineEditInputName(action, "price", cfg)
	assert.ErrorContains(t, err, "multiple input fields")
	_ = in2

	// No mapping entries at all → "must map" reject.
	action.Config = json.RawMessage(`{"property_mappings":[]}`)
	_, err = resolveInlineEditInputName(action, "price", cfg)
	assert.ErrorContains(t, err, "must map property 'price'")

	// Exactly one mapping with no explicit InputName → return it.
	action.Config = json.RawMessage(`{"property_mappings":[{"property_name":"price","input_name":"amount"}]}`)
	got, err = resolveInlineEditInputName(action, "price", cfg)
	assert.NoError(t, err)
	assert.Equal(t, "amount", got)
}

// libs/ontology-kernel/src/handlers/properties.rs
// `isEmptyArrayOrNull` — accepts the two empty shapes the Rust
// source counts as "no side effects configured".
func TestIsEmptyArrayOrNull(t *testing.T) {
	assert.True(t, isEmptyArrayOrNull(json.RawMessage(`null`)))
	assert.True(t, isEmptyArrayOrNull(json.RawMessage(`[]`)))
	assert.True(t, isEmptyArrayOrNull(json.RawMessage(`  []  `)))
	assert.False(t, isEmptyArrayOrNull(json.RawMessage(`[1]`)))
	assert.False(t, isEmptyArrayOrNull(json.RawMessage(`{"a":1}`)))
}
