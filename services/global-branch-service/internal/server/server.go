// Package server wires the HTTP router, observability and graceful
// shutdown for the global-branch-service.
//
// Routes implement the Global Branching Milestone A surface:
// lifecycle CRUD on global branches + per-service participation
// management. The legacy /api/v1/code-repos/.../branches paths owned
// by code-repository-review-service are NOT mounted here — the
// gateway routing for them will switch over in a follow-up PR (see
// internal/README.md).
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
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/handler"
)

// Server bundles the lifecycle of the HTTP listener.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	log        *slog.Logger
}

// New builds a Server with all middleware and routes mounted.
//
// Passing h == nil keeps only the public surface mounted (health,
// metrics, capability catalog). Main permits that state only for
// explicit non-production smoke mode; product routes appear in the
// catalog only when handlers are wired.
func New(cfg *config.Config, h *handler.Handlers, metrics *observability.Metrics, log *slog.Logger, probes ...capabilities.DependencyProbe) (*Server, error) {
	jwtCfg := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)

	r := BuildRouter(cfg, h, metrics, jwtCfg, probes...)

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

// BuildRouter assembles the chi router with middleware, capabilities,
// and product routes. Exposed so handler tests can spin up an
// httptest.Server pointing at the same routing logic.
func BuildRouter(cfg *config.Config, h *handler.Handlers, metrics *observability.Metrics, jwtCfg *authmw.JWTConfig, probes ...capabilities.DependencyProbe) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)

	// Public endpoints (no auth).
	r.Get("/healthz", handler.Health(cfg.Service.Name, cfg.Service.Version))
	if metrics != nil {
		r.Method(http.MethodGet, "/metrics", metrics.Handler())
	}
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	if h != nil {
		api := r.With(authmw.Middleware(jwtCfg))
		mountAPIRoutes(api, caps, h)
	}

	return r
}

// Run blocks until the listener returns or ctx is cancelled.
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

// mountAPIRoutes registers every product route. Every endpoint requires
// authentication and tenant scope (claims.OrgID).
func mountAPIRoutes(r chi.Router, caps *capabilities.Registry, h *handler.Handlers) {
	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.branches.create",
		Method:       http.MethodPost,
		Path:         "/api/v1/global-branches",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Create a global branch coordinating local branches across services.",
		Tags:         []string{"global-branching"},
	}, http.HandlerFunc(h.CreateBranch))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.branches.list",
		Method:       http.MethodGet,
		Path:         "/api/v1/global-branches",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "List the caller-tenant's global branches, optionally filtered by status.",
		Tags:         []string{"global-branching"},
	}, http.HandlerFunc(h.ListBranches))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.branches.get",
		Method:       http.MethodGet,
		Path:         "/api/v1/global-branches/{id}",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Fetch a global branch by id.",
		Tags:         []string{"global-branching"},
	}, http.HandlerFunc(h.GetBranch))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.branches.update",
		Method:       http.MethodPatch,
		Path:         "/api/v1/global-branches/{id}",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Update branch metadata (name, description).",
		Tags:         []string{"global-branching"},
	}, http.HandlerFunc(h.UpdateBranch))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.branches.abandon",
		Method:       http.MethodPost,
		Path:         "/api/v1/global-branches/{id}/abandon",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Abandon an open branch (terminal state).",
		Tags:         []string{"global-branching", "lifecycle"},
	}, http.HandlerFunc(h.AbandonBranch))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.branches.merge",
		Method:       http.MethodPost,
		Path:         "/api/v1/global-branches/{id}/merge",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "Trigger the coordinated merge across participating services.",
		Tags:         []string{"global-branching", "lifecycle"},
	}, http.HandlerFunc(h.MergeBranch))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.participants.add",
		Method:       http.MethodPost,
		Path:         "/api/v1/global-branches/{id}/participants",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Register a service participation on a global branch.",
		Tags:         []string{"global-branching", "participation"},
	}, http.HandlerFunc(h.AddParticipant))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.participants.remove",
		Method:       http.MethodDelete,
		Path:         "/api/v1/global-branches/{id}/participants/{service}",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "Remove a service participation from a global branch.",
		Tags:         []string{"global-branching", "participation"},
	}, http.HandlerFunc(h.RemoveParticipant))

	caps.MustRegister(r, capabilities.Capability{
		ID:           "global-branch.participants.list",
		Method:       http.MethodGet,
		Path:         "/api/v1/global-branches/{id}/participants",
		Stable:       true,
		RequiresAuth: true,
		Summary:      "List participations enrolled on a global branch.",
		Tags:         []string{"global-branching", "participation"},
	}, http.HandlerFunc(h.ListParticipants))
}
