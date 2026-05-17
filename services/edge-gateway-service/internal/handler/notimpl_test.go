package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/handler"
)

// TestNotImplemented_WireShape pins the response envelope for retired
// upstreams. Frontend code branches on `code`, so renaming
// `service_not_implemented` here is a wire break.
func TestNotImplemented_WireShape(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/streaming/anything", nil)

	handler.NotImplemented().ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotImplemented; got != want {
		t.Errorf("status = %d, want %d", got, want)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json prefix", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"code":"service_not_implemented"`) {
		t.Errorf("body missing code field; got %q", body)
	}
}
