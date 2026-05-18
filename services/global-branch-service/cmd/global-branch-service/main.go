// Command global-branch-service is the entrypoint for the OpenFoundry
// global branching service. It hosts the Milestone A surface — global
// branch lifecycle CRUD + per-service participation coordination —
// described in
// docs/migration/foundry-global-branching-1to1-checklist.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/server"
)

// version is injected at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/global-branch-service/config.yaml", "path to config file")
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
	defer func() {
		_ = shutdownTracing(context.Background())
	}()

	metrics := observability.NewMetrics()

	// Database wiring. Empty DSN keeps the binary bootable in smoke
	// tests where Postgres is not provisioned (no product route works
	// without a pool, but /healthz and /metrics still do).
	var (
		pool    *pgxpool.Pool
		dbProbe []capabilities.DependencyProbe
		h       *handler.Handlers
	)
	if cfg.DatabaseURL != "" {
		pool, err = pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Error("pgx pool failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		if err := repo.Migrate(ctx, pool); err != nil {
			log.Error("migrations failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		h = handler.NewHandlers(repo.New(pool))
		dbProbe = append(dbProbe, probes.Postgres("primary", pool))
	} else {
		log.Warn("OF_DATABASE_URL not set; product routes will be unmounted until configured")
	}

	srv, err := server.New(cfg, h, metrics, log, dbProbe...)
	if err != nil {
		log.Error("server build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
