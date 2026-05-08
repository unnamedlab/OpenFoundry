// Command reindex-coordinator-service is the Kafka-triggered
// Cassandra reindex coordinator (FASE 4 / Tarea 4.2 Rust→Go port).
//
// Wiring (RC-5 slice):
//   - cmd/main, config, observability, server (substrate /health +
//     /healthz + /metrics on the Rust default port 9090),
//     pgxpool + embedded migrations.
//   - Pure logic: event types, UUID-v5 derivation, state machine,
//     scan decoders, ReindexRecord shape, topic constants
//     (RC-1..RC-3).
//   - Live wiring: Postgres-backed JobRepo + processed_events
//     idempotency store (RC-3), Cassandra paginated scanner (RC-4),
//     Kafka subscriber/publisher (event-bus-data) + Coordinator
//     state machine (this slice, RC-5).
//
// Follow-up slices (queued):
//   - DLQ on poison messages (today they are committed and skipped).
//   - Rate-limit auto-tuning hooks.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/jackc/pgx/v5/pgxpool"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/runtime"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/scan"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/server"
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

	metrics := observability.NewMetrics()
	coordinatorMetrics := runtime.NewMetrics(metrics.Registry)
	srv := server.New(cfg, metrics)

	throttle, err := runtime.ThrottleFromEnv()
	if err != nil {
		log.Error("throttle config invalid", slog.String("error", err.Error()))
		os.Exit(1)
	}

	var (
		coordinatorWG  sync.WaitGroup
		coordinatorErr error
	)
	if cfg.KafkaBootstrap == "" || cfg.CassandraContactPoints == "" {
		log.Warn("KAFKA_BOOTSTRAP_SERVERS / CASSANDRA_CONTACT_POINTS unset — coordinator runtime disabled (substrate-only mode)")
	} else {
		closer, runErr := startCoordinator(ctx, log, cfg, pool, coordinatorMetrics, throttle)
		if runErr != nil {
			log.Error("coordinator startup failed", slog.String("error", runErr.Error()))
			os.Exit(1)
		}
		coordinatorWG.Add(1)
		go func() {
			defer coordinatorWG.Done()
			coordinatorErr = closer()
		}()
	}

	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
	coordinatorWG.Wait()
	if coordinatorErr != nil && !errors.Is(coordinatorErr, context.Canceled) {
		log.Error("coordinator exited with error", slog.String("error", coordinatorErr.Error()))
		os.Exit(1)
	}
}

// startCoordinator wires the live Cassandra session, Kafka publisher
// and Kafka subscriber and returns a closure that blocks on
// runtime.Run. Returning a closure (rather than running synchronously)
// lets main keep the HTTP substrate alive on the same ctx so /metrics
// and /healthz stay reachable while the consumer drains.
func startCoordinator(
	ctx context.Context,
	log *slog.Logger,
	cfg *config.Config,
	pool *pgxpool.Pool,
	metrics *runtime.Metrics,
	throttle runtime.Throttle,
) (func() error, error) {
	cluster := gocql.NewCluster(strings.Split(cfg.CassandraContactPoints, ",")...)
	cluster.Keyspace = cfg.CassandraKeyspace
	cluster.Consistency = gocql.LocalQuorum
	cluster.Timeout = 10 * time.Second
	cluster.ConnectTimeout = 10 * time.Second
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}

	brokers := splitCSV(cfg.KafkaBootstrap)
	bus := databus.NewConfig(brokers, kafkaPrincipalFromEnv(cfg.Service.Name))
	publisher, err := databus.NewKafkaPublisher(bus)
	if err != nil {
		session.Close()
		return nil, err
	}
	subscriber, err := databus.NewKafkaSubscriber(bus, runtime.ConsumerGroup, runtime.SubscribeTopics)
	if err != nil {
		session.Close()
		_ = publisher.Close()
		return nil, err
	}

	jobs := repo.NewJobRepo(pool)
	idem := repo.NewProcessedEventsStore(pool)
	scanner := scan.NewCassandraScanner(session, cfg.CassandraKeyspace)
	coordinator := runtime.NewCoordinator(
		jobs, idem, scanner, publisher, metrics, throttle,
		cfg.OpenLineageNamespace, log,
	)

	log.Info("coordinator runtime ready",
		slog.Any("topics", runtime.SubscribeTopics),
		slog.String("group", runtime.ConsumerGroup),
		slog.String("kafka_bootstrap", cfg.KafkaBootstrap),
		slog.String("cassandra_keyspace", cfg.CassandraKeyspace))

	return func() error {
		defer session.Close()
		defer func() { _ = publisher.Close() }()
		defer func() { _ = subscriber.Close() }()
		return runtime.Run(ctx, coordinator, subscriber)
	}, nil
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// kafkaPrincipalFromEnv mirrors the Rust data_bus_config_from_env
// helper: SCRAM-SHA-512 in prod (KAFKA_SASL_PASSWORD set) with
// optional mechanism / security-protocol overrides; PLAINTEXT for
// dev when no password is configured.
func kafkaPrincipalFromEnv(serviceName string) databus.ServicePrincipal {
	username := firstNonEmpty(os.Getenv("KAFKA_SASL_USERNAME"), os.Getenv("KAFKA_CLIENT_ID"), serviceName)
	password := strings.TrimSpace(os.Getenv("KAFKA_SASL_PASSWORD"))
	var p databus.ServicePrincipal
	if password == "" {
		p = databus.InsecureDev(username)
	} else {
		p = databus.ScramSHA512(username, password)
	}
	if mech := strings.TrimSpace(os.Getenv("KAFKA_SASL_MECHANISM")); mech != "" {
		p.Mechanism = mech
	}
	if proto := strings.TrimSpace(os.Getenv("KAFKA_SECURITY_PROTOCOL")); proto != "" {
		p.SecurityProtocol = proto
	}
	return p
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if s := strings.TrimSpace(x); s != "" {
			return s
		}
	}
	return ""
}
