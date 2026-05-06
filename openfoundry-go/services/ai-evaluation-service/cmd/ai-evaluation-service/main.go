// Command ai-evaluation-service hosts the LLM evaluation +
// guardrail benchmarking surface.
//
// Substrate-only port: the Rust binary is `fn main(){}` and the
// evaluations handlers (benchmark_providers + evaluate_guardrails)
// cross-reference libs/ai-kernel domain types (provider, evaluation,
// llm/{gateway, guardrails, runtime}). The Go port lights up
// /healthz + /metrics and applies the 2 migrations now; the
// /api/v1/evaluations + /api/v1/guardrails routes wire alongside
// the libs/ai-kernel-go/domain/llm slice in a follow-up.
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

	if cfg.DatabaseURL != "" {
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
	} else {
		log.Warn("DATABASE_URL unset — migrations skipped (evaluations handlers wire with libs/ai-kernel-go/domain/llm slice)")
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
