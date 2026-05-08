// Command sql-bi-gateway-service runs two concurrent surfaces:
//
//  1. Arrow Flight SQL gRPC listener on cfg.Port (default 50133) —
//     primary edge surface for Tableau / Superset / JDBC BI clients.
//     Substrate-only in the Go port until the proxy bindings land
//     (see internal/flightsql for the full divergence note).
//
//  2. HTTP side router on cfg.HealthzPort (default 50134) — exposes
//     /healthz, the saved-queries CRUD and the warehousing / tabular
//     routes absorbed via ADR-0030.
//
// The saved-queries Postgres pool is connected best-effort: if the
// CNPG cluster is unreachable the side router still serves /healthz
// so the Flight SQL surface stays available during BI database
// outages. Mirrors the Rust main.rs `match PgPoolOptions::connect`.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/flightsql"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/server"
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

	metrics := observability.NewMetrics()

	pool := connectPoolBestEffort(ctx, cfg, log)
	defer func() {
		if pool != nil {
			pool.Close()
		}
	}()

	httpSrv := server.NewHTTPServer(cfg, server.Deps{
		Pool:    pool,
		Metrics: metrics,
		Log:     log,
	})
	flight := flightsql.New(cfg, log)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		log.Info("http side router listening", slog.String("addr", httpSrv.Addr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		if err := flight.ListenAndServe(ctx); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-errCh:
		log.Error("listener exited", slog.String("error", err.Error()))
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = flight.Stop()
	wg.Wait()
}

// connectPoolBestEffort dials the saved-queries Postgres cluster.
// Returns nil (NOT a fatal error) if the connection cannot be made,
// so the gateway still serves /healthz + the Flight SQL surface
// during BI-database outages. Matches Rust main.rs.
func connectPoolBestEffort(ctx context.Context, cfg *config.Config, log *slog.Logger) *pgxpool.Pool {
	if cfg.DatabaseURL == "" {
		log.Warn("DATABASE_URL unset; HTTP router will only expose /healthz")
		return nil
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Warn("invalid DATABASE_URL; HTTP router will only expose /healthz",
			slog.String("error", err.Error()))
		return nil
	}
	poolCfg.MaxConns = 8
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(dialCtx, poolCfg)
	if err != nil {
		log.Warn("saved-queries Postgres unreachable; HTTP router will only expose /healthz",
			slog.String("error", err.Error()))
		return nil
	}
	if err := pool.Ping(dialCtx); err != nil {
		log.Warn("saved-queries Postgres ping failed; HTTP router will only expose /healthz",
			slog.String("error", err.Error()))
		pool.Close()
		return nil
	}
	return pool
}
