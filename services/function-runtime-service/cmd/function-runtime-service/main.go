// Command function-runtime-service runs the v0 user-function runtime
// (TypeScript + Python stubs) described in
// docs/migration/foundry-functions-runtime-1to1-checklist.md.
//
// When DATABASE_URL is empty the service falls back to an in-memory
// store; this is intended for local development and tests only and
// is logged at startup.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/executor"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/server"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/function-runtime-service/config.yaml", "path to config file")
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

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	store, dependencyProbes, closeStore, err := buildStore(ctx, cfg, log)
	if err != nil {
		log.Error("store build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if closeStore != nil {
		defer closeStore()
	}

	limits := executor.Limits{
		Timeout:        cfg.DefaultExecutorTimeout(),
		MemoryLimitMiB: cfg.Executor.MemoryLimitMiB,
	}
	registry := executor.NewRegistry()
	registry.Register("ts", executor.NewTSStubExecutor(cfg.Executor.NodeBinary, limits))
	registry.Register("python", executor.NewPythonStubExecutor(cfg.Executor.PythonBinary, limits))

	h := &handlers.Handlers{
		Store:          store,
		Exec:           registry,
		DefaultTimeout: cfg.DefaultExecutorTimeout(),
		MaxTimeout:     cfg.MaxExecutorTimeout(),
		Now:            func() time.Time { return time.Now().UTC() },
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, h, metrics, dependencyProbes...)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func buildStore(ctx context.Context, cfg *config.Config, log *slog.Logger) (repo.Store, []capabilities.DependencyProbe, func(), error) {
	if cfg.Database.URL == "" {
		log.Warn("DATABASE_URL unset — using in-memory store (dev/test only)")
		return repo.NewMemoryStore(), nil, nil, nil
	}
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := repo.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	return repo.NewPostgresStore(pool), []capabilities.DependencyProbe{probes.Postgres("primary", pool)}, pool.Close, nil
}
