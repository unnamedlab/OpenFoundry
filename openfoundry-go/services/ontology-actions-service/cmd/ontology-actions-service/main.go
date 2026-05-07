// Command ontology-actions-service runs the consolidated ontology
// action / funnel / function / rule HTTP surface (per ADR-0030).
//
// Wiring status: when DATABASE_URL is set the binary builds an
// `ontologykernel.AppState` with a real `pgxpool` and an in-memory
// `Stores` bag, and the kernel handlers that have been ported (today
// only `GET /storage/insights`) are mounted against it. Without
// DATABASE_URL the substrate stubs in `internal/handler` keep the
// URL grid alive so smoke tests pass with no infrastructure.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
	pythonsidecar "github.com/openfoundry/openfoundry-go/libs/python-sidecar"
	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/server"
)

// pythonRuntimeAdapter bridges *pythonsidecar.InlineFunctionResult to the
// ontologykernel.PythonInlineRuntime contract (different result type).
type pythonRuntimeAdapter struct{ mgr *pythonsidecar.Manager }

func (a pythonRuntimeAdapter) ExecuteInline(ctx context.Context, source string, inputJSON []byte, timeoutSeconds uint32) (*ontologykernel.InlineRuntimeResult, error) {
	out, err := a.mgr.ExecuteInline(ctx, source, inputJSON, timeoutSeconds)
	if err != nil {
		return nil, err
	}
	return &ontologykernel.InlineRuntimeResult{
		ResultJSON: out.ResultJSON,
		Stdout:     out.Stdout,
		Stderr:     out.Stderr,
	}, nil
}

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
		log.Warn("CASSANDRA_CONTACT_POINTS not set — stores fall back to in-memory (S1.4.a). " +
			"Production deployments MUST set this variable.")
	}

	state, err := buildState(ctx, cfg, log)
	if err != nil {
		log.Error("AppState build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if state != nil && state.DB != nil {
		defer state.DB.Close()
	}

	if state != nil && cfg.PythonSidecarBinary != "" {
		mgr, err := pythonsidecar.New(pythonSidecarConfig(cfg), log)
		if err != nil {
			log.Error("python-sidecar config invalid", slog.String("error", err.Error()))
			os.Exit(1)
		}
		startCtx, cancelStart := context.WithTimeout(ctx, cfg.PythonSidecarTimeout)
		if err := mgr.Start(startCtx); err != nil {
			cancelStart()
			log.Error("python-sidecar start failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		cancelStart()
		defer func() { _ = mgr.Stop(context.Background()) }()
		state.PythonRuntime = pythonRuntimeAdapter{mgr: mgr}
		log.Info("python sidecar wired", slog.String("binary", cfg.PythonSidecarBinary))
	} else if state != nil {
		log.Warn("PYTHON_SIDECAR_BINARY unset — inline Python functions will return ErrPythonRuntimeNotWired")
		log.Warn("PYTHON_SIDECAR_BINARY unset — inline Python functions will explicitly return ErrPythonRuntimeNotWired")
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, state, metrics)
	if err := run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func pythonSidecarConfig(cfg *config.Config) pythonsidecar.Config {
	return pythonsidecar.Config{
		BinaryPath:      cfg.PythonSidecarBinary,
		Args:            append([]string(nil), cfg.PythonSidecarArgs...),
		Env:             append([]string(nil), cfg.PythonSidecarEnv...),
		StartupTimeout:  cfg.PythonSidecarTimeout,
		HardCallTimeout: cfg.PythonSidecarTimeout,
	}
}

func buildState(ctx context.Context, cfg *config.Config, log *slog.Logger) (*ontologykernel.AppState, error) {
	if cfg.DatabaseURL == "" {
		log.Warn("DATABASE_URL unset — kernel handlers (storage insights, …) fall back to substrate stubs")
		return nil, nil
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	state := &ontologykernel.AppState{
		DB:                            pool,
		Stores:                        stores.NewInMemory(),
		HTTPClient:                    &http.Client{Timeout: 30 * time.Second},
		JWTConfig:                     authmw.NewJWTConfig(cfg.JWTSecret),
		AuditServiceURL:               cfg.AuditServiceURL,
		DatasetServiceURL:             cfg.DatasetServiceURL,
		OntologyServiceURL:            cfg.OntologyServiceURL,
		PipelineServiceURL:            cfg.PipelineServiceURL,
		AIServiceURL:                  cfg.AIServiceURL,
		NotificationServiceURL:        cfg.NotificationServiceURL,
		SearchEmbeddingProvider:       cfg.SearchEmbeddingProvider,
		NodeRuntimeCommand:            cfg.NodeRuntimeCommand,
		ConnectorManagementServiceURL: cfg.ConnectorManagementServiceURL,
	}
	return state, nil
}

func run(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		log.Info("shutting down")
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
