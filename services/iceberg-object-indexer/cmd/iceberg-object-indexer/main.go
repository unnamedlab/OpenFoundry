// Command iceberg-object-indexer reads an Iceberg table via
// apache/iceberg-go and PUTs each row into object-database-service.
//
// Replaces the Scala IcebergToObjectStoreIndexer (Spark) — same CLI
// surface (--source-table, --target-tenant, --target-type-id,
// --id-column, --object-database-url, --catalog, --catalog-uri,
// --limit, --smoke) plus three new flags that the SparkApplication CR
// used to inject as sparkConf: --catalog-warehouse,
// --catalog-credential, --oauth-token-uri, --oauth-scope. See
// services/iceberg-object-indexer/README.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/runner"
	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/server"
	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/sink"
	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/source"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

const serviceName = "iceberg-object-indexer"

func main() {
	args, err := runner.ParseArgs(serviceName, os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "argument error: %v\n", err)
		os.Exit(2)
	}
	if code := run(args); code != 0 {
		os.Exit(code)
	}
}

func run(args runner.Args) int {
	log := buildLogger(args.LogFormat)
	log.Info("iceberg-object-indexer starting",
		slog.String("version", version),
		slog.String("source_table", args.SourceTable),
		slog.String("target_type_id", args.TargetTypeID),
		slog.Int64("limit", args.Limit),
		slog.Bool("smoke", args.Smoke),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	reg := prometheus.NewRegistry()
	metrics := server.NewMetrics(reg)
	srv := server.New(args.HealthAddr, serviceName, version, reg)

	var wg sync.WaitGroup
	httpErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("health/metrics listening", slog.String("addr", srv.Addr()))
		if err := srv.Run(ctx); err != nil {
			httpErr <- err
		}
		close(httpErr)
	}()

	deps, depErr := buildDeps(ctx, args, log, metrics)
	if depErr != nil {
		log.Error("dependency wiring failed", slog.String("error", depErr.Error()))
		cancel()
		wg.Wait()
		return 1
	}
	defer func() {
		if deps.Source != nil {
			_ = deps.Source.Close()
		}
	}()

	runErr := runner.Run(ctx, args, deps)
	cancel()
	wg.Wait()

	if runErr != nil {
		log.Error("indexing failed", slog.String("error", runErr.Error()))
		return 1
	}
	if err, ok := <-httpErr; ok && err != nil {
		log.Error("health server exited with error", slog.String("error", err.Error()))
		return 1
	}
	return 0
}

func buildDeps(ctx context.Context, args runner.Args, log *slog.Logger, metrics runner.Metrics) (runner.Deps, error) {
	if args.Smoke {
		return runner.Deps{Log: log, Metrics: metrics}, nil
	}
	src, err := source.OpenIceberg(ctx, source.IcebergConfig{
		CatalogName:   args.CatalogName,
		CatalogURI:    args.CatalogURI,
		Warehouse:     args.CatalogWarehouse,
		Credential:    args.CatalogCredential,
		OAuthTokenURI: args.OAuthTokenURI,
		OAuthScope:    args.OAuthScope,
		SourceTable:   args.SourceTable,
	})
	if err != nil {
		return runner.Deps{}, fmt.Errorf("open iceberg source: %w", err)
	}
	sk, err := sink.NewObjectDB(args.ObjectDatabaseURL, args.InternalToken, 0)
	if err != nil {
		_ = src.Close()
		return runner.Deps{}, fmt.Errorf("build object-database sink: %w", err)
	}
	return runner.Deps{
		Source:  src,
		Sink:    sk,
		Log:     log,
		Metrics: metrics,
	}, nil
}

func buildLogger(format string) *slog.Logger {
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		h = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	return slog.New(h).With(slog.String("service", serviceName))
}

