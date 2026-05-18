// Package server wires the chi router for ontology-query-service.
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
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
)

func New(cfg *config.Config, jwt *authmw.JWTConfig, h *handlers.Handlers, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Method(http.MethodGet, "/metrics", m.Handler())

	// Capability registry — M1.1.
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	r.Route("/api/v1/ontology", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		api.Get("/objects/{tenant}/{object_id}", h.GetObject)
		api.Get("/objects/{tenant}/by-type/{type_id}", h.ListObjectsByType)
		api.Get("/objects/{tenant}/{object_id}/links/{link_type}/outgoing", h.ListOutgoingLinks)
		api.Get("/objects/{tenant}/{object_id}/links/{link_type}/incoming", h.ListIncomingLinks)
	})

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "ontology-query",
		AuthPaths: []string{"/api/v1/ontology"},
		Tags:      []string{"ontology", "query"},
	}); err != nil {
		panic("ontology-query-service: capability ingest failed: " + err.Error())
	}

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
