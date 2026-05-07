package types

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

// libs/ontology-kernel/src/handlers/types.rs — every endpoint
// returns 401 when the request carries no Claims.
func TestEndpointsRequireClaims(t *testing.T) {
	state := &ontologykernel.AppState{}
	cases := []struct {
		method string
		path   string
		body   string
		fn     http.HandlerFunc
	}{
		{http.MethodPost, "/ontology/types", `{"name":"x"}`, CreateObjectType(state)},
		{http.MethodGet, "/ontology/types", ``, ListObjectTypes(state)},
		{http.MethodGet, "/ontology/types/" + uuid.New().String(), ``, GetObjectType(state)},
		{http.MethodPatch, "/ontology/types/" + uuid.New().String(), `{}`, UpdateObjectType(state)},
		{http.MethodDelete, "/ontology/types/" + uuid.New().String(), ``, DeleteObjectType(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code,
			"%s %s should be 401", tc.method, tc.path)
		assert.Contains(t, rec.Body.String(), "missing claims")
	}
}

// libs/ontology-kernel/src/handlers/types.rs — Mount registers
// every endpoint at the documented path / verb pair.
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
		"POST /ontology/types",
		"GET /ontology/types",
		"GET /ontology/types/{id}",
		"PATCH /ontology/types/{id}",
		"DELETE /ontology/types/{id}",
	}
	for _, key := range want {
		assert.True(t, routes[key], "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/types.rs `create_object_type`
// — invalid JSON body surfaces 400.
func TestCreateObjectTypeRejectsInvalidBody(t *testing.T) {
	state := &ontologykernel.AppState{}
	req := httptest.NewRequest(http.MethodPost, "/ontology/types", strings.NewReader(`{not json}`))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), CreateObjectType(state)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid request body")
}

// libs/ontology-kernel/src/handlers/types.rs — pathUUID rejects
// malformed path values with 400.
func TestPathUUIDRejectsMalformed(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/ontology/types/{id}", GetObjectType(&ontologykernel.AppState{}))

	req := httptest.NewRequest(http.MethodGet, "/ontology/types/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid path id")
}

// Round-trip the wire shape: ListObjectTypesResponse marshals as
// the Rust `struct ListObjectTypesResponse` does.
func TestListObjectTypesResponseShape(t *testing.T) {
	resp := models.ListObjectTypesResponse{
		Data:    []models.ObjectType{},
		Total:   0,
		Page:    1,
		PerPage: 50,
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Equal(t, `{"data":[],"total":0,"page":1,"per_page":50}`, string(b))
}
