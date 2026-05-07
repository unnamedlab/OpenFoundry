// Package server wires the chi router for entity-resolution-service.
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
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/handlers"
)

func New(cfg *config.Config, jwt *authmw.JWTConfig, h *handlers.Handlers, m *observability.Metrics) *http.Server {
	r := buildRouter(cfg, jwt, h, m)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, jwt *authmw.JWTConfig, h *handlers.Handlers, m *observability.Metrics) http.Handler {
	return buildRouter(cfg, jwt, h, m)
}

func buildRouter(cfg *config.Config, jwt *authmw.JWTConfig, h *handlers.Handlers, m *observability.Metrics) chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	r.Route("/api/v1/fusion", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		api.Get("/overview", h.GetOverview)

		api.Get("/rules", h.ListRules)
		api.Post("/rules", h.CreateRule)
		api.Patch("/rules/{id}", h.UpdateRule)

		api.Get("/merge-strategies", h.ListMergeStrategies)
		api.Post("/merge-strategies", h.CreateMergeStrategy)
		api.Patch("/merge-strategies/{id}", h.UpdateMergeStrategy)

		api.Get("/jobs", h.ListJobs)
		api.Post("/jobs", h.CreateJob)
		api.Post("/jobs/{id}/run", h.RunJob)

		api.Get("/clusters", h.ListClusters)
		api.Get("/clusters/{id}", h.GetCluster)
		api.Post("/clusters/{id}/review", h.SubmitReview)

		api.Get("/review-queue", h.ListReviewQueue)
		api.Get("/golden-records", h.ListGoldenRecords)
	})

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
