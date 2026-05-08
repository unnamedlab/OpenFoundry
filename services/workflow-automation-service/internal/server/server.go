// Package server hosts the HTTP surface for workflow-automation-service.
//
// Mirrors `services/workflow-automation-service/src/main.rs::build_router`:
//
//	/healthz                                            (always-on)
//	/metrics                                            (always-on)
//
//	/api/v1/workflows                                   (JWT)
//	/api/v1/workflows/{id}                              (JWT)
//	/api/v1/workflows/{id}/runs                         (JWT)
//	/api/v1/workflows/approvals/{approval_id}/continue  (JWT)
//	/api/v1/workflows/{id}/webhook                      (open)
//	/api/v1/workflows/{id}/_internal/lineage            (open)
//	/api/v1/workflows/{id}/_internal/triggered          (open)
//
//	/api/v1/automations                                 (JWT)
//	/api/v1/automations/{id}                            (JWT)
//	/api/v1/automations/{parent_id}/runs                (JWT)
//
//	/api/v1/approvals                                   (JWT)
//	/api/v1/approvals/{approval_id}/decide              (JWT)
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
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/approvals"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/automationoperations"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/handlers"
)

// Options bundles the optional handler set + JWT config. When nil,
// the router only mounts /healthz + /metrics (HTTP-health mode for
// dev / smoke tests where the database is unavailable).
type Options struct {
	JWT          *authmw.JWTConfig
	Workflows    *handlers.CrudHandlers
	Approvals    *approvals.Handlers
	Automations  *automationoperations.Handlers
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
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	if opts == nil || opts.JWT == nil {
		return r
	}

	r.Route("/api/v1", func(api chi.Router) {
		// Open (no JWT) routes — webhook senders + service-to-service
		// internal triggers.
		if opts.Workflows != nil {
			api.Post("/workflows/{id}/webhook", opts.Workflows.TriggerWebhook)
			api.Post("/workflows/{id}/_internal/lineage", opts.Workflows.StartInternalLineageRun)
			api.Post("/workflows/{id}/_internal/triggered", opts.Workflows.StartInternalTriggeredRun)
		}

		// JWT-gated routes.
		api.Group(func(authed chi.Router) {
			authed.Use(authmw.Middleware(opts.JWT))

			if opts.Workflows != nil {
				authed.Get("/workflows", opts.Workflows.ListWorkflows)
				authed.Post("/workflows", opts.Workflows.CreateWorkflow)
				authed.Get("/workflows/{id}", opts.Workflows.GetWorkflow)
				authed.Patch("/workflows/{id}", opts.Workflows.UpdateWorkflow)
				authed.Delete("/workflows/{id}", opts.Workflows.DeleteWorkflow)
				authed.Get("/workflows/{id}/runs", opts.Workflows.ListRuns)
				authed.Post("/workflows/{id}/runs", opts.Workflows.StartManualRun)
				authed.Post("/workflows/approvals/{approval_id}/continue", opts.Workflows.ContinueAfterApproval)
			}

			if opts.Automations != nil {
				authed.Get("/automations", opts.Automations.ListItems)
				authed.Post("/automations", opts.Automations.CreateItem)
				authed.Get("/automations/{id}", opts.Automations.GetItem)
				authed.Get("/automations/{parent_id}/runs", opts.Automations.ListSecondary)
				authed.Post("/automations/{parent_id}/runs", opts.Automations.CreateSecondary)
			}

			if opts.Approvals != nil {
				authed.Get("/approvals", opts.Approvals.ListApprovals)
				authed.Post("/approvals", opts.Approvals.CreateApproval)
				authed.Post("/approvals/{approval_id}/decide", opts.Approvals.DecideApproval)
			}
		})
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
