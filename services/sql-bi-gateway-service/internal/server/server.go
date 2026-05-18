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

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/repo"
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
func NewHTTPServer(cfg *config.Config, deps Deps, probes ...capabilities.DependencyProbe) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.HealthzPort)
	return &http.Server{
		Addr:              addr,
		Handler:           BuildRouter(cfg, deps, probes...),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// BuildRouter is exposed for in-process tests (parity with
// `tower::ServiceExt::oneshot` callers in Rust).
//
// Auth model: /healthz, /health, /metrics, and the capability registry
// are anonymous (k8s probes + Prometheus scraping). Every /api/v1/*
// route goes through authmw.Middleware; when cfg.AllowAnonymous is true
// (local dev / CI only) the middleware passes unauthenticated requests
// through, otherwise it 401s — matching the Flight SQL surface in
// internal/auth.
func BuildRouter(cfg *config.Config, deps Deps, probes ...capabilities.DependencyProbe) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", handler.Healthz)
	r.Get("/health", handler.Healthz)
	r.Get("/readyz", handler.Healthz)
	if deps.Metrics != nil {
		r.Method(http.MethodGet, "/metrics", deps.Metrics.Handler())
	}

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	var savedRepo repo.SavedQueries
	if deps.Pool != nil {
		savedRepo = repo.NewSavedQueries(deps.Pool)
	}
	saved := handler.New(savedRepo, deps.Log)

	jwtCfg := authmw.NewJWTConfig(cfg.JWTSecret)
	authOpts := authmw.Options{AllowAnonymous: cfg.AllowAnonymous}

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwtCfg, authOpts))

		api.Route("/queries/saved", func(qr chi.Router) {
			qr.Get("/", saved.ListSavedQueries)
			qr.Post("/", saved.CreateSavedQuery)
			qr.Delete("/{id}", saved.DeleteSavedQuery)
		})

		// Warehousing & tabular routes only mount when a pool is wired —
		// matches the Rust `build_router` `match db { None => base, Some
		// => merge(stateful) }` shape.
		if deps.Pool != nil {
			wh := warehousing.New(deps.Pool, deps.Log)
			api.Route("/warehouse", func(api chi.Router) {
				wh.Mount(api)
			})

			tb := tabular.New(deps.Pool, deps.Log)
			api.Route("/tabular", func(api chi.Router) {
				tb.Mount(api)
			})
		}
	})

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "sql-bi-gateway",
		AuthPaths: []string{"/api/v1"},
		Tags:      []string{"bi"},
	}); err != nil {
		panic("sql-bi-gateway-service: capability ingest failed: " + err.Error())
	}

	return r
}
