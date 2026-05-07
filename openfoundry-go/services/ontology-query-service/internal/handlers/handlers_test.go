package handlers_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
)

// reqWithChi attaches chi route params + claims to a request.
func reqWithChi(method, target string, params map[string]string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	_ = method
	_ = target
	_ = params
	return rec
}

func TestGetObjectRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/objects/acme/abc", nil)
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestGetObjectReturns501WhenAuthed(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant", "acme")
	rctx.URLParams.Add("object_id", "obj-1")

	req := httptest.NewRequest("GET", "/objects/acme/obj-1", nil)
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	assert.Equal(t, 501, rec.Code)
	assert.Contains(t, rec.Body.String(), "Cassandra read backend not wired")
}

func TestListObjectsByTypeReturns501WhenAuthed(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant", "acme")
	rctx.URLParams.Add("type_id", "Customer")

	req := httptest.NewRequest("GET", "/objects/acme/by-type/Customer", nil)
	req = req.WithContext(authmw.ContextWithClaims(
		context.WithValue(req.Context(), chi.RouteCtxKey, rctx), c))
	rec := httptest.NewRecorder()
	h.ListObjectsByType(rec, req)
	assert.Equal(t, 501, rec.Code)
}

func TestListObjectsByTypeRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/objects/acme/by-type/X", nil)
	rec := httptest.NewRecorder()
	h.ListObjectsByType(rec, req)
	assert.Equal(t, 401, rec.Code)
}
