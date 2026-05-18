// Command ai-sink consumes ai.events.v1 and lands every record in two
// tiers:
//
//   - Postgres `ai_events` — queryable hot store fronted by the
//     ai-sink HTTP/JSON API (/api/v1/ai/events*).
//   - Iceberg `lakekeeper.of_ai.{prompts,responses,evaluations,traces}`
//     — durable analytic tier; this is the historical sink the Rust
//     service mirrored 1:1.
//
// Both targets are optional but the runtime requires at least one. The
// Postgres tier is selected when DATABASE_URL is set; otherwise the
// Iceberg writer runs alone (read APIs return 404 by virtue of not
// being mounted).
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
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/runtime"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/server"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/writer"
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

	log.Info("ai-sink starting Kafka -> Writer runtime",
		slog.String("topic", config.SourceTopic),
		slog.String("consumer_group", config.ConsumerGroup),
		slog.String("namespace", config.IcebergCatalog+"."+config.IcebergNamespace),
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
// the ai-sink keeps the Iceberg-only behaviour.
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
// Iceberg are wired, the MultiWriter fans each batch to both. JSONL
// mode (AI_SINK_JSONL_DIR) bypasses Iceberg as before.
func buildWriter(cfg *config.Config, store *repo.Repo) (writer.Writer, error) {
	var primary writer.Writer
	switch {
	case cfg.JSONLWriterDir != "":
		jw, err := writer.NewJSONLWriter(cfg.JSONLWriterDir)
		if err != nil {
			return nil, err
		}
		primary = jw
	case cfg.CatalogURL != "":
		primary = writer.NewIcebergWriterWithAdapter(
			cfg.CatalogURL, cfg.TableWriterURL, cfg.Warehouse, config.IcebergNamespace,
		)
	}

	if store == nil {
		if primary == nil {
			return nil, errors.New("ai-sink: no writer configured (set DATABASE_URL or ICEBERG_CATALOG_URL or AI_SINK_JSONL_DIR)")
		}
		return primary, nil
	}

	pgw := writer.NewPostgresWriter(store)
	if primary == nil {
		return pgw, nil
	}
	return writer.NewMultiWriter(pgw, primary), nil
}
