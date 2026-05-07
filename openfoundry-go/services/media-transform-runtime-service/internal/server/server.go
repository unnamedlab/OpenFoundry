// Package server wires the chi router for media-transform-runtime-service.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/runtime"
)

func New(cfg *config.Config, m *observability.Metrics) *http.Server {
	r := buildRouter(m)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// BuildRouter is exposed for tests.
func BuildRouter(m *observability.Metrics) http.Handler {
	return buildRouter(m)
}

func buildRouter(m *observability.Metrics) chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(60 * time.Second))

	r.Get("/healthz", runtime.HealthzHandler)
	r.Get("/catalog", runtime.ListCatalogHandler)
	r.Get("/catalog/{kind}", runtime.CatalogEntryHandler(func(req *http.Request) string {
		return chi.URLParam(req, "kind")
	}))
	r.Post("/transform", runtime.TransformHandler)
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
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
