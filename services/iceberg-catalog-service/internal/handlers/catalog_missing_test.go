package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

type catalogStore struct {
	*fakeStore
	namespaces map[string]*models.IcebergNamespace
	adminErr   error
}

func newCatalogStore() *catalogStore {
	return &catalogStore{fakeStore: newFakeStore(), namespaces: map[string]*models.IcebergNamespace{}}
}

func (s *catalogStore) ListNamespaces(_ context.Context, projectRID string) ([]models.IcebergNamespace, error) {
	out := []models.IcebergNamespace{}
	for _, ns := range s.namespaces {
		if projectRID == "" || ns.ProjectRID == projectRID {
			out = append(out, *ns)
		}
	}
	return out, nil
}

func (s *catalogStore) FetchNamespaceByName(_ context.Context, projectRID string, path []string) (*models.IcebergNamespace, error) {
	ns := s.namespaces[projectRID+":"+strings.Join(path, ".")]
	return ns, nil
}

func (s *catalogStore) CreateNamespace(_ context.Context, body *models.CreateNamespaceRequest, _ uuid.UUID) (*models.IcebergNamespace, error) {
	if body.Name == "duplicate" {
		return nil, errors.New("namespace already exists")
	}
	ns := &models.IcebergNamespace{ID: uuid.New(), ProjectRID: body.ProjectRID, Name: body.Name, Properties: body.Properties, CreatedBy: uuid.New()}
	s.namespaces[body.ProjectRID+":"+body.Name] = ns
	return ns, nil
}

func (s *catalogStore) UpdateNamespaceProperties(_ context.Context, id uuid.UUID, properties []byte) (*models.IcebergNamespace, error) {
	for _, ns := range s.namespaces {
		if ns.ID == id {
			ns.Properties = properties
			return ns, nil
		}
	}
	return nil, nil
}

func (s *catalogStore) DeleteNamespace(_ context.Context, id uuid.UUID) (bool, error) {
	for key, ns := range s.namespaces {
		if ns.ID == id {
			delete(s.namespaces, key)
			return true, nil
		}
	}
	return false, nil
}

func (s *catalogStore) UpdateTableSchema(_ context.Context, id uuid.UUID, schema json.RawMessage) (*models.IcebergTable, error) {
	for _, t := range s.tables {
		if t.ID == id {
			t.SchemaJSON = schema
			return t, nil
		}
	}
	return nil, nil
}

func (s *catalogStore) ListAdminTables(_ context.Context, q models.ListIcebergTablesQuery) ([]models.IcebergTableSummary, error) {
	if s.adminErr != nil {
		return nil, s.adminErr
	}
	out := []models.IcebergTableSummary{}
	for _, t := range s.tables {
		if q.Name != "" && !strings.Contains(t.Name, q.Name) {
			continue
		}
		out = append(out, models.IcebergTableSummary{ID: t.ID, RID: t.RID, Namespace: t.Namespace, Name: t.Name, FormatVersion: t.FormatVersion, Location: t.Location, Markings: t.Markings, CreatedAt: t.CreatedAt})
	}
	return out, nil
}

func (s *catalogStore) GetTableByRID(_ context.Context, rid string) (*models.IcebergTable, error) {
	for _, t := range s.tables {
		if t.RID == rid {
			return t, nil
		}
	}
	return nil, nil
}

func TestRESTCatalogNamespaceRoutesUseIcebergEnvelope(t *testing.T) {
	t.Parallel()
	store := newCatalogStore()
	h := &handlers.Handlers{Repo: store}

	req := authed(http.MethodPost, "/iceberg/v1/namespaces", `{"namespace":["lakehouse","bronze"],"properties":{"owner":"analytics"}}`)
	rec := httptest.NewRecorder()
	h.CreateCatalogNamespace(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var created map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&created))
	require.Equal(t, []any{"lakehouse", "bronze"}, created["namespace"])
	require.Contains(t, created, "properties")

	req = withChiParams(authed(http.MethodGet, "/iceberg/v1/namespaces/lakehouse.bronze", ""), map[string]string{"namespace": "lakehouse.bronze"})
	rec = httptest.NewRecorder()
	h.LoadCatalogNamespace(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"owner":"analytics"`)
}

func TestRESTCatalogNamespaceErrorsAuthzAndMissingCatalog(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: newCatalogStore()}

	req := httptest.NewRequest(http.MethodGet, "/iceberg/v1/namespaces/missing", nil)
	req = withChiParams(req, map[string]string{"namespace": "missing"})
	rec := httptest.NewRecorder()
	h.LoadCatalogNamespace(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), `"type":"AuthenticationException"`)

	req = withChiParams(authed(http.MethodGet, "/iceberg/v1/namespaces/missing", ""), map[string]string{"namespace": "missing"})
	rec = httptest.NewRecorder()
	h.LoadCatalogNamespace(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), `"error"`)
	require.Contains(t, rec.Body.String(), `"namespace not found"`)
}

func TestTableExistsAndAlterSchemaStatusCodes(t *testing.T) {
	t.Parallel()
	store := newCatalogStore()
	_, _, err := store.CreateTable(context.Background(), "", []string{"events"}, &models.CreateTableRequest{Name: "logins", Schema: json.RawMessage(`{"schema-id":0,"fields":[]}`)}, uuid.New())
	require.NoError(t, err)
	h := &handlers.Handlers{Repo: store}

	req := withChiParams(authed(http.MethodHead, "/iceberg/v1/namespaces/events/tables/logins", ""), map[string]string{"namespace": "events", "table": "logins"})
	rec := httptest.NewRecorder()
	h.TableExists(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	req = withChiParams(authed(http.MethodPost, "/iceberg/v1/namespaces/events/tables/logins/alter-schema", `{"updates":[{"action":"add-column","name":"user_id","type":"string"}]}`), map[string]string{"namespace": "events", "table": "logins"})
	rec = httptest.NewRecorder()
	h.AlterSchema(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"schema_id":1`)
	require.Contains(t, rec.Body.String(), `"user_id"`)
}

func TestAdminRoutesUseDocumentedRustOnlyEnvelope(t *testing.T) {
	t.Parallel()
	store := newCatalogStore()
	tab, _, err := store.CreateTable(context.Background(), "", []string{"events"}, &models.CreateTableRequest{Name: "logins", Schema: json.RawMessage(`{"schema-id":0,"fields":[]}`)}, uuid.New())
	require.NoError(t, err)
	h := &handlers.Handlers{Repo: store}

	req := authed(http.MethodGet, "/api/v1/iceberg-tables", "")
	rec := httptest.NewRecorder()
	h.ListIcebergTables(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"tables"`)

	id := strings.TrimPrefix(tab.RID, "ri.foundry.main.iceberg-table.")
	req = withChiParams(authed(http.MethodGet, "/api/v1/iceberg-tables/"+id, ""), map[string]string{"id": id})
	rec = httptest.NewRecorder()
	h.GetIcebergTableDetail(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"summary"`)
}

func TestGoOnlyAndRustOnlyAliasesAreDocumentedWithCompatibleStatus(t *testing.T) {
	t.Parallel()
	store := newCatalogStore()
	h := &handlers.Handlers{Repo: store}

	// Go-only /api/v1/namespaces remains a management alias and uses the
	// Go list envelope, while the Rust-only REST Catalog namespace route
	// uses the Iceberg REST `namespaces` envelope.
	req := authed(http.MethodGet, "/api/v1/namespaces", "")
	rec := httptest.NewRecorder()
	h.ListNamespaces(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"items"`)

	req = authed(http.MethodGet, "/iceberg/v1/namespaces", "")
	rec = httptest.NewRecorder()
	h.ListCatalogNamespaces(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"namespaces"`)
}
