package search

import (
	"context"
	"encoding/json"
	"errors"
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

// withClaims wraps a handler so the test can inject Claims via the
// authmw context. Mirrors what the real auth middleware does.
func withClaims(claims *authmw.Claims, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := authmw.ContextWithClaims(r.Context(), claims)
		handler.ServeHTTP(w, r.WithContext(ctx))
	})
}

// libs/ontology-kernel/src/handlers/search.rs `search_ontology`
// rejects an empty query body with 400 + `query is required`.
func TestSearchOntologyRejectsEmptyQuery(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.SearchRequest{Query: "   "})
	req := httptest.NewRequest(http.MethodPost, "/ontology/search", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), SearchOntology(state, nil)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "query is required")
}

// libs/ontology-kernel/src/handlers/search.rs — missing claims
// surface 401 with `missing claims`.
func TestSearchOntologyMissingClaimsUnauthorized(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.SearchRequest{Query: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/ontology/search", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	SearchOntology(state, nil).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing claims")
}

// libs/ontology-kernel/src/handlers/search.rs `search_objects_fulltext`
// — empty `q` parameter rejects with 400.
func TestSearchObjectsFulltextRejectsEmptyQ(t *testing.T) {
	state := &ontologykernel.AppState{}
	req := httptest.NewRequest(http.MethodGet, "/ontology/search?q=%20%20", nil)
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), SearchObjectsFulltext(state, nil)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "q is required")
}

// libs/ontology-kernel/src/handlers/search.rs `get_quiver_vega_spec`
// — preview endpoint that builds the Vega spec without persisting.
// A valid request body returns 200 + a spec payload.
func TestGetQuiverVegaSpecValidBody(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateQuiverVisualFunctionRequest{
		Name:          "Daily orders",
		PrimaryTypeID: uuid.New(),
		JoinField:     "order_id",
		DateField:     "event_date",
		MetricField:   "gmv",
		GroupField:    "region",
	})
	req := httptest.NewRequest(http.MethodPost, "/ontology/quiver/vega-spec", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	GetQuiverVegaSpec(state).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp, "spec")
	assert.True(t, strings.HasPrefix(string(resp["spec"]), `{`), "spec should be a JSON object")
}

// libs/ontology-kernel/src/handlers/search.rs `get_quiver_vega_spec`
// — empty name (or any required field) rejects with 400.
func TestGetQuiverVegaSpecRejectsMissingFields(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateQuiverVisualFunctionRequest{
		Name: "  ",
	})
	req := httptest.NewRequest(http.MethodPost, "/ontology/quiver/vega-spec", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	GetQuiverVegaSpec(state).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "is required")
}

// libs/ontology-kernel/src/handlers/search.rs — `apply_quiver_update`
// overrides selected fields and clears the SelectedGroup when the
// PATCH body carries `Some(None)` (Rust `Option<Option<String>>` →
// Go *StringUpdate{Value: nil}).
func TestApplyQuiverUpdateOverridesAndClears(t *testing.T) {
	current := sampleQuiverRecord()
	override := "Executive Orders"
	clearGroup := &models.StringUpdate{Value: nil}
	areaKind := "area"
	sharedTrue := true
	metric := "net_revenue"

	draft := applyQuiverUpdate(current, models.UpdateQuiverVisualFunctionRequest{
		Name:          &override,
		MetricField:   &metric,
		ChartKind:     &areaKind,
		SelectedGroup: clearGroup,
		Shared:        &sharedTrue,
	})
	assert.Equal(t, "Executive Orders", draft.Name)
	assert.Equal(t, "net_revenue", draft.MetricField)
	assert.Equal(t, "area", draft.ChartKind)
	assert.Nil(t, draft.SelectedGroup, "Some(None) should clear the group")
	assert.True(t, draft.Shared)
	assert.Equal(t, current.GroupField, draft.GroupField, "untouched fields keep their value")
}

// libs/ontology-kernel/src/handlers/search.rs — Mount registers
// every endpoint at the documented path / verb pair.
func TestMountRegistersEveryRoute(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	Mount(r, state, nil)

	routes := map[string]string{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+route] = ""
		return nil
	})

	want := []string{
		"POST /ontology/search",
		"GET /ontology/search",
		"GET /ontology/graph",
		"GET /ontology/quiver",
		"POST /ontology/quiver",
		"GET /ontology/quiver/{id}",
		"PATCH /ontology/quiver/{id}",
		"DELETE /ontology/quiver/{id}",
		"POST /ontology/quiver/vega-spec",
	}
	for _, key := range want {
		assert.Contains(t, routes, key, "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/search.rs `parseGraphQuery`
// — pulls the four Rust fields out of the query string and parses
// the UUIDs verbatim.
func TestParseGraphQueryFields(t *testing.T) {
	root := uuid.New()
	rootType := uuid.New()
	r := httptest.NewRequest(http.MethodGet,
		"/ontology/graph?root_object_id="+root.String()+
			"&root_type_id="+rootType.String()+
			"&depth=3&limit=50",
		nil)
	got := parseGraphQuery(r)
	require.NotNil(t, got.RootObjectID)
	assert.Equal(t, root, *got.RootObjectID)
	require.NotNil(t, got.RootTypeID)
	assert.Equal(t, rootType, *got.RootTypeID)
	require.NotNil(t, got.Depth)
	assert.Equal(t, 3, *got.Depth)
	require.NotNil(t, got.Limit)
	assert.Equal(t, 50, *got.Limit)
}

// libs/ontology-kernel/src/handlers/search.rs `parseGraphQuery`
// — empty / missing parameters surface as nil.
func TestParseGraphQueryEmpty(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/ontology/graph", nil)
	got := parseGraphQuery(r)
	assert.Nil(t, got.RootObjectID)
	assert.Nil(t, got.RootTypeID)
	assert.Nil(t, got.Depth)
	assert.Nil(t, got.Limit)
}

// libs/ontology-kernel/src/handlers/search.rs — validateVisualFunctionDraft
// rejects every required field and propagates chart-kind errors.
func TestValidateVisualFunctionDraft(t *testing.T) {
	mk := func(d models.QuiverVisualFunctionDraft) error {
		return validateVisualFunctionDraft(d)
	}
	base := models.QuiverVisualFunctionDraft{
		Name:        "x",
		JoinField:   "j",
		DateField:   "d",
		MetricField: "m",
		GroupField:  "g",
		ChartKind:   "line",
	}
	require.NoError(t, mk(base))

	withName := base
	withName.Name = "  "
	assert.ErrorContains(t, mk(withName), "name is required")

	withJoin := base
	withJoin.JoinField = "  "
	assert.ErrorContains(t, mk(withJoin), "join_field is required")

	withChart := base
	withChart.ChartKind = "heatmap"
	assert.ErrorContains(t, mk(withChart), "chart_kind")
}

func sampleQuiverRecord() models.QuiverVisualFunction {
	pid := uuid.New()
	sec := uuid.New()
	group := "EMEA"
	return models.QuiverVisualFunction{
		ID:                  uuid.New(),
		Name:                "Daily Orders",
		Description:         "Orders by region",
		PrimaryTypeID:       pid,
		SecondaryTypeID:     &sec,
		JoinField:           "order_id",
		SecondaryJoinField:  "order_id",
		DateField:           "event_date",
		MetricField:         "gmv",
		GroupField:          "region",
		SelectedGroup:       &group,
		ChartKind:           "line",
		Shared:              false,
		VegaSpec:            json.RawMessage(`{}`),
		OwnerID:             uuid.Nil,
	}
}

// Compile-time assertion: ensure the package's helpers are reachable
// from the test file even when the linter prunes the imports.
var _ = context.Background
var _ = errors.New
