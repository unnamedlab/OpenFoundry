package sharedproperties

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// libs/ontology-kernel/src/handlers/shared_properties.rs — every
// endpoint returns 401 when the request carries no Claims.
func TestEndpointsRequireClaims(t *testing.T) {
	state := &ontologykernel.AppState{}
	tid := uuid.New().String()
	sid := uuid.New().String()
	cases := []struct {
		method string
		path   string
		body   string
		fn     http.HandlerFunc
	}{
		{http.MethodGet, "/ontology/shared-property-types", ``, ListSharedPropertyTypes(state)},
		{http.MethodPost, "/ontology/shared-property-types", `{"name":"x","property_type":"string"}`, CreateSharedPropertyType(state)},
		{http.MethodGet, "/ontology/shared-property-types/" + sid, ``, GetSharedPropertyType(state)},
		{http.MethodPatch, "/ontology/shared-property-types/" + sid, `{}`, UpdateSharedPropertyType(state)},
		{http.MethodDelete, "/ontology/shared-property-types/" + sid, ``, DeleteSharedPropertyType(state)},
		{http.MethodPost, "/ontology/types/" + tid + "/shared-property-types/" + sid, ``, AttachSharedPropertyType(state)},
		{http.MethodGet, "/ontology/types/" + tid + "/shared-property-types", ``, ListTypeSharedPropertyTypes(state)},
		{http.MethodDelete, "/ontology/types/" + tid + "/shared-property-types/" + sid, ``, DetachSharedPropertyType(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code,
			"%s %s should be 401", tc.method, tc.path)
	}
}

// libs/ontology-kernel/src/handlers/shared_properties.rs — Mount
// registers every endpoint at the documented path / verb pair.
func TestMountRegistersEveryRoute(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	Mount(r, state)

	routes := map[string]bool{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+route] = true
		return nil
	})

	want := []string{
		"GET /ontology/shared-property-types",
		"POST /ontology/shared-property-types",
		"GET /ontology/shared-property-types/{id}",
		"PATCH /ontology/shared-property-types/{id}",
		"DELETE /ontology/shared-property-types/{id}",
		"POST /ontology/types/{type_id}/shared-property-types/{shared_id}",
		"GET /ontology/types/{type_id}/shared-property-types",
		"DELETE /ontology/types/{type_id}/shared-property-types/{shared_id}",
	}
	for _, key := range want {
		assert.True(t, routes[key], "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/shared_properties.rs
// `create_shared_property_type` — empty name rejects with 400 and
// the verbatim Rust string.
func TestCreateRejectsEmptyName(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateSharedPropertyTypeRequest{
		Name:         "  ",
		PropertyType: "string",
	})
	req := httptest.NewRequest(http.MethodPost, "/ontology/shared-property-types", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), CreateSharedPropertyType(state)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "shared property type name is required")
}

// libs/ontology-kernel/src/handlers/shared_properties.rs
// `create_shared_property_type` — unknown property_type rejects
// with 400 and the verbatim type-system error string.
func TestCreateRejectsUnknownPropertyType(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateSharedPropertyTypeRequest{
		Name:         "rating",
		PropertyType: "magic",
	})
	req := httptest.NewRequest(http.MethodPost, "/ontology/shared-property-types", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), CreateSharedPropertyType(state)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid property type")
}

// libs/ontology-kernel/src/handlers/shared_properties.rs
// `create_shared_property_type` — default_value mismatch rejects
// with 400 and the type-system error verbatim.
func TestCreateRejectsBadDefaultValue(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateSharedPropertyTypeRequest{
		Name:         "score",
		PropertyType: "integer",
		DefaultValue: json.RawMessage(`"not-an-integer"`),
	})
	req := httptest.NewRequest(http.MethodPost, "/ontology/shared-property-types", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), CreateSharedPropertyType(state)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "expected integer value")
}

// libs/ontology-kernel/src/handlers/shared_properties.rs — invalid
// JSON body in Update rejects with 400.
func TestUpdateRejectsInvalidBody(t *testing.T) {
	r := chi.NewRouter()
	r.Patch("/ontology/shared-property-types/{id}", UpdateSharedPropertyType(&ontologykernel.AppState{}))
	req := httptest.NewRequest(http.MethodPatch,
		"/ontology/shared-property-types/"+uuid.New().String(),
		strings.NewReader(`{garbage}`))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
}

// libs/ontology-kernel/src/handlers/shared_properties.rs — path
// helpers reject malformed UUIDs with 400 + verbatim message.
func TestPathHelpersRejectMalformedUUIDs(t *testing.T) {
	r := chi.NewRouter()
	state := &ontologykernel.AppState{}
	r.Post("/ontology/types/{type_id}/shared-property-types/{shared_id}", AttachSharedPropertyType(state))

	req := httptest.NewRequest(http.MethodPost,
		"/ontology/types/not-a-uuid/shared-property-types/"+uuid.New().String(),
		nil)
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid type_id")
}

// Round-trip wire shape — ListSharedPropertyTypesResponse marshals
// as the Rust struct does.
func TestListSharedPropertyTypesResponseShape(t *testing.T) {
	resp := models.ListSharedPropertyTypesResponse{
		Data:    []models.SharedPropertyType{},
		Total:   0,
		Page:    1,
		PerPage: 50,
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Equal(t, `{"data":[],"total":0,"page":1,"per_page":50}`, string(b))
}
