// Command ai-evaluation-service hosts the LLM evaluation +
// guardrail benchmarking surface.
//
// The Rust binary is `fn main(){}`; the canonical handlers live in
// services/ai-evaluation-service/src/handlers/evaluations.rs and
// cross-reference libs/ai-kernel domain types (provider, evaluation,
// llm/{gateway, guardrails, runtime}). The Go binary mounts the
// /healthz + /metrics substrate alongside POST
// /api/v1/evaluations/benchmark + POST /api/v1/guardrails/evaluate,
// backed by libs/ai-kernel-go/domain/llm.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/ai-evaluation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ai-evaluation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/ai-evaluation-service/internal/server"
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

	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		var err error
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
	} else {
		log.Warn("DATABASE_URL unset — benchmark route returns 503 until pgx pool is configured")
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, metrics, server.Options{Pool: pool})
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
