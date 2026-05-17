package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
)

// TestMountAPIRoutes_StubReturns501 asserts the gateway-facing prefix
// returns the documented 501 envelope so the frontend sees a typed
// error instead of a 502 from a missing upstream.
func TestMountAPIRoutes_StubReturns501(t *testing.T) {
	t.Parallel()
	caps := capabilities.New("cipher-service", "test")
	r := chi.NewRouter()
	caps.Mount(r)
	mountAPIRoutes(r, caps)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/auth/cipher"},
		{http.MethodGet, "/api/v1/auth/cipher/keys"},
		{http.MethodPost, "/api/v1/auth/cipher/encrypt"},
		{http.MethodDelete, "/api/v1/auth/cipher/keys/abc"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != http.StatusNotImplemented {
				t.Fatalf("%s %s = %d, want 501", tc.method, tc.path, w.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body=%q)", err, w.Body.String())
			}
			if body["code"] != "not_implemented" || body["service"] != "cipher-service" || body["milestone"] == "" {
				t.Fatalf("unexpected envelope: %+v", body)
			}
		})
	}
}
