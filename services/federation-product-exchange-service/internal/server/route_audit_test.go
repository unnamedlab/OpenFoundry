package server

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/marketplace"
	"github.com/openfoundry/openfoundry-go/services/federation-product-exchange-service/internal/productdistribution"
)

func TestRouteAuditSurfaceGroupedByDomain(t *testing.T) {
	t.Parallel()

	routes, ok := BuildRouter(
		testConfig(),
		authmwConfigForTest(),
		marketplace.NewHandlers(newMemoryRepo()),
		productdistribution.NewHandlers(newDistributionMemoryRepo()),
		observability.NewMetrics(),
	).(chi.Routes)
	require.True(t, ok, "router must expose chi routes for route-audit parity checks")

	mounted := map[string]struct{}{}
	require.NoError(t, chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		mounted[method+" "+route] = struct{}{}
		return nil
	}))

	required := map[string][]string{
		"federation": {
			"GET /health",
			"GET /healthz",
			"GET /metrics",
		},
		"marketplace": {
			"GET /v1/marketplace/overview",
			"GET /v1/marketplace/categories",
			"GET /v1/marketplace/listings",
			"POST /v1/marketplace/listings",
			"GET /v1/marketplace/listings/{id}",
			"PATCH /v1/marketplace/listings/{id}",
			"GET /v1/marketplace/listings/{id}/versions",
			"POST /v1/marketplace/listings/{id}/versions",
			"POST /v1/marketplace/listings/{id}/actions",
			"GET /v1/marketplace/search",
			"GET /v1/marketplace/installs",
			"POST /v1/marketplace/installs",
			"POST /v1/marketplace/products/from-dataset/{rid}",
			"GET /v1/marketplace/products/{id}",
			"POST /v1/marketplace/products/{id}/install",
			"POST /v1/products/from-dataset/{rid}",
			"GET /v1/products/{id}",
			"POST /v1/products/{id}/install",
			"POST /v1/products/{id}/schedules",
			"POST /v1/products/{id}/install:schedules",
		},
		"product-distribution": {
			"GET /api/v1/product-distribution/peers",
			"POST /api/v1/product-distribution/peers",
			"GET /api/v1/product-distribution/peers/{id}",
			"PATCH /api/v1/product-distribution/peers/{id}",
			"DELETE /api/v1/product-distribution/peers/{id}",
			"GET /api/v1/product-distribution/contracts",
			"POST /api/v1/product-distribution/contracts",
			"PATCH /api/v1/product-distribution/contracts/{id}",
			"GET /api/v1/product-distribution/shares",
			"POST /api/v1/product-distribution/shares",
			"GET /api/v1/product-distribution/shares/{id}",
			"GET /api/v1/product-distribution/sync-statuses",
			"PATCH /api/v1/product-distribution/shares/{id}/sync-status",
			"POST /api/v1/product-distribution/queries",
		},
	}

	for domain, expected := range required {
		domain := domain
		expected := append([]string(nil), expected...)
		sort.Strings(expected)
		t.Run(domain, func(t *testing.T) {
			for _, route := range expected {
				if _, ok := mounted[route]; !ok {
					t.Fatalf("%s route %s is not mounted; mounted routes:\n%s", domain, route, strings.Join(sortedRouteKeys(mounted), "\n"))
				}
			}
		})
	}
}

func TestRouteAuditAliasesServeWithHTTPTest(t *testing.T) {
	t.Parallel()

	jwt := authmwConfigForTest()
	token := testToken(t, jwt)
	srv := httptest.NewServer(BuildRouter(
		testConfig(),
		jwt,
		marketplace.NewHandlers(newMemoryRepo()),
		productdistribution.NewHandlers(newDistributionMemoryRepo()),
		observability.NewMetrics(),
	))
	t.Cleanup(srv.Close)

	assertHTTPStatus(t, srv.URL+"/health", "", http.StatusOK)
	assertHTTPStatus(t, srv.URL+"/v1/marketplace/categories", token, http.StatusOK)
	assertHTTPStatus(t, srv.URL+"/api/v1/product-distribution/peers", token, http.StatusOK)
}

func assertHTTPStatus(t *testing.T, url string, token string, want int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, want, resp.StatusCode)
}

func sortedRouteKeys(routes map[string]struct{}) []string {
	keys := make([]string, 0, len(routes))
	for route := range routes {
		keys = append(keys, route)
	}
	sort.Strings(keys)
	return keys
}
