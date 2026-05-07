// Package server wires the HTTP surface for notebook-runtime-service.
//
// URL grid mirrors `services/notebook-runtime-service/src/handlers/*`
// — same paths, same verbs. Auth-protected routes live under
// `/api/v1`; `/healthz` and `/metrics` stay public.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/notebook-runtime-service/internal/handler"
)

func New(cfg *config.Config, pool *pgxpool.Pool, m *observability.Metrics) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           BuildRouter(cfg, pool, m),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func BuildRouter(cfg *config.Config, pool *pgxpool.Pool, m *observability.Metrics) http.Handler {
	state := &handler.State{Cfg: cfg, Pool: pool}

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

		// Notebook CRUD.
		api.Get("/notebooks", state.ListNotebooks)
		api.Post("/notebooks", state.CreateNotebook)
		api.Get("/notebooks/{notebook_id}", state.GetNotebook)
		api.Put("/notebooks/{notebook_id}", state.UpdateNotebook)
		api.Patch("/notebooks/{notebook_id}", state.UpdateNotebook)
		api.Delete("/notebooks/{notebook_id}", state.DeleteNotebook)

		// Cells.
		api.Post("/notebooks/{notebook_id}/cells", state.AddCell)
		api.Patch("/notebooks/{notebook_id}/cells/{cell_id}", state.UpdateCell)
		api.Delete("/notebooks/{notebook_id}/cells/{cell_id}", state.DeleteCell)

		// Sessions.
		api.Get("/notebooks/{notebook_id}/sessions", state.ListSessions)
		api.Post("/notebooks/{notebook_id}/sessions", state.CreateSession)
		api.Post("/notebooks/{notebook_id}/sessions/{session_id}/stop", state.StopSession)

		// Execute.
		api.Post("/notebooks/{notebook_id}/cells/{cell_id}/execute", state.ExecuteCell)
		api.Post("/notebooks/{notebook_id}/cells/execute-all", state.ExecuteAllCells)

		// Workspace files.
		api.Get("/notebooks/{notebook_id}/workspace", state.ListWorkspaceFiles)
		api.Put("/notebooks/{notebook_id}/workspace", state.UpsertWorkspaceFile)
		api.Delete("/notebooks/{notebook_id}/workspace", state.DeleteWorkspaceFile)

		// Notepad documents + presence + export.
		api.Get("/notepad/documents", state.ListDocuments)
		api.Post("/notepad/documents", state.CreateDocument)
		api.Get("/notepad/documents/{document_id}", state.GetDocument)
		api.Patch("/notepad/documents/{document_id}", state.UpdateDocument)
		api.Delete("/notepad/documents/{document_id}", state.DeleteDocument)
		api.Get("/notepad/documents/{document_id}/presence", state.ListPresence)
		api.Post("/notepad/documents/{document_id}/presence", state.UpsertPresence)
		api.Post("/notepad/documents/{document_id}/export", state.ExportDocument)
	})

	return r
}
