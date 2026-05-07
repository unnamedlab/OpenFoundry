// Command reindex-coordinator-service is the Kafka-triggered
// Cassandra reindex coordinator (FASE 4 / Tarea 4.2 Rust→Go port).
//
// Architecture + 1 vertical slice scope (this iteration):
//   - Architecture: cmd/main, config, observability, server
//     (substrate /health + /healthz + /metrics on the Rust default
//     port 9090), pgxpool + embedded migrations.
//   - Vertical slice: pure logic that the wire-compat tests pin —
//     event types, UUID-v5 derivation (job_id, batch event_id),
//     state machine + transition validator, scan decoders +
//     ReindexRecord shape, topic constants.
//
// Follow-up slices (queued):
//   - Postgres-backed JobRepo (CRUD + claim-or-join).
//   - Idempotency store (Postgres `processed_events`).
//   - Cassandra paginated scanner (objects_by_type → objects_by_id).
//   - Kafka subscriber loop (event-bus-data port).
//   - Kafka publisher to ontology.reindex.v1 + ontology.reindex.completed.v1.
//   - Throttle + retry envelope.
//   - Prometheus metric registrations (run counters, page durations).
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
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/server"
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

	if cfg.KafkaBootstrap == "" {
		log.Warn("KAFKA_BOOTSTRAP_SERVERS unset — Kafka consumer/publisher land in a follow-up slice")
	}
	if cfg.CassandraContactPoints == "" {
		log.Warn("CASSANDRA_CONTACT_POINTS unset — Cassandra scanner lands in a follow-up slice")
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
