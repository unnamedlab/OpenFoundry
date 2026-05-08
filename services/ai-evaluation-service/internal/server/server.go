// Package server wires the substrate HTTP surface for ai-evaluation-service.
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

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/ai-evaluation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ai-evaluation-service/internal/handlers"
)

// Options bundles optional dependencies passed by main / tests.
type Options struct {
	Pool    *pgxpool.Pool
	Runtime llm.Runtime
}

func New(cfg *config.Config, m *observability.Metrics, opts Options) *http.Server {
	r := buildRouter(cfg, m, opts)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, m *observability.Metrics, opts Options) http.Handler {
	return buildRouter(cfg, m, opts)
}

func buildRouter(cfg *config.Config, m *observability.Metrics, opts Options) chi.Router {
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

	h := &handlers.Handlers{Pool: opts.Pool, Runtime: opts.Runtime}
	r.Route("/api/v1", func(api chi.Router) {
		api.Post("/evaluations/benchmark", h.BenchmarkProviders)
		api.Post("/guardrails/evaluate", h.EvaluateGuardrails)
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
