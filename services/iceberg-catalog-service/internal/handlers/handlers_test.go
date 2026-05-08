package handlers_test

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
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

func TestNamespaceJSONShape(t *testing.T) {
	t.Parallel()
	parent := uuid.New()
	n := models.IcebergNamespace{
		ID: uuid.New(), ProjectRID: "ri.foundry.main.project.x",
		Name: "lakehouse.bronze", ParentNamespaceID: &parent,
		Properties: json.RawMessage(`{"owner":"team"}`),
		CreatedAt:  time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		CreatedBy:  uuid.New(),
	}
	out, err := json.Marshal(n)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "project_rid", "name", "parent_namespace_id",
		"properties", "created_at", "created_by",
	} {
		assert.Contains(t, view, k)
	}
}

func TestCreateNamespaceRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/namespaces",
		strings.NewReader(`{"project_rid":"x","name":"y"}`))
	rec := httptest.NewRecorder()
	h.CreateNamespace(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateNamespaceRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/namespaces",
		strings.NewReader(`{"project_rid":"","name":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateNamespace(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "project_rid and name required")
}

func TestListNamespacesRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/namespaces", nil)
	rec := httptest.NewRecorder()
	h.ListNamespaces(rec, req)
	assert.Equal(t, 401, rec.Code)
}
