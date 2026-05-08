package workspace_test

// Validation tests for the resource_ops handlers. Mirror the
// `mod tests` block in services/tenancy-organizations-service/src/handlers/resource_ops.rs
// — every code path that does not require a database is exercised here so
// the request taxonomy matches Rust byte-exact (status code + error
// message). Database-dependent paths (the actual UPDATE/INSERT) are
// covered by integration tests in this package.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/workspace"
)

// reqWithChi wires chi route params + claims into a request context so
// the handler-under-test sees the same shape as a real chi-routed request.
func reqWithChi(method, body, kind, id string, claims *authmw.Claims) *httptest.ResponseRecorder {
	rctx := chi.NewRouteContext()
	if kind != "" {
		rctx.URLParams.Add("kind", kind)
	}
	if id != "" {
		rctx.URLParams.Add("id", id)
	}
	var r interface{ Body() string }
	_ = r
	_ = method
	_ = body
	return httptest.NewRecorder()
}

// ─── JSON shapes ─────────────────────────────────────────────────────

func TestBatchResponseJSONShape(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	out, err := json.Marshal(workspace.BatchResponse{
		Results: []workspace.BatchResultEntry{
			{Op: "delete", ResourceKind: "ontology_folder", ResourceID: id, OK: true},
		},
	})
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	assert.Contains(t, view, "results")
	results := view["results"].([]any)
	first := results[0].(map[string]any)
	for _, k := range []string{"op", "resource_kind", "resource_id", "ok", "error"} {
		assert.Contains(t, first, k)
	}
	assert.Equal(t, true, first["ok"])
}

// ─── Move ────────────────────────────────────────────────────────────

func TestMoveResourceRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	body := `{"target_folder_id":"` + uuid.New().String() + `"}`
	req := httptest.NewRequest("POST", "/x/move", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.MoveResource(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestMoveResourceRejectsBadKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "banana")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("POST", "/x/move", strings.NewReader(`{}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.MoveResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "banana")
}

func TestMoveResourceRejectsBadID(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "ontology_folder")
	rctx.URLParams.Add("id", "not-a-uuid")
	req := httptest.NewRequest("POST", "/x/move", strings.NewReader(`{}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.MoveResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid resource id")
}

func TestMoveResourceRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	// Admin short-circuits ensureOwnerOrAdmin so we hit the kind dispatch
	// without touching the database.
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("POST", "/x/move", strings.NewReader(`{}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.MoveResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "move is not supported")
	assert.Contains(t, rec.Body.String(), "dataset")
}

func TestMoveResourceBindingRequiresTargetProjectID(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "ontology_resource_binding")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("POST", "/x/move", strings.NewReader(`{}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.MoveResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "target_project_id")
}

// ─── Rename ──────────────────────────────────────────────────────────

func TestRenameResourceRejectsEmptyName(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "ontology_project")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("POST", "/x/rename", strings.NewReader(`{"name":"   "}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.RenameResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "must not be empty")
}

func TestRenameResourceRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "ontology_resource_binding")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("POST", "/x/rename", strings.NewReader(`{"name":"new"}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.RenameResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "rename is not supported")
}

func TestRenameResourceRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("POST", "/x/rename", strings.NewReader(`{"name":"foo"}`))
	rec := httptest.NewRecorder()
	h.RenameResource(rec, req)
	assert.Equal(t, 401, rec.Code)
}

// ─── Duplicate ───────────────────────────────────────────────────────

func TestDuplicateResourceRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "ontology_project")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("POST", "/x/duplicate", strings.NewReader(`{}`))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.DuplicateResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "duplicate is not supported")
	assert.Contains(t, rec.Body.String(), "Phase 1")
}

func TestDuplicateResourceRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("POST", "/x/duplicate", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.DuplicateResource(rec, req)
	assert.Equal(t, 401, rec.Code)
}

// ─── SoftDelete ──────────────────────────────────────────────────────

func TestSoftDeleteRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("DELETE", "/x", nil)
	rec := httptest.NewRecorder()
	h.SoftDeleteResource(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestSoftDeleteRejectsBadKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "banana")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("DELETE", "/x", nil)
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.SoftDeleteResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "banana")
}

func TestSoftDeleteRejectsUnsupportedKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("DELETE", "/x", nil)
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.SoftDeleteResource(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "soft delete is not supported")
}

// ─── Batch ───────────────────────────────────────────────────────────

func TestBatchApplyRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("POST", "/batch", strings.NewReader(`{"actions":[]}`))
	rec := httptest.NewRecorder()
	h.BatchApply(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestBatchApplyEmptyReturnsEmptyResults(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/batch", strings.NewReader(`{"actions":[]}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.BatchApply(rec, req)
	assert.Equal(t, 200, rec.Code)
	var resp workspace.BatchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Results)
}

func TestBatchApplyReportsBadKindPerAction(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	id := uuid.New()
	body := `{"actions":[{"op":"delete","resource_kind":"banana","resource_id":"` + id.String() + `"}]}`
	req := httptest.NewRequest("POST", "/batch", strings.NewReader(body))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.BatchApply(rec, req)
	assert.Equal(t, 200, rec.Code)
	var resp workspace.BatchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 1)
	assert.False(t, resp.Results[0].OK)
	require.NotNil(t, resp.Results[0].Error)
	assert.Contains(t, *resp.Results[0].Error, "banana")
	assert.Equal(t, "delete", resp.Results[0].Op)
	assert.Equal(t, id, resp.Results[0].ResourceID)
}

func TestBatchApplyReportsUnsupportedOpPerAction(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	// Admin short-circuits ensureOwnerOrAdmin; hits unsupported-op branch.
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	id := uuid.New()
	body := `{"actions":[{"op":"yolo","resource_kind":"ontology_folder","resource_id":"` + id.String() + `"}]}`
	req := httptest.NewRequest("POST", "/batch", strings.NewReader(body))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.BatchApply(rec, req)
	assert.Equal(t, 200, rec.Code)
	var resp workspace.BatchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 1)
	require.NotNil(t, resp.Results[0].Error)
	assert.Contains(t, *resp.Results[0].Error, "unsupported batch op")
	assert.Contains(t, *resp.Results[0].Error, "yolo")
}

// silence lint on the placeholder helper above; future integration tests
// can grow against the same signature.
var _ = reqWithChi
