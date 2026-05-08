// Command ingestion-replication-service hosts the foundation slice of
// the Foundry ingestion + replication runtime (Kafka/Flink jobs +
// streaming + cdc_metadata).
//
// Runtime scope: ingest_jobs CRUD plus streaming/CDC control-plane
// provisioning through Kafka and Flink runtime adapters.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/reconcile"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/server"
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

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	runtime := handlers.NewProductionStreamingRuntime(
		&handlers.HTTPKafkaAdmin{
			BaseURL:          os.Getenv("KAFKA_RUNTIME_URL"),
			BootstrapServers: splitCSV(os.Getenv("KAFKA_BOOTSTRAP_SERVERS")),
		},
		&handlers.HTTPFlinkDeployer{BaseURL: os.Getenv("FLINK_RUNTIME_URL")},
	)
	store := &repo.Repo{Pool: pool}
	// Reconciler defaults to LoggingApplier (no-op). When
	// INGESTION_CONTROL_PLANE_URL is set, swap in the HTTP applier that talks
	// to the Kubernetes-backed control plane (mirrors the Rust hot-standby
	// reconcile loop in services/ingestion-replication-service/src/reconcile.rs).
	reconciler := &reconcile.Reconciler{Logger: log}
	if cpURL := os.Getenv("INGESTION_CONTROL_PLANE_URL"); cpURL != "" {
		reconciler.Applier = &reconcile.HTTPApplier{BaseURL: cpURL, Logger: log}
	}
	_ = reconciler // wired for future reconcile-loop hookup; keeps applier reachable.
	h := &handlers.Handlers{Repo: store, Runtime: runtime}
	metrics := observability.NewMetrics()

	// IRF-9: schema-validation + history endpoints, backed by the
	// shared event-bus-control schema-registry helpers (parses Avro,
	// runs Confluent compatibility, produces the canonical fingerprint).
	//
	// IRF-8: stream-branch CRUD plus best-effort cold-tier bridge to
	// dataset-versioning-service when DATASET_SERVICE_URL is set.
	branches := &handlers.BranchesHandler{Store: store}
	if cfg.DatasetServiceURL != "" {
		branches.Cold = &handlers.HTTPColdTierBridge{BaseURL: cfg.DatasetServiceURL}
	}
	streamingMeta := server.StreamingMetadata{
		Schemas: &handlers.SchemasHandler{
			Store:    store,
			Registry: handlers.BusControlSchemaRegistry{},
		},
		Branches: branches,
	}
	srv := server.New(cfg, jwt, h, metrics, streamingMeta)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
