// Package server wires the chi router for model-catalog-service.
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
	"github.com/openfoundry/openfoundry-go/services/model-catalog-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/model-catalog-service/internal/handlers"
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

	r.Route("/api/v1/model-catalog", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		// Adapter surface (local).
		api.Get("/adapters", h.ListAdapters)
		api.Post("/adapters", h.RegisterAdapter)
		api.Get("/adapters/{id}", h.GetAdapter)
		api.Get("/adapters/{id}/contracts", h.ListContracts)
		api.Post("/adapters/{id}/contracts", h.PublishContract)

		// Lifecycle surface (local).
		api.Get("/submissions", h.ListSubmissions)
		api.Post("/submissions", h.CreateSubmission)
		api.Get("/submissions/{id}", h.GetSubmission)
		api.Post("/submissions/{id}/transition", h.TransitionSubmission)

		api.Get("/objectives", h.ListObjectives)
		api.Post("/objectives", h.CreateObjective)
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
