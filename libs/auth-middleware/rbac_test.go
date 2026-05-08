package authmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func protectedHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusNoContent)
	})
}

func runMiddleware(mw func(http.Handler) http.Handler, claims *authmw.Claims) (*httptest.ResponseRecorder, bool) {
	called := false
	handler := mw(protectedHandler(&called))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if claims != nil {
		req = req.WithContext(authmw.ContextWithClaims(context.Background(), claims))
	}
	handler.ServeHTTP(rec, req)
	return rec, called
}

func TestRequireRoles401WithoutClaims(t *testing.T) {
	t.Parallel()
	rec, called := runMiddleware(authmw.RequireRoles(authmw.RoleAdmin), nil)
	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireRoles403WithMismatchedClaims(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{authmw.RoleViewer}}
	rec, called := runMiddleware(authmw.RequireRoles(authmw.RoleAdmin, authmw.RoleEditor), c)
	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "requires one of: admin, editor")
}

func TestRequireRolesAcceptsAnyMatchingRole(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.New(), Roles: []string{authmw.RoleEditor}}
	rec, called := runMiddleware(authmw.RequireRoles(authmw.RoleAdmin, authmw.RoleEditor), c)
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestRequireAdminConvenienceWrapper(t *testing.T) {
	t.Parallel()
	admin := &authmw.Claims{Sub: uuid.New(), Roles: []string{authmw.RoleAdmin}}
	rec, called := runMiddleware(authmw.RequireAdmin(), admin)
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	viewer := &authmw.Claims{Sub: uuid.New(), Roles: []string{authmw.RoleViewer}}
	rec, called = runMiddleware(authmw.RequireAdmin(), viewer)
	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRequirePermissionsHonoursWildcardsAndAdmin(t *testing.T) {
	t.Parallel()
	// Wildcard match: `datasets:*` should satisfy `datasets:read`.
	wild := &authmw.Claims{Sub: uuid.New(), Permissions: []string{"datasets:*"}}
	rec, called := runMiddleware(authmw.RequirePermissions("datasets:read"), wild)
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Admin role: short-circuits any permission check.
	admin := &authmw.Claims{Sub: uuid.New(), Roles: []string{authmw.RoleAdmin}}
	rec, called = runMiddleware(authmw.RequirePermissions("ontology:write"), admin)
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Unrelated permission: forbidden.
	other := &authmw.Claims{Sub: uuid.New(), Permissions: []string{"pipelines:read"}}
	rec, called = runMiddleware(authmw.RequirePermissions("ontology:write"), other)
	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
