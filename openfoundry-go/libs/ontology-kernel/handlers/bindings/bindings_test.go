package bindings

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

// libs/ontology-kernel/src/handlers/bindings.rs — every endpoint
// returns 401 when the request carries no Claims.
func TestEndpointsRequireClaims(t *testing.T) {
	state := &ontologykernel.AppState{}
	tid := uuid.New().String()
	bid := uuid.New().String()
	cases := []struct {
		method, path, body string
		fn                 http.HandlerFunc
	}{
		{http.MethodPost, "/ontology/types/" + tid + "/bindings", `{}`, CreateObjectTypeBinding(state)},
		{http.MethodGet, "/ontology/types/" + tid + "/bindings", ``, ListObjectTypeBindings(state)},
		{http.MethodGet, "/ontology/types/" + tid + "/bindings/" + bid, ``, GetObjectTypeBinding(state)},
		{http.MethodPatch, "/ontology/types/" + tid + "/bindings/" + bid, `{}`, UpdateObjectTypeBinding(state)},
		{http.MethodDelete, "/ontology/types/" + tid + "/bindings/" + bid, ``, DeleteObjectTypeBinding(state)},
		{http.MethodPost, "/ontology/types/" + tid + "/bindings/" + bid + "/materialize", `{}`, MaterializeObjectTypeBinding(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

// libs/ontology-kernel/src/handlers/bindings.rs — Mount registers
// every endpoint at the documented path / verb pair.
func TestMountRegistersEveryRoute(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, &ontologykernel.AppState{})

	got := map[string]bool{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	})

	want := []string{
		"POST /ontology/types/{type_id}/bindings",
		"GET /ontology/types/{type_id}/bindings",
		"GET /ontology/types/{type_id}/bindings/{binding_id}",
		"PATCH /ontology/types/{type_id}/bindings/{binding_id}",
		"DELETE /ontology/types/{type_id}/bindings/{binding_id}",
		"POST /ontology/types/{type_id}/bindings/{binding_id}/materialize",
	}
	for _, key := range want {
		assert.True(t, got[key], "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/bindings.rs `validate_marking`
// — accepts the 5 canonical markings, rejects everything else with
// the verbatim Rust error string.
func TestValidateMarking(t *testing.T) {
	for _, ok := range []string{"public", "internal", "confidential", "pii", "restricted"} {
		assert.NoError(t, validateMarking(ok), ok)
	}
	err := validateMarking("topsecret")
	if assert.Error(t, err) {
		assert.Equal(t,
			"marking 'topsecret' is not supported; expected one of: public, internal, confidential, pii, restricted",
			err.Error())
	}
}

// libs/ontology-kernel/src/handlers/bindings.rs `validate_mapping_targets`
// — rejects empty fields and duplicate target_property entries.
func TestValidateMappingTargets(t *testing.T) {
	t.Run("empty source", func(t *testing.T) {
		err := validateMappingTargets([]models.ObjectTypeBindingPropertyMapping{
			{SourceField: "  ", TargetProperty: "name"},
		})
		assert.EqualError(t, err, "property_mapping.source_field cannot be empty")
	})
	t.Run("empty target", func(t *testing.T) {
		err := validateMappingTargets([]models.ObjectTypeBindingPropertyMapping{
			{SourceField: "id_col", TargetProperty: ""},
		})
		assert.EqualError(t, err, "property_mapping.target_property cannot be empty")
	})
	t.Run("duplicate target", func(t *testing.T) {
		err := validateMappingTargets([]models.ObjectTypeBindingPropertyMapping{
			{SourceField: "a", TargetProperty: "name"},
			{SourceField: "b", TargetProperty: "name"},
		})
		assert.EqualError(t, err, "property_mapping.target_property 'name' is duplicated")
	})
	t.Run("happy path", func(t *testing.T) {
		assert.NoError(t, validateMappingTargets([]models.ObjectTypeBindingPropertyMapping{
			{SourceField: "id_col", TargetProperty: "id"},
			{SourceField: "name_col", TargetProperty: "name"},
		}))
	})
}

// libs/ontology-kernel/src/handlers/bindings.rs `project_row`. Empty
// mapping passes the row through; non-empty mapping picks each
// source field and rewrites it to the target property.
func TestProjectRow(t *testing.T) {
	row := json.RawMessage(`{"id_col":"abc","name_col":"alpha","extra":1}`)

	t.Run("empty mapping passes through", func(t *testing.T) {
		out, err := projectRow(row, nil)
		assert.NoError(t, err)
		var got map[string]any
		assert.NoError(t, json.Unmarshal(out, &got))
		assert.Equal(t, "abc", got["id_col"])
		assert.Equal(t, "alpha", got["name_col"])
		assert.Equal(t, float64(1), got["extra"])
	})

	t.Run("mapping picks subset", func(t *testing.T) {
		out, err := projectRow(row, []models.ObjectTypeBindingPropertyMapping{
			{SourceField: "id_col", TargetProperty: "id"},
			{SourceField: "name_col", TargetProperty: "name"},
			{SourceField: "missing_col", TargetProperty: "missing"},
		})
		assert.NoError(t, err)
		var got map[string]any
		assert.NoError(t, json.Unmarshal(out, &got))
		assert.Equal(t, "abc", got["id"])
		assert.Equal(t, "alpha", got["name"])
		_, hasExtra := got["extra"]
		assert.False(t, hasExtra, "extra should be filtered")
		_, hasMissing := got["missing"]
		assert.False(t, hasMissing, "missing source columns are skipped")
	})

	t.Run("non-object row errors", func(t *testing.T) {
		_, err := projectRow(json.RawMessage(`[1,2,3]`), nil)
		assert.EqualError(t, err, "dataset preview row is not a JSON object")
	})
}

// libs/ontology-kernel/src/handlers/bindings.rs `extract_primary_key`
// — must surface a verbatim "row is missing primary key column" if
// the column is absent.
func TestExtractPrimaryKey(t *testing.T) {
	t.Run("string pk", func(t *testing.T) {
		v, err := extractPrimaryKey(json.RawMessage(`{"id":"abc-123","name":"x"}`), "id")
		assert.NoError(t, err)
		assert.Equal(t, "abc-123", v)
	})
	t.Run("missing pk column", func(t *testing.T) {
		_, err := extractPrimaryKey(json.RawMessage(`{"name":"x"}`), "id")
		assert.EqualError(t, err, "row is missing primary key column 'id'")
	})
	t.Run("non-object row", func(t *testing.T) {
		_, err := extractPrimaryKey(json.RawMessage(`[]`), "id")
		assert.EqualError(t, err, "row is not an object")
	})
}

// libs/ontology-kernel/src/handlers/bindings.rs `clamp_int32` —
// clamps to [lo, hi] inclusive; pinned in case the helper is reused.
func TestClampInt32(t *testing.T) {
	assert.Equal(t, int32(1), clampInt32(0, 1, 100))
	assert.Equal(t, int32(100), clampInt32(500, 1, 100))
	assert.Equal(t, int32(50), clampInt32(50, 1, 100))
}

// libs/ontology-kernel/src/handlers/bindings.rs — invalid path
// type_id rejects with 400 + verbatim error.
func TestInvalidTypeIDRejected(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	r.Post("/ontology/types/{type_id}/bindings", CreateObjectTypeBinding(state))

	req := httptest.NewRequest(http.MethodPost, "/ontology/types/not-a-uuid/bindings", strings.NewReader("{}"))
	ctx := authmw.ContextWithClaims(req.Context(), &authmw.Claims{Sub: uuid.Nil, Email: "test@example.com"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req.WithContext(ctx))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid type_id")
}
