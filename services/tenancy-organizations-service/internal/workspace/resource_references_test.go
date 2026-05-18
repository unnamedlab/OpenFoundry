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

func TestResourceReferenceGraphJSONShape(t *testing.T) {
	t.Parallel()
	sourceID := uuid.New()
	targetID := uuid.New()
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	graph := workspace.ResourceReferenceGraphResponse{
		ResourceKind: "dashboard",
		ResourceID:   sourceID,
		ResourceRID:  "ri.foundry.main.dashboard." + sourceID.String(),
		DependsOn: []workspace.ResourceReferenceEdge{{
			Source: workspace.ResourceReferenceNode{
				ResourceKind: "dashboard",
				ResourceID:   sourceID,
				ResourceRID:  "ri.foundry.main.dashboard." + sourceID.String(),
				DisplayName:  "Executive dashboard",
			},
			Target: workspace.ResourceReferenceNode{
				ResourceKind: "query",
				ResourceID:   targetID,
				ResourceRID:  "ri.foundry.main.query." + targetID.String(),
				DisplayName:  "Revenue query",
			},
			Relationship: "reads",
			CreatedAt:    now,
			UpdatedAt:    now,
			Derived:      false,
		}},
		UsedBy: []workspace.ResourceReferenceEdge{},
	}

	out, err := json.Marshal(graph)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{"resource_kind", "resource_id", "resource_rid", "depends_on", "used_by"} {
		assert.Contains(t, view, key)
	}
	dependsOn := view["depends_on"].([]any)
	first := dependsOn[0].(map[string]any)
	for _, key := range []string{"source", "target", "relationship", "created_at", "updated_at", "derived"} {
		assert.Contains(t, first, key)
	}
	assert.Equal(t, "reads", first["relationship"])
}

func TestGetResourceReferencesRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	req := httptest.NewRequest("GET", "/workspace/resources/dataset/"+uuid.New().String()+"/references", nil)
	rec := httptest.NewRecorder()
	h.GetResourceReferences(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestGetResourceReferencesRejectsBadKind(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "banana")
	rctx.URLParams.Add("id", uuid.New().String())
	req := httptest.NewRequest("GET", "/workspace/resources/banana/x/references", nil)
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.GetResourceReferences(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "banana")
}

func TestReplaceResourceReferencesRejectsSelfReference(t *testing.T) {
	t.Parallel()
	h := &workspace.Handlers{}
	resourceID := uuid.New()
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "dataset")
	rctx.URLParams.Add("id", resourceID.String())
	body := `{"depends_on":[{"resource_kind":"dataset","resource_id":"` + resourceID.String() + `"}]}`
	req := httptest.NewRequest("PUT", "/workspace/resources/dataset/x/references", strings.NewReader(body))
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.ReplaceResourceReferences(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "cannot reference itself")
}
