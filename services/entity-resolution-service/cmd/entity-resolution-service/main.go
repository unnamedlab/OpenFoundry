// Command entity-resolution-service hosts the fusion (entity-resolution)
// plane: match rules, merge strategies, jobs, clusters, golden records,
// and the fuzzy-matching engine (blocking + comparator + ML matcher +
// graph resolution).
//
// Architecture + 1 vertical slice scope (this iteration):
//   - Architecture: cmd/main, config, observability, server, models,
//     pgx repo + embedded migrations, JWT-gated /api/v1/fusion router.
//   - Vertical slice: match rules CRUD (list/create/update) +
//     merge strategies CRUD (list/create/update). Both back the
//     simplest control-plane surface every other piece needs to
//     reference (cluster review, job execution, golden-record merge).
//
// Follow-up slices:
//   - jobs handlers + jobs queue model + job lifecycle.
//   - clusters handlers + cluster review/feedback domain.
//   - domain/engine: blocking, comparator, graph_resolution, rule_matcher,
//     ml_matcher; domain/{deduplication, feedback, merge}.
//
// The Rust binary is `fn main(){}` — same pattern as
// authorization-policy-service, the Go port is canonical.
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
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/server"
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
	h := &handlers.Handlers{
		Rules:           &repo.MatchRuleRepo{Pool: pool},
		MergeStrategies: &repo.MergeStrategyRepo{Pool: pool},
		Jobs:            &repo.FusionJobRepo{Pool: pool},
		Clusters:        &repo.ClusterRepo{Pool: pool},
		Review:          &repo.ReviewQueueRepo{Pool: pool},
		Golden:          &repo.GoldenRecordRepo{Pool: pool},
		Overview:        &repo.OverviewRepo{Pool: pool},
	}
	metrics := observability.NewMetrics()

	srv := server.New(cfg, jwt, h, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
