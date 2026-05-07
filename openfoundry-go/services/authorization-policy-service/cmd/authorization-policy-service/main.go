// Command authorization-policy-service is the writer + custodian of
// every Cedar policy + ABAC rule in the platform (per ADR-0027).
//
// This Go port is the canonical implementation while the Rust crate's
// consolidated binary remains a stub. The live surface includes Cedar policy
// CRUD, tenant-scoped ABAC policy management/evaluation, top-level RBAC
// roles/groups/permissions, governance, checkpoint/purpose, cipher, and
// network-boundary routes.
//
// RBAC source-of-truth split: identity-federation owns identity-local user
// lifecycle, login/session, API-key, and SCIM group administration. This
// service owns authorization-policy RBAC grants used to protect policy
// management and authorization evaluation (tenant-scoped roles, groups,
// permissions, user-role assignments, group membership, and group-role grants).
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
