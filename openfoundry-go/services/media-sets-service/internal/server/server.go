// Package server wires the chi router for media-sets-service.
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
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/handlers"
)

func New(
	cfg *config.Config,
	jwt *authmw.JWTConfig,
	h *handlers.Handlers,
	ap *handlers.AccessPatternHandlers,
	items *handlers.MediaItemHandlers,
	branches *handlers.BranchHandlers,
	txs *handlers.TransactionHandlers,
	ret *handlers.RetentionHandlers,
	m *observability.Metrics,
) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Method(http.MethodGet, "/metrics", m.Handler())

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		api.Get("/media-sets", h.ListMediaSets)
		api.Post("/media-sets", h.CreateMediaSet)
		api.Get("/media-sets/{rid}", h.GetMediaSet)
		api.Patch("/media-sets/{rid}", h.UpdateMediaSet)
		api.Delete("/media-sets/{rid}", h.DeleteMediaSet)

		// Access patterns: register / list / run / per-item shortcut.
		api.Get("/media-sets/{rid}/access-patterns", ap.ListAccessPatterns)
		api.Post("/media-sets/{rid}/access-patterns", ap.RegisterAccessPattern)
		api.Post("/access-patterns/{id}/run", ap.RunAccessPattern)
		api.Get("/items/{rid}/access-patterns/{kind}/url", ap.ItemAccessPatternShortcut)

		// Media items: presigned upload/download, list/get/delete,
		// virtual-item registration, per-item markings override.
		api.Post("/media-sets/{rid}/items", items.PresignUpload)
		api.Get("/media-sets/{rid}/items", items.ListItems)
		api.Post("/media-sets/{rid}/virtual-items", items.RegisterVirtualItem)
		api.Get("/items/{rid}", items.GetItem)
		api.Delete("/items/{rid}", items.DeleteItem)
		api.Get("/items/{rid}/download", items.PresignDownload)
		api.Patch("/items/{rid}/markings", items.PatchMarkings)

		// Branches: list/create/delete/reset/merge.
		api.Get("/media-sets/{rid}/branches", branches.ListBranches)
		api.Post("/media-sets/{rid}/branches", branches.CreateBranch)
		api.Delete("/media-sets/{rid}/branches/{name}", branches.DeleteBranch)
		api.Post("/media-sets/{rid}/branches/{name}/reset", branches.ResetBranch)
		api.Post("/media-sets/{rid}/branches/{name}/merge", branches.MergeBranch)

		// Transactions: open/commit/abort/list.
		api.Post("/media-sets/{rid}/transactions", txs.OpenTransaction)
		api.Get("/media-sets/{rid}/transactions", txs.ListTransactions)
		api.Post("/transactions/{rid}/commit", txs.CommitTransaction)
		api.Post("/transactions/{rid}/abort", txs.AbortTransaction)

		// Retention window PATCH (synchronous reaper hook).
		api.Patch("/media-sets/{rid}/retention", ret.PatchRetention)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
