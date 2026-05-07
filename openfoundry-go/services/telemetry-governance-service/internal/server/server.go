// Package server wires the chi router for telemetry-governance-service.
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
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/telemetry-governance-service/internal/streamingmonitors"
)

// New builds the http.Server with the four feature CRUD blocks mounted.
func New(cfg *config.Config, jwt *authmw.JWTConfig, pool *pgxpool.Pool, m *observability.Metrics) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Method(http.MethodGet, "/metrics", m.Handler())

	smH := &streamingmonitors.Handlers{Repo: &streamingmonitors.Repo{Pool: pool}}

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		for _, feature := range models.AllFeatures() {
			mountFeature(api, pool, feature)
		}
		mountStreamingMonitors(api, smH)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// mountFeature wires the 5 endpoints for a single feature triplet.
//
// For feature "health-checks" (parent: health_checks, child:
// health_check_results, child path: "results") this mounts:
//
//	GET    /api/v1/health-checks
//	POST   /api/v1/health-checks
//	GET    /api/v1/health-checks/{id}
//	GET    /api/v1/health-checks/{id}/results
//	POST   /api/v1/health-checks/{id}/results
func mountFeature(api chi.Router, pool *pgxpool.Pool, ft models.FeatureTables) {
	h := &handlers.Feature{Repo: &repo.FeatureRepo{Pool: pool, Tables: ft}}
	api.Route("/"+ft.Feature, func(g chi.Router) {
		g.Get("/", h.ListPrimary)
		g.Post("/", h.CreatePrimary)
		g.Get("/{id}", h.GetPrimary)
		g.Get("/{id}/"+ft.SecondaryPath, h.ListSecondary)
		g.Post("/{id}/"+ft.SecondaryPath, h.CreateSecondary)
	})
}

// mountStreamingMonitors wires the Foundry-parity streaming-monitor
// surface (Bloque P4) at /api/v1.
func mountStreamingMonitors(api chi.Router, h *streamingmonitors.Handlers) {
	api.Route("/monitoring-views", func(g chi.Router) {
		g.Get("/", h.ListViews)
		g.Post("/", h.CreateView)
		g.Get("/{id}", h.GetView)
		g.Get("/{id}/rules", h.ListRulesForView)
		g.Post("/{id}/rules", h.CreateRule)
	})
	api.Route("/monitor-rules", func(g chi.Router) {
		g.Get("/", h.ListRules)
		g.Patch("/{id}", h.PatchRule)
		g.Delete("/{id}", h.DeleteRule)
		g.Get("/{id}/evaluations", h.ListEvaluations)
	})
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
