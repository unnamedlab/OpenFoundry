package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
)

type fakeObjectStore struct {
	getObj  *repos.Object
	getErr  error
	listRes repos.PagedResult[repos.Object]
	listErr error
}

func (f *fakeObjectStore) Get(_ context.Context, _ repos.TenantId, _ repos.ObjectId, _ repos.ReadConsistency) (*repos.Object, error) {
	return f.getObj, f.getErr
}
func (f *fakeObjectStore) Put(context.Context, repos.Object, *uint64) (repos.PutOutcome, error) {
	return repos.PutOutcome{}, repos.Backend("not implemented")
}
func (f *fakeObjectStore) Delete(context.Context, repos.TenantId, repos.ObjectId) (bool, error) {
	return false, repos.Backend("not implemented")
}
func (f *fakeObjectStore) ListByType(_ context.Context, _ repos.TenantId, _ repos.TypeId, _ repos.Page, _ repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	return f.listRes, f.listErr
}
func (f *fakeObjectStore) ListByOwner(context.Context, repos.TenantId, repos.OwnerId, repos.Page, repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	return repos.PagedResult[repos.Object]{}, repos.Backend("not implemented")
}
func (f *fakeObjectStore) ListByMarking(context.Context, repos.TenantId, repos.MarkingId, repos.Page, repos.ReadConsistency) (repos.PagedResult[repos.Object], error) {
	return repos.PagedResult[repos.Object]{}, repos.Backend("not implemented")
}

func newHandler(store *fakeObjectStore) *handlers.Handlers {
	return handlers.New(handlers.AppState{Objects: store})
}

func authedReq(method, target string, params map[string]string, claims *authmw.Claims) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	req := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if claims == nil {
		claims = &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	}
	return req.WithContext(authmw.ContextWithClaims(ctx, claims))
}

func TestGetObjectRequiresAuth(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeObjectStore{})
	req := httptest.NewRequest("GET", "/objects/acme/abc", nil)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "authentication required")
}

func TestGetObjectRejectsInvalidTenant(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeObjectStore{})
	req := authedReq("GET", "/objects/not-a-uuid/abc", map[string]string{
		"tenant":    "not-a-uuid",
		"object_id": uuid.NewString(),
	}, nil)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "tenant is not a valid UUID")
}

func TestGetObjectRejectsInvalidObjectID(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeObjectStore{})
	req := authedReq("GET", "/objects/acme/not-a-uuid", map[string]string{
		"tenant":    uuid.NewString(),
		"object_id": "not-a-uuid",
	}, nil)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "object_id is not a valid UUID")
}

func TestGetObjectReturns404WhenAbsent(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeObjectStore{})
	tenant := uuid.NewString()
	objectID := uuid.NewString()
	req := authedReq("GET", "/objects/"+tenant+"/"+objectID, map[string]string{
		"tenant": tenant, "object_id": objectID,
	}, nil)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "object not found")
}

func TestGetObjectReturnsRustCompatibleObjectShape(t *testing.T) {
	t.Parallel()
	tenant := uuid.NewString()
	objectID := uuid.NewString()
	owner := repos.OwnerId(uuid.NewString())
	created := int64(1717171717000)
	store := &fakeObjectStore{getObj: &repos.Object{
		Tenant:      repos.TenantId(tenant),
		ID:          repos.ObjectId(objectID),
		TypeID:      repos.TypeId("aircraft"),
		Version:     7,
		Payload:     json.RawMessage(`{"callsign":"OF-1"}`),
		CreatedAtMs: &created,
		UpdatedAtMs: created + 1000,
		Owner:       &owner,
		Markings:    []repos.MarkingId{"PUBLIC"},
	}}
	h := newHandler(store)
	claims := &authmw.Claims{
		Sub:          uuid.New(),
		Roles:        []string{"user"},
		SessionScope: &authmw.SessionScope{AllowedMarkings: []string{"PUBLIC"}},
	}
	req := authedReq("GET", "/objects/"+tenant+"/"+objectID, map[string]string{
		"tenant": tenant, "object_id": objectID,
	}, claims)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, tenant, body["tenant"])
	assert.Equal(t, objectID, body["id"])
	assert.Equal(t, "aircraft", body["type_id"])
	assert.Equal(t, float64(7), body["version"])
	assert.Equal(t, map[string]any{"callsign": "OF-1"}, body["payload"])
	assert.Equal(t, []any{"PUBLIC"}, body["markings"])
}

func TestGetObjectBackendErrorReturns500(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeObjectStore{getErr: repos.Backend("cassandra timeout")})
	tenant := uuid.NewString()
	objectID := uuid.NewString()
	req := authedReq("GET", "/objects/"+tenant+"/"+objectID, map[string]string{
		"tenant": tenant, "object_id": objectID,
	}, nil)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "backend error: cassandra timeout")
}

func TestListObjectsByTypeReturnsPage(t *testing.T) {
	t.Parallel()
	tenant := uuid.NewString()
	next := "opaque"
	store := &fakeObjectStore{listRes: repos.PagedResult[repos.Object]{
		Items: []repos.Object{{
			Tenant:      repos.TenantId(tenant),
			ID:          repos.ObjectId(uuid.NewString()),
			TypeID:      repos.TypeId("aircraft"),
			Version:     1,
			Payload:     json.RawMessage(`{}`),
			UpdatedAtMs: 10,
		}},
		NextToken: &next,
	}}
	h := newHandler(store)
	req := authedReq("GET", "/objects/"+tenant+"/by-type/aircraft?size=10", map[string]string{
		"tenant": tenant, "type_id": "aircraft",
	}, nil)
	rec := httptest.NewRecorder()
	h.ListObjectsByType(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body["items"], 1)
	assert.Equal(t, next, body["next_token"])
}

func TestListObjectsByTypeRequiresAuth(t *testing.T) {
	t.Parallel()
	h := newHandler(&fakeObjectStore{})
	req := httptest.NewRequest("GET", "/objects/acme/by-type/X", nil)
	rec := httptest.NewRecorder()
	h.ListObjectsByType(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
