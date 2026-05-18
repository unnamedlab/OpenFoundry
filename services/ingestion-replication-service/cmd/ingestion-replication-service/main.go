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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
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
	reconciler, err := buildReconcilerFromEnv(log, os.Getenv)
	if err != nil {
		log.Error("reconciler configuration failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	h := &handlers.Handlers{Repo: store, Runtime: runtime, Reconciler: reconciler}
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
	irProbes := []capabilities.DependencyProbe{probes.Postgres("primary", pool)}
	if kb := os.Getenv("KAFKA_BOOTSTRAP_SERVERS"); kb != "" {
		irProbes = append(irProbes, probes.Kafka("ingestion-events", splitCSV(kb)))
	}
	srv := server.New(cfg, jwt, h, metrics, streamingMeta, irProbes...)
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

func buildReconcilerFromEnv(log *slog.Logger, getenv func(string) string) (*reconcile.Reconciler, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if log == nil {
		log = slog.Default()
	}
	mode := strings.ToLower(strings.TrimSpace(getenv("INGESTION_RECONCILE_MODE")))
	cpURL := strings.TrimSpace(getenv("INGESTION_CONTROL_PLANE_URL"))
	allowNoop := parseBoolEnv(getenv("INGESTION_ALLOW_NOOP_RECONCILER"))

	if cpURL != "" {
		return &reconcile.Reconciler{Logger: log, Applier: &reconcile.HTTPApplier{
			BaseURL:    cpURL,
			HTTPClient: &http.Client{Timeout: reconcileHTTPTimeout(getenv)},
			Logger:     log,
		}}, nil
	}

	if allowNoop || mode == "noop" || mode == "log" || mode == "dev" {
		log.Warn("INGESTION RECONCILER IS RUNNING IN EXPLICIT NO-OP MODE; CONTROL-PLANE RESOURCES WILL NOT BE MATERIALIZED",
			slog.String("mode", mode),
			slog.Bool("allow_noop", allowNoop),
		)
		return &reconcile.Reconciler{Logger: log, Applier: &reconcile.LoggingApplier{Logger: log}}, nil
	}
	if mode != "" && mode != "http" && mode != "production" {
		return nil, fmt.Errorf("unsupported INGESTION_RECONCILE_MODE %q (use http/production with INGESTION_CONTROL_PLANE_URL, or explicit noop/log/dev)", mode)
	}
	return nil, fmt.Errorf("INGESTION_CONTROL_PLANE_URL is required unless INGESTION_RECONCILE_MODE=noop/log/dev or INGESTION_ALLOW_NOOP_RECONCILER=true is set")
}

func reconcileHTTPTimeout(getenv func(string) string) time.Duration {
	v := strings.TrimSpace(getenv("INGESTION_CONTROL_PLANE_TIMEOUT"))
	if v == "" {
		return reconcile.DefaultHTTPTimeout
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return reconcile.DefaultHTTPTimeout
}

func parseBoolEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}
