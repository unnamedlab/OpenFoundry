// Package server hosts the HTTP-health-mode surface for lineage-service.
//
// Mirrors the Rust binary's RuntimeMode::HttpHealth: a public /health
// endpoint plus the JWT-gated /api/v1/lineage query + workflow-sync
// surface ported from `services/lineage-service/src/handlers/lineage.rs`.
// The Kafka → Iceberg sink lives in a separate runtime gated on
// iceberg-go availability and is not mounted here.
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
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/handlers"
)

// Options bundles the optional lineage-domain plumbing. When nil, the
// /api/v1/lineage routes are skipped (HTTP-health-only mode, same as
// the Rust impl when DATABASE_URL is unset).
type Options struct {
	JWT      *authmw.JWTConfig
	Handlers *handlers.Handlers
}

func New(cfg *config.Config, m *observability.Metrics, opts *Options) *http.Server {
	r := buildRouter(cfg, m, opts)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, m *observability.Metrics, opts *Options) http.Handler {
	return buildRouter(cfg, m, opts)
}

func buildRouter(cfg *config.Config, m *observability.Metrics, opts *Options) chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	// Plain text "ok" matches the Rust HttpHealth mode body verbatim.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	// /healthz returns the structured openfoundry-go body.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	if opts != nil && opts.JWT != nil && opts.Handlers != nil {
		r.Route("/api/v1/lineage", func(api chi.Router) {
			api.Use(authmw.Middleware(opts.JWT))

			api.Get("/datasets/{id}", opts.Handlers.GetDatasetLineage)
			api.Get("/datasets/{id}/columns", opts.Handlers.GetDatasetColumnLineage)
			api.Get("/datasets/{id}/impact", opts.Handlers.GetDatasetLineageImpact)
			api.Post("/datasets/{id}/builds", opts.Handlers.TriggerDatasetLineageBuilds)
			api.Get("/full", opts.Handlers.GetFullLineage)

			api.Post("/workflows/{id}/sync", opts.Handlers.SyncWorkflowLineage)
			api.Delete("/workflows/{id}", opts.Handlers.DeleteWorkflowLineage)
		})
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
