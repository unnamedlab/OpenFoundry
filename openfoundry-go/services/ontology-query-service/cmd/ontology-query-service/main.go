// Command ontology-query-service hosts the read path of the ontology
// plane (per S1.5 of the Cassandra-Foundry parity plan).
//
// Foundation slice scope: skeleton + 2 Cassandra-bound endpoints that
// 501 until libs/cassandra-kernel-go + libs/storage-abstraction-go
// ports land. Per the Rust S1.5.e note this service has no SQL surface;
// the schema lives in Cassandra.
//
// Follow-up: hook libs/cassandra-kernel-go (already scaffolded under
// identity-federation slice 2), the moka-equivalent in-memory cache,
// the NATS invalidation subscriber on `ontology.write.v1`, and the
// per-handler reads against the ObjectStore trait.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ontology-query-service/internal/server"
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

	if cfg.CassandraContactPoints == "" {
		log.Warn("CASSANDRA_CONTACT_POINTS unset — read endpoints will return 501")
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	h := &handlers.Handlers{}
	metrics := observability.NewMetrics()

	srv := server.New(cfg, jwt, h, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
