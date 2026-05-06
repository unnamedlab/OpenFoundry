// Command media-transform-runtime-service hosts the worker that runs
// Foundry-style media access patterns.
//
// Foundation slice scope:
//   - Catalog endpoint (`GET /catalog`, `GET /catalog/{kind}`) ports
//     the full Rust catalog verbatim. Image entries marked Native on
//     the Rust side are NotImplemented on the Go side until the
//     image-handler slice (golang.org/x/image port) lands.
//   - REST runtime (`POST /transform`) routes by catalog status with
//     the Rust 501 envelope for NotImplemented + External entries.
//   - `/healthz` returns plain text "ok" matching Rust.
//
// Follow-up slice: Go-native image handlers (thumbnail / resize /
// resize_within_bounding_box / rotate / crop / grayscale) using
// stdlib + golang.org/x/image for round-trip parity with the Rust
// `image` crate. Cost-meter integration (compute_seconds) lands
// alongside that slice.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/media-transform-runtime-service/internal/server"
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
	srv := server.New(cfg, metrics)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
