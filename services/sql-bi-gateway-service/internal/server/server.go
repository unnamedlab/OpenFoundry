// Package server wires the HTTP side router (port = HealthzPort).
//
// The Flight SQL gRPC primary surface lives in [`internal/flightsql`];
// these two listeners run concurrently in main. URL grid below
// matches services/sql-bi-gateway-service/src/http.rs::build_router
// 1:1 — same paths, same verbs, same status codes.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/tabular"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/warehousing"
)

// Deps is the side-router dependency graph: optional Postgres pool +
// an observability sink + the per-service logger. `Pool` may be nil
// in environments where the saved-queries Postgres cluster is not
// provisioned (smoke clusters, integration tests of the Flight SQL
// surface in isolation), in which case only /healthz, /metrics and
// the seed-only saved-query stubs are exposed — matching the Rust
// behaviour `build_router(None)`.
type Deps struct {
	Pool    *pgxpool.Pool
	Metrics *observability.Metrics
	Log     *slog.Logger
}

// NewHTTPServer builds the chi-backed *http.Server bound to the
// healthz port.
func NewHTTPServer(cfg *config.Config, deps Deps) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.HealthzPort)
	return &http.Server{
		Addr:              addr,
		Handler:           BuildRouter(cfg, deps),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// BuildRouter is exposed for in-process tests (parity with
// `tower::ServiceExt::oneshot` callers in Rust).
func BuildRouter(_ *config.Config, deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", handler.Healthz)
	r.Get("/health", handler.Healthz)
	if deps.Metrics != nil {
		r.Method(http.MethodGet, "/metrics", deps.Metrics.Handler())
	}

	saved := handler.New(deps.Pool, deps.Log)
	r.Route("/api/v1/queries/saved", func(api chi.Router) {
		api.Get("/", saved.ListSavedQueries)
		api.Post("/", saved.CreateSavedQuery)
		api.Delete("/{id}", saved.DeleteSavedQuery)
	})

	// Warehousing & tabular routes only mount when a pool is wired —
	// matches the Rust `build_router` `match db { None => base, Some =>
	// merge(stateful) }` shape.
	if deps.Pool != nil {
		wh := warehousing.New(deps.Pool, deps.Log)
		r.Route("/api/v1/warehouse", func(api chi.Router) {
			wh.Mount(api)
		})

		tb := tabular.New(deps.Pool, deps.Log)
		r.Route("/api/v1/tabular", func(api chi.Router) {
			tb.Mount(api)
		})
	}

	return r
}
