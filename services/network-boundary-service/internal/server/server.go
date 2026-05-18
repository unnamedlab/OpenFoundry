// Package server wires the HTTP router, observability and graceful
// shutdown for the network-boundary-service. The shape mirrors
// docs/templates/service-skeleton/internal/server so platform tooling stays uniform;
// the only divergence is the route table in `mountAPIRoutes`, which
// reflects the gateway's `u.NetworkBoundary` prefixes.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/handler"
)

// Server bundles the lifecycle of the HTTP listener.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	log        *slog.Logger
}

// New builds a Server with all middleware and routes mounted. The
// egress policy store is injected so main.go can pick the persistent
// (Postgres) or in-memory implementation depending on whether
// DATABASE_URL is configured.
func New(cfg *config.Config, metrics *observability.Metrics, log *slog.Logger, store handler.EgressPolicyStore, probes ...capabilities.DependencyProbe) (*Server, error) {
	if store == nil {
		store = handler.NewMemoryEgressPolicyStore()
	}
	jwtCfg := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)

	// Public endpoints (no auth).
	r.Get("/healthz", handler.Health(cfg.Service.Name, cfg.Service.Version))
	r.Method(http.MethodGet, "/metrics", metrics.Handler())
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	// Authenticated API mount.
	api := r.With(authmw.Middleware(jwtCfg))
	mountAPIRoutes(api, caps, store)

	s := &Server{
		cfg: cfg,
		log: log,
		httpServer: &http.Server{
			Addr:              cfg.Server.Addr,
			Handler:           r,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	return s, nil
}

// Run blocks until the listener returns or `ctx` is cancelled.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", slog.String("addr", s.cfg.Server.Addr))
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errCh:
		return err
	}
}

func (s *Server) shutdown() error {
	timeout := 15 * time.Second
	if d, err := time.ParseDuration(s.cfg.Server.ShutdownTimeout); err == nil {
		timeout = d
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.log.Info("shutting down")
	return s.httpServer.Shutdown(ctx)
}

// placeholderRoutes lists every prefix the edge gateway fans out to
// `u.NetworkBoundary`. Keep in sync with the `u.NetworkBoundary` branch
// in services/edge-gateway-service/internal/proxy/router_table.go.
var placeholderRoutes = []struct {
	id   string
	path string
}{
	{"network-boundary.boundaries.stub", "/api/v1/network-boundaries"},
	{"network-boundary.boundary.stub", "/api/v1/network-boundary"},
}

// mountAPIRoutes registers the gateway-facing endpoints. Each prefix
// gets a single catch-all 501 placeholder bound to every common verb,
// so the frontend always sees the documented envelope instead of a 405
// or 502.
func mountAPIRoutes(r chi.Router, caps *capabilities.Registry, store handler.EgressPolicyStore) {
	const milestone = "S8.6/B14"
	stub := handler.NotImplemented(milestone)
	egress := handler.NewEgressHandler(store)

	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.list",
		Method:       http.MethodGet,
		Path:         "/api/v1/data-connection/egress-policies",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "List visible network egress policies.",
		Tags:         []string{"network-boundary", "egress", "security-governance"},
	}, egress.ListPolicies)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.create",
		Method:       http.MethodPost,
		Path:         "/api/v1/data-connection/egress-policies",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Create direct, agent-proxy, or same-region bucket egress policies.",
		Tags:         []string{"network-boundary", "egress", "security-governance"},
	}, egress.CreatePolicy)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.evaluate-workload",
		Method:       http.MethodPost,
		Path:         "/api/v1/data-connection/egress-policies:evaluate-workload",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Evaluate explicit workload egress imports at runtime.",
		Tags:         []string{"network-boundary", "egress", "runtime"},
	}, egress.EvaluateWorkload)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.approvals.list",
		Method:       http.MethodGet,
		Path:         "/api/v1/data-connection/egress-policies/approvals",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "List network egress approval tasks.",
		Tags:         []string{"network-boundary", "egress", "approvals"},
	}, egress.ListApprovals)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.approvals.decide",
		Method:       http.MethodPost,
		Path:         "/api/v1/data-connection/egress-policies/approvals/{task_id}/decision",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Approve or deny a network egress workflow task.",
		Tags:         []string{"network-boundary", "egress", "approvals"},
	}, egress.DecideApproval)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.get",
		Method:       http.MethodGet,
		Path:         "/api/v1/data-connection/egress-policies/{id}",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Get one network egress policy.",
		Tags:         []string{"network-boundary", "egress", "security-governance"},
	}, egress.GetPolicy)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.update-state",
		Method:       http.MethodPatch,
		Path:         "/api/v1/data-connection/egress-policies/{id}/state",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Approve, pause, or revoke an egress policy.",
		Tags:         []string{"network-boundary", "egress", "security-governance"},
	}, egress.UpdateState)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.update-sharing",
		Method:       http.MethodPatch,
		Path:         "/api/v1/data-connection/egress-policies/{id}/sharing",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Update viewer, importer, and administrator grants for an egress policy.",
		Tags:         []string{"network-boundary", "egress", "security-governance"},
	}, egress.UpdateSharing)
	registerCapability(caps, r, capabilities.Capability{
		ID:           "network-boundary.egress-policies.delete-blocked",
		Method:       http.MethodDelete,
		Path:         "/api/v1/data-connection/egress-policies/{id}",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Reject deletion because egress policies are immutable; use revoke instead.",
		Tags:         []string{"network-boundary", "egress", "security-governance"},
	}, egress.DeletePolicy)

	for _, route := range placeholderRoutes {
		caps.MustRegister(r, capabilities.Capability{
			ID:           route.id,
			Method:       http.MethodGet,
			Path:         route.path + "/*",
			Stable:       false,
			RequiresAuth: true,
			Summary:      "501 stub for " + route.path + "/* until ADR-0030 / S8.6 ships.",
			Tags:         []string{"network-boundary", "stub"},
		}, stub)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			r.Method(method, route.path+"/*", stub)
		}
		r.Get(route.path, stub)
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			r.Method(method, route.path, stub)
		}
	}
}

func registerCapability(caps *capabilities.Registry, r chi.Router, cap capabilities.Capability, h http.HandlerFunc) {
	caps.MustRegister(r, cap, h)
}
