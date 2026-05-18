// Command retrieval-context-service hosts the document-intelligence
// HTTP surface plus the substrate routes (healthz, metrics, auth
// probe). The knowledge-base + conversation surfaces live in
// knowledge-index-service and agent-runtime-service respectively;
// here we own only document-intelligence jobs, status events and
// structured extractions.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/server"
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

	if cfg.DatabaseURL == "" {
		log.Error("DATABASE_URL unset — refusing to start")
		os.Exit(1)
	}
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

	metrics := observability.NewMetrics()
	store := repo.NewPgStore(pool)
	deps := server.Deps{
		Jobs: &handlers.Jobs{Store: store, Logger: log},
		JWT:  authmw.NewJWTConfig(cfg.JWTSecret),
	}

	srv := server.New(cfg, deps, metrics, probes.Postgres("primary", pool))
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
