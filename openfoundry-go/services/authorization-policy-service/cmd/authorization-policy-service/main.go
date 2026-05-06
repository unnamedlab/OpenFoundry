// Command authorization-policy-service is the writer + custodian of
// every Cedar policy + ABAC rule in the platform (per ADR-0027).
//
// Slice 1 (foundation, this binary): Cedar policy CRUD over Postgres
// with strict schema validation via libs/authz-cedar-go before every
// write; optional NATS publish on `authz.policy.changed` so peer
// services hot-reload via libs/authz-cedar-go's PolicyReloadSubscriber.
//
// The Rust crate is currently `fn main() {}` (per ADR-0030 / S8 / B14
// consolidation pending), so this Go port becomes the canonical
// implementation. RBAC, groups, restricted views, ABAC evaluator,
// security_governance / checkpoints_purpose / cipher /
// network_boundary sub-modules land in follow-up slices.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/server"
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

	var nc *nats.Conn
	if cfg.NATSURL != "" {
		nc, err = nats.Connect(cfg.NATSURL)
		if err != nil {
			log.Error("nats connect failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer nc.Close()
		log.Info("nats connected — reload signals will publish on authz.policy.changed")
	} else {
		log.Info("nats disabled (NATS_URL unset) — reload signals are local-only")
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	r := &repo.Repo{Pool: pool}
	h := handlers.NewHandlers(r, nc)
	metrics := observability.NewMetrics()

	srv := server.New(cfg, jwt, h, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
