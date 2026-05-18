// Command network-boundary-service backs the
// `/api/v1/network-boundaries`, `/api/v1/network-boundary` and
// `/api/v1/data-connection/egress-policies` routes fanned out by
// `edge-gateway-service`. Boundary-management routes remain S8.6 / B14
// placeholders; the SG.34 data-connection egress policy surface is
// implemented here against Postgres until consolidation into
// `authorization-policy-service`.
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
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/network-boundary-service/internal/server"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/network-boundary-service/config.yaml", "path to config file")
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

	var (
		store    handler.EgressPolicyStore = handler.NewMemoryEgressPolicyStore()
		pool     *pgxpool.Pool
		depProbe []capabilities.DependencyProbe
	)
	if cfg.Database.URL != "" {
		pool, err = pgxpool.New(ctx, cfg.Database.URL)
		if err != nil {
			log.Error("pgx pool failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		if err := repo.Migrate(ctx, pool); err != nil {
			log.Error("migrations failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		store = repo.NewPgEgressPolicyStore(pool)
		depProbe = append(depProbe, probes.Postgres("primary", pool))
		log.Info("egress policy persistence: postgres", slog.String("dsn_host", redactDSN(cfg.Database.URL)))
	} else {
		log.Warn("DATABASE_URL is empty — egress policies will be lost on shutdown",
			slog.String("store", "in-memory"))
	}

	srv, err := server.New(cfg, metrics, log, store, depProbe...)
	if err != nil {
		log.Error("server build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// redactDSN returns the DSN with the userinfo stripped so DSNs can be
// logged at startup without leaking credentials.
func redactDSN(dsn string) string {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig == nil {
		return "configured"
	}
	return cfg.ConnConfig.Host
}
