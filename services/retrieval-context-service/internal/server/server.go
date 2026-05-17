// Package server hosts the substrate HTTP surface for retrieval-context-service.
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
)

func New(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	r := buildRouter(cfg, m, probes...)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) http.Handler {
	return buildRouter(cfg, m, probes...)
}

func buildRouter(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(15 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	// Protected route group. Real handlers wire alongside
	// libs/ai-kernel-go/handlers in a follow-up slice; until then the
	// group only carries an auth probe so the middleware chain is
	// exercised end-to-end. Anything mounted under /api/v1 in the
	// future inherits authmw enforcement by default.
	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		api.Get("/_authz_probe", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
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
