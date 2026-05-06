// Command workflow-automation-service hosts the consolidated
// workflow / automation / approval plane (S8 / ADR-0030):
//
//   - Workflow definitions/runs + Foundry-pattern condition
//     consumer (legacy workflow-automation-service).
//   - Saga substrate + saga.step.requested.v1 consumer (legacy
//     automation-operations-service).
//   - Human-in-the-loop approvals state machine + approval.*.v1
//     outbox (legacy approvals-service).
//
// Architecture + 1 vertical slice scope (this iteration):
//   - Architecture: cmd/main, config, observability, server (substrate
//     /healthz + /metrics on Rust default port 50137), pgxpool +
//     embedded migrations (9 SQL files copied verbatim) with the
//     four schemas pre-created.
//   - Vertical slice — pure-logic / wire constants:
//     * AutomateConditionV1 / AutomateOutcomeV1 JSON shape.
//     * UUIDv5 namespace pinned + DeriveRunID + DeriveConditionEventID
//       + TenantUUIDFromStr.
//     * Topic constants for all three sub-domains:
//         AUTOMATE_CONDITION_V1 / AUTOMATE_OUTCOME_V1
//         SAGA_STEP_{REQUESTED,COMPLETED,FAILED,COMPENSATED}_V1 +
//         SAGA_{COMPENSATE,COMPLETED,ABORTED}_V1
//         APPROVAL_{REQUESTED,DECIDED,COMPLETED,EXPIRED}_V1
//
// Follow-up slices (queued):
//   - Workflow definitions / runs HTTP handlers + Postgres repo.
//   - Foundry-pattern condition consumer (consumer of automate.condition.v1
//     → outbox automate.outcome.v1; record-before-process via
//     workflow_automation.processed_events).
//   - Saga consumer (libs/saga-go port + automation_operations.processed_events).
//   - Approvals state machine (request/decide/expire) + audit-compliance
//     HTTP client + approval.*.v1 outbox.
//   - approvals-timeout-sweep CronJob companion binary.
//   - NATS subscriber for legacy of.workflows.run.requested subject.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/server"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.FromEnv()
	if err != nil {
		slog.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if cfg.Service.Version == "dev" {
		cfg.Service.Version = version
	}

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)
	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Error("pgx pool failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		if err := repo.Migrate(ctx, pool); err != nil {
			log.Error("migrations failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	} else {
		log.Warn("DATABASE_URL unset — migrations skipped (handlers + consumers land with follow-up slices)")
	}
	if cfg.KafkaBootstrap == "" {
		log.Warn("KAFKA_BOOTSTRAP_SERVERS unset — condition + saga + approval consumers land with follow-up slices")
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
