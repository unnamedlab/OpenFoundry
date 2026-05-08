// `schedules-tick` is a single-shot CLI that runs one
// [eventscheduler.Scheduler.Tick] and exits.
//
// Mirrors libs/event-scheduler/src/bin/schedules-tick.rs verbatim —
// same env contract, same exit semantics. Intended deployment is a
// Kubernetes CronJob running every minute.
//
// Environment:
//
//   - DATABASE_URL — required, e.g. postgres://user:pw@scheduler-db/foundry.
//   - KAFKA_BOOTSTRAP_SERVERS — required, comma-separated brokers.
//   - KAFKA_SASL_USERNAME / KAFKA_SASL_PASSWORD /
//     KAFKA_SASL_MECHANISM / KAFKA_SECURITY_PROTOCOL — optional;
//     when KAFKA_SASL_PASSWORD is unset we fall back to PLAINTEXT
//     (dev / testcontainer).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	es "github.com/openfoundry/openfoundry-go/libs/event-scheduler"
)

const serviceName = "schedules-tick"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("schedules-tick failed", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL must be set (Postgres connection URL)")
	}
	bootstrap := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	if bootstrap == "" {
		return fmt.Errorf("KAFKA_BOOTSTRAP_SERVERS must be set (comma-separated brokers)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pgCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	pgCfg.MaxConns = 4
	pgCfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	defer pool.Close()

	publisher, err := databus.NewKafkaPublisher(databus.NewConfig(splitCSV(bootstrap), buildPrincipal()))
	if err != nil {
		return fmt.Errorf("build Kafka publisher: %w", err)
	}
	defer func() { _ = publisher.Close() }()

	scheduler := es.NewScheduler(pool, publisher)
	now := time.Now().UTC()

	fired, tickErr := scheduler.Tick(ctx, now)
	if tickErr != nil {
		return fmt.Errorf("scheduler tick failed: %w", tickErr)
	}

	slog.Info("schedules-tick completed", "fired", fired, "now", now.Format(time.RFC3339))
	fmt.Println(fired)
	return nil
}

// splitCSV parses a comma-separated list, trimming whitespace and
// dropping empty entries. Mirrors the helper used by other Go services
// in this repo.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// buildPrincipal mirrors the env contract used by other Go services in
// this repo. With no KAFKA_SASL_PASSWORD we fall back to a PLAINTEXT
// dev principal so testcontainer / docker-compose runs work without
// extra env knobs.
func buildPrincipal() databus.ServicePrincipal {
	password := os.Getenv("KAFKA_SASL_PASSWORD")
	username := defaultStr(os.Getenv("KAFKA_SASL_USERNAME"),
		defaultStr(os.Getenv("KAFKA_CLIENT_ID"), serviceName))
	if password == "" {
		return databus.InsecureDev(username)
	}
	mechanism := defaultStr(os.Getenv("KAFKA_SASL_MECHANISM"), databus.SASLMechanismScramSHA512)
	protocol := defaultStr(os.Getenv("KAFKA_SECURITY_PROTOCOL"), databus.SecurityProtocolSASLSSL)
	return databus.ServicePrincipal{
		Service:          username,
		Password:         password,
		Mechanism:        mechanism,
		SecurityProtocol: protocol,
	}
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
