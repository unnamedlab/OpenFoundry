// Command object-database-service hosts the runtime owner for
// ontology object storage (S1.7 of the Cassandra-Foundry parity plan).
//
// Foundation slice scope:
//   - In-memory ObjectStore / LinkStore that mirror the Rust contract
//     verbatim. CASSANDRA_CONTACT_POINTS is logged as a warning and
//     the binary still boots so dev / CI can exercise the API.
//   - Full HTTP surface (`/health`, `/ready`, `/readiness`, `/status`,
//     `/api/v1/object-database/*`) wired to the in-memory stores.
//
// Follow-up slice: Cassandra-backed ObjectStore / LinkStore wired
// through libs/cassandra-kernel-go (gocql), plus migration-runner
// for the `ontology_objects` + `ontology_indexes` keyspaces. The
// CQL files are copied verbatim under cql/ for that slice.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/server"
	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/storage"
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

	backend := config.BackendInMemory
	if strings.TrimSpace(cfg.CassandraContactPoints) == "" {
		log.Warn("CASSANDRA_CONTACT_POINTS unset — using in-memory stores; production deployments must set Cassandra contact points")
	} else {
		log.Warn("CASSANDRA_CONTACT_POINTS set — Cassandra wiring lands in a follow-up slice; foundation still uses in-memory stores")
	}

	objects := storage.NewInMemoryObjectStore()
	links := storage.NewInMemoryLinkStore()
	h := &handlers.Handlers{Objects: objects, Links: links, Backend: backend}
	metrics := observability.NewMetrics()

	srv := server.New(cfg, h, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
