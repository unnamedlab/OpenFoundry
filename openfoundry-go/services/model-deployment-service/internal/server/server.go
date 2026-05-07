// Package server hosts the substrate HTTP surface for model-deployment-service.
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
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/serving"
	mlhandlers "github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/handlers"
	mlmodels "github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/model-deployment-service/internal/config"
)

var (
	SeedModelID        = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	SeedModelVersionID = uuid.MustParse("10000000-0000-0000-0000-000000000101")
)

func New(cfg *config.Config, m *observability.Metrics) *http.Server {
	r := buildRouter(cfg, m)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, m *observability.Metrics) http.Handler {
	return buildRouter(cfg, m)
}

func buildRouter(cfg *config.Config, m *observability.Metrics) chi.Router {
	deploymentHandler := defaultDeploymentHandler()
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
	mountDeploymentRoutes(r, "/api/v1/deployments", deploymentHandler)
	mountDeploymentRoutes(r, "/api/v1/model-deployment/deployments", deploymentHandler)
	return r
}

func defaultDeploymentHandler() *mlhandlers.DeploymentsHandlers {
	store := mlhandlers.NewFakeDeploymentStore()
	store.SeedModel(mlmodels.RegisteredModel{
		ID:          SeedModelID,
		Name:        "seed-model",
		ProblemType: mlmodels.DefaultProblemType,
		Status:      "active",
	}, mlmodels.ModelVersion{
		ID:            SeedModelVersionID,
		ModelID:       SeedModelID,
		VersionNumber: 1,
		VersionLabel:  "v1",
		Stage:         "production",
	})
	return &mlhandlers.DeploymentsHandlers{Store: store, Runtime: serving.NewFakeDeploymentRuntime()}
}

func mountDeploymentRoutes(r chi.Router, prefix string, h *mlhandlers.DeploymentsHandlers) {
	r.Get(prefix, h.ListDeployments)
	r.Post(prefix, h.CreateDeployment)
	r.Get(prefix+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "id must be a uuid", http.StatusBadRequest)
			return
		}
		h.GetDeployment(w, r, id)
	})
	r.Patch(prefix+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, "id must be a uuid", http.StatusBadRequest)
			return
		}
		h.UpdateDeployment(w, r, id)
	})
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
