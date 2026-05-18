// Command action-log-sink is the `ontology.actions.applied.v1` consumer.
//
// It lands every record in two tiers:
//
//   - Postgres `action_log_events` — queryable hot store fronted by
//     the action-log HTTP API (/api/v1/action-log/events*).
//   - Iceberg `lakekeeper.default.action_log` — durable analytic tier;
//     this is the historical sink the Scala
//     `com.openfoundry.audit.ActionLogStreamSink` mirrored 1:1.
//
// Both targets are optional but the runtime requires at least one. The
// Postgres tier is selected when DATABASE_URL is set; otherwise the
// Iceberg / JSONL writer runs alone (read API routes are not mounted).
//
// Phase B of ADR-0045.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/config"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/runtime"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/server"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/writer"
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

	pool, store, err := buildPostgres(ctx, log)
	if err != nil {
		log.Error("postgres init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if pool != nil {
		defer pool.Close()
	}

	w, err := buildWriter(cfg, store)
	if err != nil {
		log.Error("writer build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = w.Close() }()

	sub, err := databus.NewKafkaSubscriber(cfg.DataBus, config.ConsumerGroup, []string{config.SourceTopic})
	if err != nil {
		log.Error("kafka subscriber init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = sub.Close() }()

	metrics := runtime.NewMetrics()

	var h *handlers.Handlers
	if store != nil {
		h = &handlers.Handlers{Repo: store}
	}
	httpSrv := server.New(cfg.MetricsAddr, cfg.Service.Name, cfg.Service.Version, metrics, h)

	log.Info("action-log-sink starting Kafka -> Writer runtime",
		slog.String("topic", config.SourceTopic),
		slog.String("consumer_group", config.ConsumerGroup),
		slog.String("target_table", config.IcebergCatalog+"."+config.IcebergNamespace+"."+config.IcebergTable),
		slog.Bool("postgres_enabled", store != nil),
		slog.String("metrics_addr", cfg.MetricsAddr))

	var wg sync.WaitGroup
	wg.Add(2)
	httpErr := make(chan error, 1)
	runErr := make(chan error, 1)

	go func() {
		defer wg.Done()
		if err := httpSrv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			httpErr <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := runtime.Run(ctx, cfg, sub, w, metrics, log); err != nil && !errors.Is(err, context.Canceled) {
			runErr <- err
		}
	}()

	select {
	case err := <-httpErr:
		log.Error("metrics http server failed", slog.String("error", err.Error()))
		cancel()
	case err := <-runErr:
		log.Error("kafka runtime failed", slog.String("error", err.Error()))
		cancel()
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}
	wg.Wait()
}

// buildPostgres returns (pool, repo, nil) when DATABASE_URL is set,
// running migrations on the way out. (nil, nil, nil) when not set —
// the action-log-sink keeps its Iceberg-only behaviour.
func buildPostgres(ctx context.Context, log *slog.Logger) (*pgxpool.Pool, *repo.Repo, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Info("DATABASE_URL not set; query API disabled (Iceberg-only mode)")
		return nil, nil, nil
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, err
	}
	if err := repo.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, err
	}
	return pool, &repo.Repo{Pool: pool}, nil
}

// buildWriter assembles the writer chain. When both Postgres and
// Iceberg (or JSONL) are wired, MultiWriter fans each batch to both.
func buildWriter(cfg *config.Config, store *repo.Repo) (writer.Writer, error) {
	var primary writer.Writer
	switch {
	case cfg.JSONLWriterPath != "":
		jw, err := writer.NewJSONLWriter(cfg.JSONLWriterPath)
		if err != nil {
			return nil, err
		}
		primary = jw
	case cfg.CatalogURL != "":
		primary = writer.NewIcebergWriter(cfg.CatalogURL, cfg.TableWriterURL, cfg.Warehouse)
	}

	if store == nil {
		if primary == nil {
			return nil, errors.New("action-log-sink: no writer configured (set DATABASE_URL or ICEBERG_CATALOG_URL or ACTION_LOG_SINK_JSONL_PATH)")
		}
		return primary, nil
	}

	pgw := writer.NewPostgresWriter(store)
	if primary == nil {
		return pgw, nil
	}
	return writer.NewMultiWriter(pgw, primary), nil
}
