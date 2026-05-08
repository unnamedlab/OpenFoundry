package server_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/server"
)

func TestTopLevelRBACRoutesAreMountedInAuthorizationPolicyService(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Service.Name = "authorization-policy-service"
	srv := server.New(cfg, nil, &handlers.Handlers{}, observability.NewMetrics())
	router, ok := srv.Handler.(chi.Routes)
	require.True(t, ok)

	mounted := map[string]bool{}
	require.NoError(t, chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+route] = true
		return nil
	}))

	for _, route := range []string{
		"GET /api/v1/permissions",
		"POST /api/v1/permissions",
		"GET /api/v1/roles",
		"POST /api/v1/roles",
		"GET /api/v1/roles/{id}",
		"PUT /api/v1/roles/{id}",
		"PATCH /api/v1/roles/{id}",
		"DELETE /api/v1/roles/{id}",
		"GET /api/v1/groups",
		"POST /api/v1/groups",
		"GET /api/v1/groups/{id}",
		"PUT /api/v1/groups/{id}",
		"PATCH /api/v1/groups/{id}",
		"DELETE /api/v1/groups/{id}",
		"POST /api/v1/users/{id}/roles",
		"DELETE /api/v1/users/{id}/roles/{role_id}",
		"POST /api/v1/users/{id}/groups",
		"DELETE /api/v1/users/{id}/groups/{group_id}",
	} {
		require.Truef(t, mounted[route], "missing route %s", route)
	}
}

func TestRestrictedViewCRUDRoutesAreNotMountedInAuthorizationPolicyService(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Service.Name = "authorization-policy-service"
	srv := server.New(cfg, nil, &handlers.Handlers{}, observability.NewMetrics())
	router, ok := srv.Handler.(chi.Routes)
	require.True(t, ok)

	mounted := map[string]bool{}
	require.NoError(t, chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+route] = true
		return nil
	}))

	require.True(t, mounted["POST /api/v1/policy-evaluations"], "restricted views are evaluated through policy evaluations")
	for _, route := range []string{
		"GET /api/v1/restricted-views",
		"POST /api/v1/restricted-views",
		"GET /api/v1/restricted-views/{id}",
		"PUT /api/v1/restricted-views/{id}",
		"PATCH /api/v1/restricted-views/{id}",
		"DELETE /api/v1/restricted-views/{id}",
	} {
		require.Falsef(t, mounted[route], "restricted-view CRUD is consolidated in identity-federation-service; unexpected route %s", route)
	}
}
