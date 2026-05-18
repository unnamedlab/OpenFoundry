// Command compute-module-service runs the Compute Modules CRUD and
// project-placement API described in
// docs/migration/foundry-compute-modules-1to1-checklist.md (CM.1).
//
// The entrypoint mirrors docs/templates/service-skeleton: config → observability →
// (optional) pgx pool + migrations → server. When DATABASE_URL is unset
// the service runs against the in-memory repository (smoke / dev mode);
// when set it boots the pgx-backed Repository and applies embedded
// migrations on startup.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/server"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/compute-module-service/config.yaml", "path to config file")
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
	// Allow operators to override DSN via DATABASE_URL without touching
	// the config file; matches the convention used across other Go
	// services and the Helm `envSecrets.DATABASE_URL` wiring.
	if dsn := strings.TrimSpace(os.Getenv("DATABASE_URL")); dsn != "" {
		cfg.DatabaseURL = dsn
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

	var (
		store         repo.Repository
		pool          *pgxpool.Pool
		extraProbes   []capabilities.DependencyProbe
	)
	if cfg.DatabaseURL == "" {
		log.Warn("DATABASE_URL unset — using in-memory repository (smoke mode)")
		store = repo.NewMemoryRepository()
	} else {
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
		store = repo.NewPgRepository(pool)
		extraProbes = append(extraProbes, probes.Postgres("primary", pool))
		log.Info("postgres repository wired", slog.String("dsn_host", redactedDSN(cfg.DatabaseURL)))
	}

	srv, err := server.New(cfg, store, metrics, log, extraProbes...)
	if err != nil {
		log.Error("server build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// redactedDSN strips userinfo and query parameters from the DSN so the
// startup log never leaks credentials.
func redactedDSN(dsn string) string {
	if idx := strings.Index(dsn, "@"); idx >= 0 {
		dsn = dsn[idx+1:]
	}
	if idx := strings.IndexAny(dsn, "?#"); idx >= 0 {
		dsn = dsn[:idx]
	}
	return dsn
}
