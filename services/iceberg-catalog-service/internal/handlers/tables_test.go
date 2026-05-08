package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

// fakeStore is a minimal in-memory Store for handler-level tests of the
// iceberg table CRUD lifecycle. Only the table surface is exercised
// here; namespace plumbing is left to the existing
// handlers_test.go suite.
type fakeStore struct {
	handlers.Store

	mu      sync.Mutex
	tables  map[string]*models.IcebergTable
	creates int32
	dropped int32

	createErr error
	getErr    error

	renameNS string
}

func newFakeStore() *fakeStore {
	return &fakeStore{tables: map[string]*models.IcebergTable{}}
}

func tableKey(namespace []string, name string) string {
	return strings.Join(namespace, ".") + "/" + name
}

func (f *fakeStore) ListTables(_ context.Context, _ string, namespace []string) ([]models.IcebergTable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := strings.Join(namespace, ".") + "/"
	out := []models.IcebergTable{}
	for k, t := range f.tables {
		if strings.HasPrefix(k, prefix) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (f *fakeStore) GetTable(_ context.Context, _ string, namespace []string, name string) (*models.IcebergTable, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.tables[tableKey(namespace, name)]; ok {
		return t, nil
	}
	return nil, nil
}

func (f *fakeStore) CreateTable(_ context.Context, _ string, namespace []string, body *models.CreateTableRequest, _ uuid.UUID) (*models.IcebergTable, string, error) {
	if f.createErr != nil {
		return nil, "", f.createErr
	}
	atomic.AddInt32(&f.creates, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tableKey(namespace, body.Name)
	if _, exists := f.tables[key]; exists {
		return nil, "", fmt.Errorf("table `%s` already exists in namespace", body.Name)
	}
	id := uuid.New()
	tab := &models.IcebergTable{
		ID:            id,
		RID:           "ri.foundry.main.iceberg-table." + id.String(),
		Namespace:     namespace,
		Name:          body.Name,
		TableUUID:     uuid.NewString(),
		FormatVersion: 2,
		Location:      "s3://openfoundry-warehouse/" + strings.Join(namespace, ".") + "/" + body.Name,
		SchemaJSON:    body.Schema,
		PartitionSpec: json.RawMessage(`{"spec-id":0,"fields":[]}`),
		SortOrder:     json.RawMessage(`{"order-id":0,"fields":[]}`),
		Properties:    json.RawMessage(`{}`),
		Markings:      body.Markings,
	}
	f.tables[key] = tab
	return tab, tab.Location + "/metadata/v1.metadata.json", nil
}

func (f *fakeStore) DropTable(_ context.Context, _ string, namespace []string, name string, _ bool) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tableKey(namespace, name)
	if _, ok := f.tables[key]; !ok {
		return false, nil
	}
	delete(f.tables, key)
	atomic.AddInt32(&f.dropped, 1)
	return true, nil
}

func (f *fakeStore) RenameTable(_ context.Context, _ string, src []string, srcName string, dst []string, dstName string) (*models.IcebergTable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, ok := f.tables[tableKey(src, srcName)]
	if !ok {
		return nil, nil
	}
	delete(f.tables, tableKey(src, srcName))
	cur.Namespace = dst
	cur.Name = dstName
	f.tables[tableKey(dst, dstName)] = cur
	f.renameNS = strings.Join(dst, ".")
	return cur, nil
}

func (f *fakeStore) ListSnapshots(_ context.Context, _ uuid.UUID) ([]models.Snapshot, error) {
	return nil, nil
}

func (f *fakeStore) ListRefs(_ context.Context, _ uuid.UUID) ([]models.TableRef, error) {
	return nil, nil
}

func authed(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	claims := &authmw.Claims{Sub: uuid.New()}
	return req.WithContext(authmw.ContextWithClaims(context.Background(), claims))
}

func withChiParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestCreateTableRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: newFakeStore()}
	req := httptest.NewRequest("POST", "/iceberg/v1/namespaces/events/tables",
		strings.NewReader(`{"name":"logins","schema":{"schema-id":0}}`))
	rec := httptest.NewRecorder()
	h.CreateTable(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreateTableRejectsMissingSchema(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: newFakeStore()}
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables",
		`{"name":"logins"}`), map[string]string{"namespace": "events"})
	rec := httptest.NewRecorder()
	h.CreateTable(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name and schema required")
}

func TestCreateLoadDropTableRoundTrip(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	h := &handlers.Handlers{Repo: store}

	// CREATE
	body := `{"name":"logins","schema":{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"long"}]},"properties":{"format":"parquet"}}`
	createReq := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables", body),
		map[string]string{"namespace": "events"})
	createRec := httptest.NewRecorder()
	h.CreateTable(createRec, createReq)
	require.Equal(t, http.StatusOK, createRec.Code, createRec.Body.String())

	var loaded models.LoadTableResponse
	require.NoError(t, json.Unmarshal(createRec.Body.Bytes(), &loaded))
	assert.Contains(t, loaded.MetadataLocation, "/v1.metadata.json")

	// LOAD
	loadReq := withChiParams(authed("GET", "/iceberg/v1/namespaces/events/tables/logins", ""),
		map[string]string{"namespace": "events", "table": "logins"})
	loadRec := httptest.NewRecorder()
	h.LoadTable(loadRec, loadReq)
	require.Equal(t, http.StatusOK, loadRec.Code, loadRec.Body.String())

	// DROP
	dropReq := withChiParams(authed("DELETE", "/iceberg/v1/namespaces/events/tables/logins", ""),
		map[string]string{"namespace": "events", "table": "logins"})
	dropRec := httptest.NewRecorder()
	h.DropTable(dropRec, dropReq)
	require.Equal(t, http.StatusNoContent, dropRec.Code)

	// LOAD AGAIN → 404
	missReq := withChiParams(authed("GET", "/iceberg/v1/namespaces/events/tables/logins", ""),
		map[string]string{"namespace": "events", "table": "logins"})
	missRec := httptest.NewRecorder()
	h.LoadTable(missRec, missReq)
	assert.Equal(t, http.StatusNotFound, missRec.Code)
}

// TestCreateTableConflictMaps409 asserts the handler maps the repo's
// "already exists" error to HTTP 409, matching Rust's
// TableError::AlreadyExists → 409 Conflict.
func TestCreateTableConflictMaps409(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.createErr = errors.New("table `logins` already exists in namespace")
	h := &handlers.Handlers{Repo: store}

	body := `{"name":"logins","schema":{"schema-id":0}}`
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables", body),
		map[string]string{"namespace": "events"})
	rec := httptest.NewRecorder()
	h.CreateTable(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestRenameTableAcrossNamespaces(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	// Pre-seed a table to rename.
	store.tables[tableKey([]string{"events"}, "logins")] = &models.IcebergTable{
		ID: uuid.New(), Name: "logins", Namespace: []string{"events"}, Markings: []string{"public"},
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"source":{"namespace":["events"],"name":"logins"},"destination":{"namespace":["warehouse"],"name":"logins_v2"}}`
	req := authed("POST", "/iceberg/v1/tables/rename", body)
	rec := httptest.NewRecorder()
	h.RenameTable(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "warehouse", store.renameNS)
}
