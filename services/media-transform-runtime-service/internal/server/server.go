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
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/runtime"
)

func New(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	r := buildRouter(cfg, m, probes...)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// BuildRouter is exposed for tests.
func BuildRouter(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) http.Handler {
	return buildRouter(cfg, m, probes...)
}

func buildRouter(cfg *config.Config, m *observability.Metrics, probes ...capabilities.DependencyProbe) chi.Router {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(60 * time.Second))

	r.Get("/healthz", runtime.HealthzHandler)
	r.Get("/readyz", runtime.HealthzHandler)
	r.Get("/catalog", runtime.ListCatalogHandler)
	r.Get("/catalog/{kind}", runtime.CatalogEntryHandler(func(req *http.Request) string {
		return chi.URLParam(req, "kind")
	}))
	r.Post("/transform", runtime.TransformHandler)
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "media-transform",
		AuthPaths: nil,
		Tags:      []string{"media"},
	}); err != nil {
		panic("media-transform-runtime-service: capability ingest failed: " + err.Error())
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
