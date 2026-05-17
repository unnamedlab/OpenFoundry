// Package server wires the chi router for audit-compliance-service.
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
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/lineagedeletion"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/retentionpolicy"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/sds"
)

// Subsystems bundles the per-subsystem handler sets so server.New
// stays tidy as the surface grows.
type Subsystems struct {
	Audit           *handlers.Handlers
	SDS             *sds.Handlers
	Retention       *retentionpolicy.Handlers
	LineageDeletion *lineagedeletion.Handlers
}

func New(cfg *config.Config, jwt *authmw.JWTConfig, sub *Subsystems, m *observability.Metrics, probes ...capabilities.DependencyProbe) *http.Server {
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, chimw.Recoverer, chimw.Compress(5))
	r.Use(chimw.Timeout(30 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(cfg.Service.Name, cfg.Service.Version))
	})
	r.Method(http.MethodGet, "/metrics", m.Handler())

	// Capability registry — see docs/agent-automation/AGENT-CAPABILITIES-ROADMAP.md (M1.1).
	// M1.2: caller-supplied probes (PG/Cassandra/Kafka/...) feed `/_meta/health`.
	caps := capabilities.New(cfg.Service.Name, cfg.Service.Version)
	for _, p := range probes {
		caps.RegisterDependency(p)
	}
	caps.Mount(r)

	// ── Audit-events append (anonymous; gateway / in-cluster posts) ──
	if sub.Audit != nil {
		r.Post("/api/v1/audit/events", sub.Audit.AppendEvent)
	}

	// ── SDS scan-only endpoint (anonymous, like Rust impl) ───────────
	if sub.SDS != nil {
		r.Post("/api/v1/sds/scan", sub.SDS.ScanSensitiveData)
	}

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(authmw.Middleware(jwt))

		// ── Audit ledger ─────────────────────────────────────────────
		if sub.Audit != nil {
			api.Get("/audit/overview", sub.Audit.GetOverview)
			api.Get("/audit/events", sub.Audit.ListEvents)
			api.Get("/audit/events/{id}", sub.Audit.GetEvent)
			api.Get("/audit/anomalies", sub.Audit.ListAnomalies)
			api.Get("/audit/collectors", sub.Audit.ListCollectors)
			api.Get("/audit/delivery/destinations", sub.Audit.ListAuditDeliveryDestinations)
			api.Post("/audit/delivery/destinations", sub.Audit.CreateAuditDeliveryDestination)
			api.Post("/audit/delivery/destinations/{id}/validate", sub.Audit.ValidateAuditDeliveryDestination)
			api.Post("/audit/delivery/destinations/{id}/backfill", sub.Audit.BackfillAuditDeliveryDestination)
			api.Get("/audit/delivery/files", sub.Audit.ListAuditDeliveryFiles)
			api.Get("/audit/delivery/files/{id}/content", sub.Audit.GetAuditDeliveryFileContent)
			// Legacy list endpoint (pre-existing).
			api.Get("/audit-events", sub.Audit.ListAuditEvents)

			api.Get("/audit/policies", sub.Audit.ListAuditPolicies)
			api.Post("/audit/policies", sub.Audit.CreateAuditPolicy)
			api.Patch("/audit/policies/{id}", sub.Audit.UpdateAuditPolicy)
			api.Get("/audit-policies", sub.Audit.ListAuditPolicies)

			api.Get("/audit/reports", sub.Audit.ListComplianceReports)
			api.Post("/audit/reports", sub.Audit.GenerateReport)
			api.Get("/compliance-reports", sub.Audit.ListComplianceReports)

			api.Post("/audit/gdpr/export", sub.Audit.ExportSubjectData)
		}

		// ── Retention policy (P4 surface) ────────────────────────────
		if sub.Retention != nil {
			api.Get("/retention/policies", sub.Retention.ListPolicies)
			api.Post("/retention/policies", sub.Retention.CreatePolicyHandler)
			api.Get("/retention/policies/{id}", sub.Retention.GetPolicyHandler)
			api.Put("/retention/policies/{id}", sub.Retention.UpdatePolicyHandler)
			api.Patch("/retention/policies/{id}", sub.Retention.UpdatePolicyHandler)
			api.Delete("/retention/policies/{id}", sub.Retention.DeletePolicyHandler)
			api.Get("/retention/jobs", sub.Retention.ListJobs)
			api.Post("/retention/jobs", sub.Retention.RunJobHandler)
			api.Get("/datasets/{dataset_id}/retention", sub.Retention.GetDatasetRetention)
			api.Get("/transactions/{transaction_id}/retention", sub.Retention.GetTransactionRetention)
			api.Get("/datasets/{rid}/applicable-policies", sub.Retention.ApplicablePolicies)
			api.Get("/datasets/{rid}/retention-preview", sub.Retention.RetentionPreviewHandler)
		}

		// ── Legacy retention-policy aliases (pre-existing) ───────────
		if sub.Audit != nil {
			api.Get("/retention-policies", sub.Audit.ListRetentionPolicies)
			api.Post("/retention-policies", sub.Audit.CreateRetentionPolicy)
			api.Get("/retention-policies/{id}", sub.Audit.GetRetentionPolicy)
			api.Patch("/retention-policies/{id}", sub.Audit.UpdateRetentionPolicy)
			api.Get("/retention-jobs", sub.Audit.ListRetentionJobs)
		}

		// ── SDS subsystem (auth-only — scan-only path is anonymous) ──
		if sub.SDS != nil {
			api.Post("/sds/jobs", sub.SDS.RunScanJob)
			api.Patch("/sds/issues/{issue_id}", sub.SDS.MarkIssue)
			api.Post("/sds/rules", sub.SDS.CreateRemediationRule)
		}
		// Legacy list endpoints.
		if sub.Audit != nil {
			api.Get("/sds-scan-jobs", sub.Audit.ListSDSScanJobs)
			api.Get("/sds-scan-jobs/{job_id}/issues", sub.Audit.ListSDSIssues)
			api.Get("/sds-remediation-rules", sub.Audit.ListSDSRemediationRules)
		}

		// ── Lineage deletion ─────────────────────────────────────────
		if sub.LineageDeletion != nil {
			api.Post("/lineage/deletions", sub.LineageDeletion.RequestDeletion)
		}
		if sub.Audit != nil {
			api.Get("/lineage-deletion-requests", sub.Audit.ListLineageDeletionRequests)
			api.Post("/lineage-deletion-requests", sub.Audit.CreateLineageDeletionRequest)
		}

		if sub.Audit != nil {
			api.Get("/saga-audit-events", sub.Audit.ListSagaAuditEvents)
		}
	})

	if _, err := caps.IngestChiRoutes(r, capabilities.IngestOptions{
		IDPrefix:  "audit-compliance",
		AuthPaths: []string{"/api/v1"},
		Tags:      []string{"audit"},
	}); err != nil {
		panic("audit-compliance-service: capability ingest failed: " + err.Error())
	}

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
