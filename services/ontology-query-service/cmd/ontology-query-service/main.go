// Command ontology-query-service hosts the read path of the ontology
// plane (per S1.5 of the Cassandra-Foundry parity plan).
//
// The read endpoints are backed by storage-abstraction stores. In
// production these are cassandra-kernel ObjectStore/LinkStore/SchemaStore
// instances; tests can inject fakes through handlers.AppState. Per the Rust
// S1.5.e note this service has no SQL surface; the schema lives in Cassandra.
//
// Startup also applies the read-side Cassandra DDL via
// `cassandrakernel.Apply` so a fresh keyspace is functional after the
// first boot; the migrations are idempotent (`CREATE TABLE IF NOT
// EXISTS`). Disable with APPLY_CASSANDRA_MIGRATIONS=false when the
// operator owns DDL out-of-band.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/gocql/gocql"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
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

	state, session, closeStores, err := buildStoreState(ctx, cfg, log)
	if err != nil {
		log.Error("cassandra store wiring failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if closeStores != nil {
		defer closeStores()
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	h := handlers.New(state)
	metrics := observability.NewMetrics()

	var deps []capabilities.DependencyProbe
	if session != nil {
		deps = append(deps, probes.Cassandra("ontology-query", session))
	}

	srv := server.New(cfg, jwt, h, metrics, deps...)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func buildStoreState(ctx context.Context, cfg *config.Config, log *slog.Logger) (handlers.AppState, *gocql.Session, func(), error) {
	if strings.TrimSpace(cfg.CassandraContactPoints) == "" {
		if cfg.DevMode {
			log.Warn("OF_DEV_STUB_MODE enabled with CASSANDRA_CONTACT_POINTS unset — object reads will surface 500 until the operator wires storage")
			return handlers.AppState{}, nil, nil, nil
		}
		return handlers.AppState{}, nil, nil, errors.New("CASSANDRA_CONTACT_POINTS is required; set OF_DEV_STUB_MODE=true only for explicit local/test execution")
	}

	cluster, err := cassandrakernel.FromEnv()
	if err != nil {
		return handlers.AppState{}, nil, nil, err
	}
	if strings.TrimSpace(cluster.Keyspace) == "" {
		cluster.Keyspace = cfg.CassandraKeyspace
	}
	session, err := cluster.Connect()
	if err != nil {
		return handlers.AppState{}, nil, nil, err
	}
	close := func() { session.Close() }

	if cfg.ApplyMigrations {
		if err := cassandrakernel.Apply(session, cluster.Keyspace, cassandrakernel.OntologyObjectStoreMigrations(cluster.Keyspace)); err != nil {
			close()
			return handlers.AppState{}, nil, nil, err
		}
		if err := cassandrakernel.Apply(session, cluster.Keyspace, cassandrakernel.OntologyLinkStoreMigrations(cluster.Keyspace)); err != nil {
			close()
			return handlers.AppState{}, nil, nil, err
		}
		log.Info("cassandra migrations applied", slog.String("keyspace", cluster.Keyspace))
	}

	select {
	case <-ctx.Done():
		close()
		return handlers.AppState{}, nil, nil, ctx.Err()
	default:
	}

	state := handlers.AppState{
		Objects: cassandrakernel.NewObjectStoreWithKeyspace(session, cluster.Keyspace),
		Links:   cassandrakernel.NewLinkStoreWithKeyspace(session, cluster.Keyspace),
		Schemas: cassandrakernel.NewSchemaStoreWithKeyspace(session, cluster.Keyspace),
	}
	return state, session, close, nil
}
