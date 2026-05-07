// Package server wires the substrate HTTP surface for ontology-actions-service.
//
// Mirrors `services/ontology-actions-service/src/lib.rs::build_router`
// 1:1 at the URL level: every action / funnel / function / rule path
// from the Rust router is mounted here, JWT-protected, and responds
// with the same envelope shape so the Rust integration tests (smoke
// + absorbed_routes_require_bearer_token) pass against the Go binary
// without modification.
//
// When an `*ontologykernel.AppState` is supplied, routes whose kernel
// handler has been ported (currently `GET /storage/insights`) are
// delegated to the kernel package; the rest stay on the substrate
// handlers in `internal/handler` until their bounded context is
// migrated.
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
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	kernelactions "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/actions"
	kernelfunctions "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/functions"
	kernelfunnel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/funnel"
	kernelrules "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/rules"
	kernelstorage "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/handlers/storage"
	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/handler"
)

// New builds the HTTP server bound to cfg.Server.{Host,Port}.
func New(cfg *config.Config, state *ontologykernel.AppState, m *observability.Metrics) *http.Server {
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	return &http.Server{
		Addr:              addr,
		Handler:           BuildRouter(cfg, state, m),
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// BuildRouter exposes the chi.Router for in-process tests
// (parity with `tower::ServiceExt::oneshot` callers in Rust).
func BuildRouter(cfg *config.Config, state *ontologykernel.AppState, m *observability.Metrics) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer)
	r.Use(chimw.Timeout(30 * time.Second))

	// Public probes.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	if m != nil {
		r.Method(http.MethodGet, "/metrics", m.Handler())
	}

	// /api/v1/ontology/* requires a Bearer token. The Rust crate
	// applies `auth_middleware::layer::auth_layer` exactly here.
	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	r.Route("/api/v1/ontology", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))
		mountActions(api, state)
		mountFunnel(api, state)
		mountFunctions(api, state)
		mountRules(api, state)
	})

	return r
}

func mountActions(r chi.Router, state *ontologykernel.AppState) {
	if state != nil {
		// Real kernel handlers (Phase 5A — CRUD + applicable + metrics
		// + what-if + upload). Execute / validate / inline-edit
		// surfaces still surface HTTP 501 with a clear sentinel until
		// Phase 5B (plan_action substrate) lands.
		kernelactions.Mount(r, state)
		return
	}
	// Substrate fallback (no DB / smoke tests).
	r.Get("/actions", handler.ListActionTypes)
	r.Post("/actions", handler.CreateActionType)
	r.Get("/actions/{id}", handler.GetActionType)
	r.Put("/actions/{id}", handler.UpdateActionType)
	r.Delete("/actions/{id}", handler.DeleteActionType)
	r.Post("/actions/{id}/validate", handler.ValidateAction)
	r.Post("/actions/{id}/execute", handler.ExecuteAction)
	r.Get("/actions/{id}/metrics", handler.GetActionMetrics)
	r.Post("/actions/{id}/execute-batch", handler.ExecuteActionBatch)
	r.Get("/actions/{id}/what-if", handler.ListActionWhatIfBranches)
	r.Post("/actions/{id}/what-if", handler.CreateActionWhatIfBranch)
	r.Delete("/actions/{id}/what-if/{branch_id}", handler.DeleteActionWhatIfBranch)
	r.Post("/types/{type_id}/properties/{property_id}/objects/{obj_id}/inline-edit", handler.ExecuteInlineEdit)
	r.Post("/types/{type_id}/inline-edit-batch", handler.ExecuteInlineEditBatch)
	r.Get("/types/{type_id}/applicable-actions", handler.ListApplicableActions)
	r.Post("/actions/uploads", handler.UploadActionAttachment)
}

func mountFunnel(r chi.Router, state *ontologykernel.AppState) {
	if state != nil {
		// /storage/insights — kernel handler (Phase 1).
		r.Get("/storage/insights", kernelstorage.GetStorageInsights(state))
		// Funnel sources + runs + health — kernel handlers (Phase 4).
		// Mount overrides /funnel/* with the 1:1 ports.
		kernelfunnel.Mount(r, state)
		return
	}
	// Fallback to substrate stubs when no DB is wired (smoke tests).
	r.Get("/funnel/health", handler.GetFunnelHealth)
	r.Get("/storage/insights", handler.GetStorageInsights)
	r.Get("/funnel/sources", handler.ListFunnelSources)
	r.Post("/funnel/sources", handler.CreateFunnelSource)
	r.Get("/funnel/sources/{id}", handler.GetFunnelSource)
	r.Patch("/funnel/sources/{id}", handler.UpdateFunnelSource)
	r.Delete("/funnel/sources/{id}", handler.DeleteFunnelSource)
	r.Get("/funnel/sources/{id}/health", handler.GetFunnelSourceHealth)
	r.Post("/funnel/sources/{id}/run", handler.TriggerFunnelRun)
	r.Get("/funnel/sources/{id}/runs", handler.ListFunnelRuns)
	r.Get("/funnel/sources/{source_id}/runs/{run_id}", handler.GetFunnelRun)
}

func mountFunctions(r chi.Router, state *ontologykernel.AppState) {
	if state != nil {
		// Real kernel handlers (Phase 3 — 1:1 of handlers/functions.rs).
		// All 10 endpoints + the static authoring surface land here.
		kernelfunctions.Mount(r, state)
		return
	}
	// Fallback to substrate stubs while the binary runs without a DB
	// (smoke tests + auth-contract integration tests).
	r.Get("/functions", handler.ListFunctionPackages)
	r.Post("/functions", handler.CreateFunctionPackage)
	r.Get("/functions/authoring-surface", handler.GetFunctionAuthoringSurface)
	r.Get("/functions/{id}", handler.GetFunctionPackage)
	r.Patch("/functions/{id}", handler.UpdateFunctionPackage)
	r.Delete("/functions/{id}", handler.DeleteFunctionPackage)
	r.Post("/functions/{id}/validate", handler.ValidateFunctionPackage)
	r.Post("/functions/{id}/simulate", handler.SimulateFunctionPackage)
	r.Get("/functions/{id}/runs", handler.ListFunctionPackageRuns)
	r.Get("/functions/{id}/metrics", handler.GetFunctionPackageMetrics)
}

func mountRules(r chi.Router, state *ontologykernel.AppState) {
	if state != nil {
		// Real kernel handlers (Phase 2 — 1:1 of handlers/rules.rs).
		// Mount the same 12 endpoints with the same path / verb.
		kernelrules.Mount(r, state)
		return
	}
	// Fallback to the substrate stubs while the binary runs without
	// DATABASE_URL (smoke tests + integration tests of the auth contract).
	r.Get("/rules", handler.ListRules)
	r.Post("/rules", handler.CreateRule)
	r.Get("/rules/insights", handler.GetMachineryInsights)
	r.Get("/rules/machinery/queue", handler.GetMachineryQueue)
	r.Patch("/rules/machinery/queue/{id}", handler.UpdateMachineryQueueItem)
	r.Get("/rules/{id}", handler.GetRule)
	r.Patch("/rules/{id}", handler.UpdateRule)
	r.Delete("/rules/{id}", handler.DeleteRule)
	r.Post("/rules/{id}/simulate", handler.SimulateRule)
	r.Post("/rules/{id}/apply", handler.ApplyRule)
	r.Get("/types/{type_id}/rules", handler.ListRulesForObjectType)
	r.Get("/objects/{obj_id}/rule-runs", handler.ListObjectRuleRuns)
}
