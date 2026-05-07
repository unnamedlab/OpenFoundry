package workspace_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

// ─── Wire-format pinning ────────────────────────────────────────────

func TestAccessLevelEnum(t *testing.T) {
	t.Parallel()
	assert.True(t, workspace.AccessViewer.IsValid())
	assert.True(t, workspace.AccessEditor.IsValid())
	assert.True(t, workspace.AccessOwner.IsValid())
	assert.False(t, workspace.AccessLevel("admin").IsValid())
	assert.Equal(t, "viewer", string(workspace.AccessViewer))
	assert.Equal(t, "editor", string(workspace.AccessEditor))
	assert.Equal(t, "owner", string(workspace.AccessOwner))
}

func TestResourceShareJSONShape(t *testing.T) {
	t.Parallel()
	uid := uuid.New()
	exp := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	s := workspace.ResourceShare{
		ID: uuid.New(), ResourceKind: workspace.ResourceDataset, ResourceID: uuid.New(),
		SharedWithUserID: &uid, SharedWithGroupID: nil,
		SharerID:    uuid.New(),
		AccessLevel: workspace.AccessViewer, Note: "ok", ExpiresAt: &exp,
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(s)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "resource_kind", "resource_id", "shared_with_user_id",
		"shared_with_group_id", "sharer_id", "access_level", "note",
		"expires_at", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "viewer", view["access_level"])
	assert.Equal(t, "dataset", view["resource_kind"])
}

// ─── Validation paths ────────────────────────────────────────────────

// chiCtxFor returns a request context with chi URLParams populated for
// the `kind` and `id` keys, so handlers reading chi.URLParam find them.
func chiCtxFor(req *httptest.ResponseRecorder, kind, id string) *chi.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", kind)
	rctx.URLParams.Add("id", id)
	return rctx
}

func TestCreateShareRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("POST", "/workspace/resources/dataset/"+uuid.New().String()+"/share",
		strings.NewReader(`{"access_level":"viewer","shared_with_user_id":"`+uuid.New().String()+`"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.CreateShare(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateShareRejectsBothPrincipals(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	body := `{"access_level":"viewer","shared_with_user_id":"` + uuid.New().String() + `","shared_with_group_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest("POST", "/workspace/resources/dataset/X/share", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.CreateShare(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "exactly one")
}

func TestCreateShareRejectsNoPrincipal(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/workspace/resources/dataset/X/share",
		strings.NewReader(`{"access_level":"viewer"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.CreateShare(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestCreateShareRejectsBadAccessLevel(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	body := `{"access_level":"god","shared_with_user_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest("POST", "/workspace/resources/dataset/X/share", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.CreateShare(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "access_level")
}

func TestCreateShareRejectsBadKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	body := `{"access_level":"viewer","shared_with_user_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest("POST", "/workspace/resources/banana/X/share", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "banana")
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.CreateShare(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestRevokeShareRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("DELETE", "/workspace/shares/"+uuid.New().String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.New().String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.RevokeShare(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestListSharedWithMeRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("GET", "/workspace/shared-with-me", nil)
	rec := httptest.NewRecorder()
	h.ListSharedWithMe(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestListSharedByMeRejectsBadKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("GET", "/workspace/shared-by-me?kind=banana", nil)
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.ListSharedByMe(rec, req)
	assert.Equal(t, 400, rec.Code)
}
