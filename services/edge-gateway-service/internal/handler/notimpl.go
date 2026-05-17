package handler

import (
	"net/http"

	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/errs"
	"github.com/openfoundry/openfoundry-go/services/edge-gateway-service/internal/proxy"
)

// NotImplemented returns 501 directly from the router, bypassing the
// proxy. Used for paths whose upstream service was retired (ADR-0030)
// with no in-tree replacement — e.g. `/api/v1/streaming/*`.
func NotImplemented() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		errs.Write(w, http.StatusNotImplemented,
			errs.CodeServiceNotImplemented, "service not implemented")
	})
}

// ProxyOrNotImplemented forwards to next when SelectUpstream resolves
// a live upstream for the request path and otherwise responds 501.
// Mounted on prefixes (like `/api/v1/apps`) whose catch-all rule was
// retired but whose more-specific rules still route to a live service.
func ProxyOrNotImplemented(next http.Handler, upstreams config.UpstreamURLs) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if proxy.SelectUpstream(r.URL.Path, upstreams) != "" {
			next.ServeHTTP(w, r)
			return
		}
		errs.Write(w, http.StatusNotImplemented,
			errs.CodeServiceNotImplemented, "service not implemented")
	})
}
