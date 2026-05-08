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
	for _, k := range []string{"user_id", "resource_kind", "resource_id", "created_at"} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "dataset", view["resource_kind"])
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
