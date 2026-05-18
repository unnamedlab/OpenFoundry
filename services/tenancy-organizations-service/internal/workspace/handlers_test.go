package workspace_test

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
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

// ─── ResourceKind parsing ────────────────────────────────────────────

func TestParseResourceKindLegacyAliases(t *testing.T) {
	t.Parallel()
	cases := map[string]workspace.ResourceKind{
		"project":                   workspace.ResourceOntologyProject,
		"ontology_project":          workspace.ResourceOntologyProject,
		"folder":                    workspace.ResourceOntologyFolder,
		"ontology_folder":           workspace.ResourceOntologyFolder,
		"resource_binding":          workspace.ResourceOntologyResourceBinding,
		"ontology_resource_binding": workspace.ResourceOntologyResourceBinding,
		"dataset":                   workspace.ResourceDataset,
		"pipeline":                  workspace.ResourcePipeline,
		"query":                     workspace.ResourceQuery,
		"notebook":                  workspace.ResourceNotebook,
		"app":                       workspace.ResourceApp,
		"dashboard":                 workspace.ResourceDashboard,
		"report":                    workspace.ResourceReport,
		"model":                     workspace.ResourceModel,
		"workflow":                  workspace.ResourceWorkflow,
		"other":                     workspace.ResourceOther,
	}
	for raw, want := range cases {
		got, err := workspace.ParseResourceKind(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
}

func TestParseResourceKindRejectsUnknown(t *testing.T) {
	t.Parallel()
	_, err := workspace.ParseResourceKind("banana")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "banana")
	assert.Contains(t, err.Error(), "ontology_project")
}

// ─── Wire-format pinning ─────────────────────────────────────────────

func TestUserFavoriteJSONShape(t *testing.T) {
	t.Parallel()
	f := workspace.UserFavorite{
		UserID: uuid.New(), ResourceKind: workspace.ResourceDataset,
		ResourceID: uuid.New(),
		CreatedAt:  time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(f)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"user_id", "resource_kind", "resource_id", "group_id", "display_order", "created_at", "updated_at"} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "dataset", view["resource_kind"])
	assert.Nil(t, view["group_id"])
	assert.Equal(t, float64(0), view["display_order"])
}

func TestFavoriteGroupJSONShape(t *testing.T) {
	t.Parallel()
	g := workspace.FavoriteGroup{
		ID: uuid.New(), UserID: uuid.New(), Name: "Daily ops", DisplayOrder: 1000,
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(g)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"id", "user_id", "name", "display_order", "created_at", "updated_at"} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "Daily ops", view["name"])
	assert.Equal(t, float64(1000), view["display_order"])
}

func TestRecentEntryJSONShape(t *testing.T) {
	t.Parallel()
	e := workspace.RecentEntry{
		ResourceKind: workspace.ResourceNotebook, ResourceID: uuid.New(),
		LastAccessedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(e)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"resource_kind", "resource_id", "last_accessed_at"} {
		assert.Contains(t, view, k)
	}
}

func TestListEnvelopeIsData(t *testing.T) {
	t.Parallel()
	out, err := json.Marshal(workspace.ListFavoritesResponse{Data: []workspace.UserFavorite{}})
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.Contains(t, view, "data", "workspace surface uses {data:[...]} (NOT {items}) — pinned for wire-compat with Rust impl")
	assert.Contains(t, view, "groups", "CMP.16 adds profile groups without replacing the existing data envelope")
	assert.NotContains(t, view, "items")
}

// ─── Handler validation paths (no DB) ────────────────────────────────

func makeReqWithClaims(method, path, body string) (*httptest.ResponseRecorder, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	return rec, rec
}

func TestCreateFavoriteRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/workspace/favorites",
		strings.NewReader(`{"resource_kind":"banana","resource_id":"`+uuid.New().String()+`"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateFavorite(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "banana")
}

func TestCreateFavoriteRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("POST", "/workspace/favorites",
		strings.NewReader(`{"resource_kind":"dataset","resource_id":"`+uuid.New().String()+`"}`))
	rec := httptest.NewRecorder()
	h.CreateFavorite(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateFavoriteRejectsNilResourceID(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/workspace/favorites",
		strings.NewReader(`{"resource_kind":"dataset","resource_id":"00000000-0000-0000-0000-000000000000"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateFavorite(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_id")
}

func TestCreateFavoriteGroupRequiresName(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/workspace/favorites/groups",
		strings.NewReader(`{"name":"   "}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateFavoriteGroup(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "name required")
}

func TestUpdateFavoriteOrderRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("PUT", "/workspace/favorites/order",
		strings.NewReader(`{"items":[{"resource_kind":"banana","resource_id":"`+uuid.New().String()+`","display_order":1000}]}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.UpdateFavoriteOrder(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "banana")
}

func TestUpdateFavoriteGroupsOrderRejectsNilID(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("PUT", "/workspace/favorites/groups/order",
		strings.NewReader(`{"groups":[{"id":"00000000-0000-0000-0000-000000000000","display_order":1000}]}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.UpdateFavoriteGroupsOrder(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "group id")
}

func TestRecordAccessRejectsUnknownKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/workspace/recents",
		strings.NewReader(`{"resource_kind":"banana","resource_id":"`+uuid.New().String()+`"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.RecordAccess(rec, req)
	assert.Equal(t, 400, rec.Code)
}
