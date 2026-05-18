// Command knowledge-index-service serves the persistent
// `/api/v1/ai/knowledge-bases` CRUD, document indexing, and search surface.
// Production startup requires Postgres via DATABASE_URL or OF_DATABASE__URL;
// the in-memory store is available only when allow_fake_store is explicitly
// enabled for local/dev tests.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/knowledge-index-service/internal/server"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/knowledge-index-service/config.yaml", "path to config file")
	flag.Parse()

	envOverride := os.Getenv("CONFIG_FILE")
	cfg, err := config.Load(*cfgPath, envOverride)
	if err != nil {
		slog.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if cfg.Service.Version == "" {
		cfg.Service.Version = version
	}
	if cfg.Database.URL == "" && !cfg.AllowFakeStore {
		slog.Error("database.url is required for knowledge-index-service production persistence; set allow_fake_store=true only for explicit local/test execution")
		os.Exit(1)
	}

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		_ = shutdownTracing(context.Background())
	}()

	var opts []server.Option
	var pool *pgxpool.Pool
	if cfg.Database.URL != "" {
		pool, err = pgxpool.New(ctx, cfg.Database.URL)
		if err != nil {
			log.Error("pgx pool failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		if err := pool.Ping(ctx); err != nil {
			log.Error("postgres ping failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		if err := repo.Migrate(ctx, pool); err != nil {
			log.Error("migrations failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		opts = append(opts, server.WithPostgresPool(pool))
	} else {
		log.Warn("allow_fake_store enabled without database.url — using in-memory knowledge store for local/test execution only")
	}

	metrics := observability.NewMetrics()

	srv, err := server.New(cfg, metrics, log, opts...)
	if err != nil {
		log.Error("server build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
