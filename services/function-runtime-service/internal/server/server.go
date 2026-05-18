// Package server wires the chi router for function-runtime-service.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/handlers"
)

// New builds the http.Server bound to cfg.Server.Addr.
func New(cfg *config.Config, h *handlers.Handlers, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           BuildRouter(cfg, h, m, probes...),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// BuildRouter exposes the chi.Router for in-process tests.
func BuildRouter(cfg *config.Config, h *handlers.Handlers, m *observability.Metrics, probes ...capabilities.DependencyProbe) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(60 * time.Second))
	r.Use(audittrail.Middleware())

	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	jwt := authmw.NewJWTConfig(cfg.JWT.Secret).
		WithIssuer(cfg.JWT.Issuer).
		WithAudience(cfg.JWT.Audience)

	r.Route("/api/v1/functions", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		// Runs filter routes mounted *before* the {id} subtree so the
		// literal "runs" segment is not captured as a function id.
		api.Get("/runs", h.ListRuns)
		api.Get("/runs/{run_id}", h.GetRun)

		api.Post("/", h.CreateFunction)
		api.Get("/", h.ListFunctions)
		api.Get("/{id}", h.GetFunction)
		api.Post("/{id}/versions", h.PublishVersion)
		api.Post("/{id}/activate", h.Activate)
		api.Post("/{id}/deprecate", h.Deprecate)
		api.Post("/{id}/invoke", h.Invoke)
		api.Post("/{id}/invoke-async", h.InvokeAsync)
	})

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "function-runtime",
		AuthPaths: []string{"/api/v1/functions"},
		Tags:      []string{"functions"},
	}); err != nil {
		panic("function-runtime-service: capability ingest failed: " + err.Error())
	}

	return r
}

// Run blocks until ctx is done or the listener returns.
func Run(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
