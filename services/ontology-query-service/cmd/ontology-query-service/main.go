// Command ontology-query-service hosts the read path of the ontology
// plane (per S1.5 of the Cassandra-Foundry parity plan).
//
// The read endpoints are backed by storage-abstraction stores. In
// production these are cassandra-kernel ObjectStore/LinkStore/SchemaStore
// instances; tests can inject fakes through handlers.AppState. Per the Rust
// S1.5.e note this service has no SQL surface; the schema lives in Cassandra.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cassandrakernel "github.com/openfoundry/openfoundry-go/libs/cassandra-kernel"
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

	state := handlers.AppState{}
	var closeCassandra func()
	if cfg.CassandraContactPoints == "" {
		log.Warn("CASSANDRA_CONTACT_POINTS unset — object reads will return backend configuration errors")
	} else {
		state, closeCassandra, err = buildStoreState()
		if err != nil {
			log.Error("cassandra store wiring failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer closeCassandra()
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	h := handlers.New(state)
	metrics := observability.NewMetrics()

	srv := server.New(cfg, jwt, h, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func buildStoreState() (handlers.AppState, func(), error) {
	cluster, err := cassandrakernel.FromEnv()
	if err != nil {
		return handlers.AppState{}, nil, err
	}
	session, err := cluster.Connect()
	if err != nil {
		return handlers.AppState{}, nil, err
	}
	closeFn := func() { session.Close() }
	state := handlers.AppState{
		Objects: cassandrakernel.NewObjectStore(session),
		Links:   cassandrakernel.NewLinkStore(session),
		Schemas: cassandrakernel.NewSchemaStore(session),
	}
	return state, closeFn, nil
}
