// Package server wires the chi router for sdk-generation-service.
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
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/handlers"
)

// New builds the http.Server. `gen` and `builds` are optional; when
// nil their endpoint groups are not mounted (e.g. for tests that
// don't need them).
func New(cfg *config.Config, jwt *authmw.JWTConfig, h *handlers.Handlers, gen *handlers.GenerateHandler, builds *handlers.BuildHandlers, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Method(http.MethodGet, "/metrics", m.Handler())

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		api.Get("/sdk-generation-jobs", h.ListJobs)
		api.Post("/sdk-generation-jobs", h.CreateJob)
		api.Get("/sdk-generation-jobs/{id}", h.GetJob)
		api.Get("/sdk-generation-jobs/{id}/publications", h.ListPublications)
		api.Post("/sdk-generation-jobs/{id}/publications", h.CreatePublication)

		// Real SDK generation: shells out to tools/of-sdk-gen, returns a
		// zip of the produced client tree. Optional — only mounted when
		// the caller supplied a generator driver.
		if gen != nil {
			api.Post("/sdk/generate", gen.Generate)
		}

		// OSDK build pipeline (v0 TypeScript). Mounted only when the
		// caller supplied the repo + worker dependencies.
		if builds != nil {
			api.Post("/sdks/builds", builds.Create)
			api.Get("/sdks/builds", builds.List)
			api.Get("/sdks/builds/{id}", builds.Get)
			api.Get("/sdks/builds/{id}/artifact", builds.Artifact)
		}
	})

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "sdk-generation",
		AuthPaths: []string{"/api/v1"},
		Tags:      []string{"sdk"},
	}); err != nil {
		panic("sdk-generation-service: capability ingest failed: " + err.Error())
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
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
