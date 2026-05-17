// Package server wires the HTTP router, observability and graceful
// shutdown for the cipher-service stub. The shape mirrors
// services/template/internal/server so platform tooling stays uniform;
// the only divergence is the route table in `mountAPIRoutes`, which
// reflects the gateway's `/api/v1/auth/cipher` prefix.
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
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/handler"
)

// Server bundles the lifecycle of the HTTP listener.
type Server struct {
	httpServer *http.Server
	cfg        *config.Config
	log        *slog.Logger
}

// New builds a Server with all middleware and routes mounted.
func New(cfg *config.Config, metrics *observability.Metrics, log *slog.Logger, probes ...capabilities.DependencyProbe) (*Server, error) {
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
	mountAPIRoutes(api, caps)

	shutdownTimeout := 15 * time.Second
	if d, err := time.ParseDuration(cfg.Server.ShutdownTimeout); err == nil {
		shutdownTimeout = d
	}

	s := &Server{
		cfg: cfg,
		log: log,
		httpServer: &http.Server{
			Addr:              cfg.Server.Addr,
			Handler:           r,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
	_ = shutdownTimeout
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

// mountAPIRoutes registers the gateway-facing endpoints. The gateway
// currently funnels everything under `/api/v1/auth/cipher` to this
// upstream (see router_table.go), so we register a single catch-all
// 501 placeholder anchored at that prefix. Each milestone in
// docs/migration/foundry-cipher-1to1-checklist.md will replace one of
// these entries with a real handler.
func mountAPIRoutes(r chi.Router, caps *capabilities.Registry) {
	const milestone = "A"

	stub := handler.NotImplemented(milestone)

	caps.MustRegister(r, capabilities.Capability{
		ID:           "cipher.gateway.stub",
		Method:       http.MethodGet,
		Path:         "/api/v1/auth/cipher/*",
		Stable:       false,
		RequiresAuth: true,
		Summary:      "501 stub for /api/v1/auth/cipher/* until Milestone A ships.",
		Tags:         []string{"cipher", "stub"},
	}, stub)

	// chi capability registry only records one Method per entry, so the
	// remaining verbs are bound directly to keep the gateway from
	// receiving 405s instead of the documented 501 envelope.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		r.Method(method, "/api/v1/auth/cipher/*", stub)
	}
	// Match the exact prefix without a trailing path segment as well.
	r.Get("/api/v1/auth/cipher", stub)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		r.Method(method, "/api/v1/auth/cipher", stub)
	}
}
