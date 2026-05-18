// Package server hosts the HTTP surface for retrieval-context-service.
//
// Routing tree:
//
//	/healthz                                                 — health probe
//	/metrics                                                 — Prometheus
//	/api/v1/_authz_probe                                     — auth chain probe
//	/api/v1/document-intelligence/jobs                       — CRUD
//	/api/v1/document-intelligence/jobs/{id}                  — CRUD
//	/api/v1/document-intelligence/jobs/{id}/events           — append + list
//	/api/v1/document-intelligence/jobs/{id}/extractions      — record + list
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/handlers"
)

// Deps groups the dependencies the router consumes. Tests build it with
// the in-memory store; main.go builds it from the pgx pool.
type Deps struct {
	Jobs *handlers.Jobs
	JWT  *authmw.JWTConfig
}

// New constructs the production http.Server.
func New(cfg *config.Config, deps Deps, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	r := buildRouter(cfg, deps, m, probes...)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// BuildRouter is the test-facing builder.
func BuildRouter(cfg *config.Config, deps Deps, m *observability.Metrics, probes ...capabilities.DependencyProbe) http.Handler {
	return buildRouter(cfg, deps, m, probes...)
}

func buildRouter(cfg *config.Config, deps Deps, m *observability.Metrics, probes ...capabilities.DependencyProbe) chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(15 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	jwt := deps.JWT
	if jwt == nil {
		jwt = authmw.NewJWTConfig(cfg.JWTSecret)
	}
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		api.Get("/_authz_probe", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		if deps.Jobs != nil {
			api.Route("/document-intelligence", func(di chi.Router) {
				deps.Jobs.Mount(di)
			})
		}
	})

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "retrieval-context",
		AuthPaths: []string{"/api/v1"},
		Tags:      []string{"ai"},
	}); err != nil {
		panic("retrieval-context-service: capability ingest failed: " + err.Error())
	}

	return r
}

// Run starts srv and shuts it down when ctx is cancelled.
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
