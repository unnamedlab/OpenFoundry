// Command media-sets-service hosts the foundation slice of the
// Foundry-style media set service (media_sets CRUD).
//
// Foundation slice scope: media_sets only. media_items, branches,
// transactions, retention, item_markings, outbox, access_patterns,
// dicom_schema (~10k LOC of Rust handlers) all land in follow-up
// slices.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthzlib "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/accesspatterns"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/branches"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/connectorclient"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/connectorresolver"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/grpcserver"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediaitems"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/metrics"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/presignclaim"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/retention"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/server"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/transactions"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/transformclient"
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

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	r := &repo.Repo{Pool: pool}
	h := &handlers.Handlers{Repo: r}
	o := observability.NewMetrics()
	mediaMetrics := metrics.New(o)
	worker := transformclient.New(cfg.MediaTransformRuntimeURL)
	apSvc := accesspatterns.New(r, worker, mediaMetrics)
	ap := &handlers.AccessPatternHandlers{Service: apSvc}

	bundled, err := cedarauthzlocal.BundledPolicyRecords()
	if err != nil {
		log.Error("cedar bundled policies", slog.String("error", err.Error()))
		os.Exit(1)
	}
	store, err := cedarauthzlib.NewWithPolicies(bundled)
	if err != nil {
		log.Error("cedar policy store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	cedarEngine := cedarauthzlocal.NewEngine(cedarauthzlib.NewEngineNoopAudit(store))

	storageBackend, err := storage.NewHMACBackend(cfg.StorageBucket, cfg.StorageEndpoint, []byte(cfg.JWTSecret))
	if err != nil {
		log.Error("storage backend", slog.String("error", err.Error()))
		os.Exit(1)
	}
	itemsSvc := mediaitems.New(r, cedarEngine, storageBackend, time.Duration(cfg.PresignTTLSeconds)*time.Second)

	// Optional virtual-resolver: requires connector-management-service
	// to be reachable. When the URL is unset the download path falls
	// back to the row's storage_uri verbatim (acceptable in dev).
	if cfg.ConnectorManagementServiceURL != "" {
		itemsSvc.VirtualResolver = connectorresolver.New(
			connectorclient.New(cfg.ConnectorManagementServiceURL),
		)
	}
	// Optional presign claim signer: opt-in via JWT_SECRET. The
	// edge-gateway validates the same secret end-to-end.
	if signer, err := presignclaim.NewSigner([]byte(cfg.JWTSecret)); err == nil {
		signer.CapTTL = time.Duration(cfg.PresignTTLSeconds) * time.Second
		itemsSvc.PresignSigner = signer
	} else {
		log.Warn("presign signer disabled", slog.String("error", err.Error()))
	}

	items := &handlers.MediaItemHandlers{Service: itemsSvc}

	branchesSvc := branches.New(r, cedarEngine)
	branchHandlers := &handlers.BranchHandlers{Service: branchesSvc}

	txSvc := transactions.New(r, cedarEngine, mediaMetrics)
	txHandlers := &handlers.TransactionHandlers{Service: txSvc}

	// Periodic retention reaper. Cancels with the main context on
	// SIGTERM. The interval is configurable via env (default 1 min).
	reaper := retention.New(pool, storageBackend, mediaMetrics, log,
		time.Duration(cfg.RetentionReaperIntervalSeconds)*time.Second)
	go reaper.Loop(ctx)

	retHandlers := &handlers.RetentionHandlers{Repo: r, Cedar: cedarEngine, Reaper: reaper}

	// gRPC server runs in parallel with HTTP. The two surfaces share
	// the same service-layer objects so audit + Cedar + metrics
	// behave identically.
	if cfg.GRPCPort > 0 {
		go runGRPC(ctx, cfg.GRPCPort, log, r, itemsSvc, txSvc)
	}

	srv := server.New(cfg, jwt, h, ap, items, branchHandlers, txHandlers, retHandlers, o)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// runGRPC boots the gRPC listener + serves until ctx.Done. Logged
// errors do not exit the process — the HTTP surface remains the
// canonical path even if gRPC fails to bind.
func runGRPC(ctx context.Context, port uint16, log *slog.Logger, r *repo.Repo, items *mediaitems.Service, txs *transactions.Service) {
	addr := fmt.Sprintf(":%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("grpc listen", slog.String("addr", addr), slog.String("error", err.Error()))
		return
	}
	g := grpc.NewServer()
	grpcserver.Register(g, grpcserver.New(r, items, txs))

	done := make(chan struct{})
	go func() {
		log.Info("grpc listening", slog.String("addr", addr))
		if err := g.Serve(lis); err != nil {
			log.Error("grpc serve", slog.String("error", err.Error()))
		}
		close(done)
	}()

	select {
	case <-ctx.Done():
		g.GracefulStop()
		<-done
	case <-done:
	}
}
