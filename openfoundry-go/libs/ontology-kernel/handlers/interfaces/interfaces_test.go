package interfaces

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

// libs/ontology-kernel/src/handlers/interfaces.rs — every endpoint
// returns 401 when the request carries no Claims.
func TestEndpointsRequireClaims(t *testing.T) {
	state := &ontologykernel.AppState{}
	tid := uuid.New().String()
	iid := uuid.New().String()
	pid := uuid.New().String()
	cases := []struct {
		method, path, body string
		fn                 http.HandlerFunc
	}{
		{http.MethodPost, "/ontology/interfaces", `{"name":"x"}`, CreateInterface(state)},
		{http.MethodGet, "/ontology/interfaces", ``, ListInterfaces(state)},
		{http.MethodGet, "/ontology/interfaces/" + iid, ``, GetInterface(state)},
		{http.MethodPatch, "/ontology/interfaces/" + iid, `{}`, UpdateInterface(state)},
		{http.MethodDelete, "/ontology/interfaces/" + iid, ``, DeleteInterface(state)},
		{http.MethodGet, "/ontology/interfaces/" + iid + "/properties", ``, ListInterfaceProperties(state)},
		{http.MethodPost, "/ontology/interfaces/" + iid + "/properties", `{}`, CreateInterfaceProperty(state)},
		{http.MethodPatch, "/ontology/interfaces/" + iid + "/properties/" + pid, `{}`, UpdateInterfaceProperty(state)},
		{http.MethodDelete, "/ontology/interfaces/" + iid + "/properties/" + pid, ``, DeleteInterfaceProperty(state)},
		{http.MethodPost, "/ontology/types/" + tid + "/interfaces/" + iid, ``, AttachInterface(state)},
		{http.MethodGet, "/ontology/types/" + tid + "/interfaces", ``, ListTypeInterfaces(state)},
		{http.MethodDelete, "/ontology/types/" + tid + "/interfaces/" + iid, ``, DetachInterface(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

// libs/ontology-kernel/src/handlers/interfaces.rs — Mount registers
// every documented route at the documented path / verb.
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
		"POST /ontology/interfaces",
		"GET /ontology/interfaces",
		"GET /ontology/interfaces/{id}",
		"PATCH /ontology/interfaces/{id}",
		"DELETE /ontology/interfaces/{id}",
		"GET /ontology/interfaces/{id}/properties",
		"POST /ontology/interfaces/{id}/properties",
		"PATCH /ontology/interfaces/{id}/properties/{property_id}",
		"DELETE /ontology/interfaces/{id}/properties/{property_id}",
		"POST /ontology/types/{type_id}/interfaces/{interface_id}",
		"GET /ontology/types/{type_id}/interfaces",
		"DELETE /ontology/types/{type_id}/interfaces/{interface_id}",
	}
	for _, key := range want {
		assert.True(t, got[key], "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/interfaces.rs `create_interface`
// — empty name rejects with 400 verbatim.
func TestCreateInterfaceRejectsEmptyName(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateInterfaceRequest{Name: "  "})
	req := httptest.NewRequest(http.MethodPost, "/ontology/interfaces", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), CreateInterface(state)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "interface name is required")
}

// libs/ontology-kernel/src/handlers/interfaces.rs — invalid UUIDs
// in path parameters reject with 400 + verbatim message.
func TestPathHelpersRejectMalformedUUIDs(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	r.Post("/ontology/types/{type_id}/interfaces/{interface_id}", AttachInterface(state))

	req := httptest.NewRequest(http.MethodPost, "/ontology/types/not-a-uuid/interfaces/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid type_id")
}

// libs/ontology-kernel/src/handlers/interfaces.rs — pagination
// helper clamps per_page to [1, 100] and defaults page to 1.
func TestParsePaginationClamps(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ontology/interfaces?page=0&per_page=500&search=alpha", nil)
	page, perPage, search := parsePagination(r)
	assert.Equal(t, int64(1), page)
	assert.Equal(t, int64(100), perPage)
	assert.Equal(t, "alpha", search)

	r = httptest.NewRequest(http.MethodGet, "/ontology/interfaces?per_page=-5", nil)
	_, perPage, _ = parsePagination(r)
	assert.Equal(t, int64(1), perPage)
}

// Round-trip wire shape for ListInterfacesResponse mirrors Rust.
func TestListInterfacesResponseShape(t *testing.T) {
	resp := models.ListInterfacesResponse{
		Data:    []models.OntologyInterface{},
		Total:   0,
		Page:    1,
		PerPage: 50,
	}
	b, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Equal(t, `{"data":[],"total":0,"page":1,"per_page":50}`, string(b))
}
