// Package server wires the HTTP surface for pipeline-build-service.
//
// URL grid mirrors the Rust router 1:1:
//
//   - /api/v1/builds                          (builds_v1)
//   - /api/v1/builds/{id}                     (builds_v1)
//   - /api/v1/builds/{id}/abort               (builds_v1)
//   - /api/v1/builds/{id}/jobs                (job_logs)
//   - /api/v1/jobs/{id}                       (job_logs)
//   - /api/v1/jobs/{id}/logs                  (job_logs)
//   - /api/v1/jobs/{id}/logs/stream           (job_logs SSE)
//   - /api/v1/pipelines                       (legacy CRUD)
//   - /api/v1/pipelines/{id}                  (legacy CRUD)
//   - /api/v1/pipelines/{id}/runs             (runs)
//   - /api/v1/pipelines/{id}/runs/{run_id}    (runs)
//   - /api/v1/pipelines/{id}/runs/{run_id}/retry  (runs)
//   - /api/v1/pipelines/{id}/runs/{run_id}/cancel (runs)
//   - /api/v1/dry-run/resolve                 (dry_run)
//   - /api/v1/dry-run/validate                (dry_run)
//   - /api/v1/execute                         (execute)
//   - /api/v1/data-integration/spark-runs                                          (spark_runs)
//   - /api/v1/data-integration/spark-runs/{id}                                     (spark_runs)
//   - /api/v1/data-integration/pipelines/{id}/runs/{run_id}/spec                   (spark_runs)
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/handler"
)

func New(cfg *config.Config, m *observability.Metrics) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           BuildRouter(cfg, m),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, m *observability.Metrics) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		// Builds (v1).
		api.Get("/builds", handler.ListBuilds)
		api.Post("/builds", handler.CreateBuild)
		api.Get("/builds/{id}", handler.GetBuild)
		api.Post("/builds/{id}/abort", handler.AbortBuild)
		api.Get("/builds/{id}/jobs", handler.ListJobs)

		// Jobs + logs.
		api.Get("/jobs/{id}", handler.GetJob)
		api.Get("/jobs/{id}/logs", handler.ListJobLogs)
		api.Get("/jobs/{id}/logs/stream", handler.StreamJobLogs)

		// Dry-run + execute.
		api.Post("/dry-run/resolve", handler.DryRunResolve)
		api.Post("/dry-run/validate", handler.DryRunValidate)
		api.Post("/execute", handler.ExecutePipeline)

		// Pipelines (legacy CRUD + runs).
		api.Get("/pipelines", handler.ListPipelines)
		api.Post("/pipelines", handler.CreatePipeline)
		api.Get("/pipelines/{id}", handler.GetPipeline)
		api.Patch("/pipelines/{id}", handler.UpdatePipeline)
		api.Put("/pipelines/{id}", handler.UpdatePipeline)
		api.Delete("/pipelines/{id}", handler.DeletePipeline)

		api.Get("/pipelines/{id}/runs", handler.ListPipelineRuns)
		api.Post("/pipelines/{id}/runs", handler.TriggerPipelineRun)
		api.Get("/pipelines/{id}/runs/{run_id}", handler.GetPipelineRun)
		api.Post("/pipelines/{id}/runs/{run_id}/retry", handler.RetryPipelineRun)
		api.Post("/pipelines/{id}/runs/{run_id}/cancel", handler.CancelPipelineRun)

		// SparkApplication-backed runs (FASE 3 / Tarea 3.4).
		api.Route("/data-integration", func(di chi.Router) {
			di.Get("/spark-runs", handler.ListSparkRuns)
			di.Post("/spark-runs", handler.SubmitSparkRun)
			di.Get("/spark-runs/{id}", handler.GetSparkRun)
			di.Get("/pipelines/{id}/runs/{run_id}/spec", handler.GetSpecForRun)
		})
	})

	return r
}
