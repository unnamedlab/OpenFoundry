// Command function-runtime-service runs the v0 user-function runtime
// (TypeScript + Python stubs) described in
// docs/migration/foundry-functions-runtime-1to1-checklist.md.
//
// When DATABASE_URL is empty the service may fall back to an in-memory
// store only when database.allow_memory_store permits it (dev/test).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/capabilities"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/executor"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/server"
)

var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/function-runtime-service/config.yaml", "path to config file")
	flag.Parse()

	envOverride := os.Getenv("CONFIG_FILE")
	cfg, err := config.Load(*cfgPath, envOverride)
	if err != nil {
		slog.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if cfg.Service.Version == "" {
		cfg.Service.Version = version
	}

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	store, dependencyProbes, closeStore, err := buildStore(ctx, cfg, log)
	if err != nil {
		log.Error("store build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if closeStore != nil {
		defer closeStore()
	}

	registry, runtimeProbes, err := buildRuntimeRegistry(cfg)
	if err != nil {
		log.Error("runtime registry build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	dependencyProbes = append(dependencyProbes, runtimeProbes...)

	h := &handlers.Handlers{
		Store:          store,
		Exec:           registry,
		EnabledRuntime: registry.Enabled,
		DefaultTimeout: cfg.DefaultExecutorTimeout(),
		MaxTimeout:     cfg.MaxExecutorTimeout(),
		Now:            func() time.Time { return time.Now().UTC() },
	}

	metrics := observability.NewMetrics()
	srv := server.New(cfg, h, metrics, dependencyProbes...)
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func buildStore(ctx context.Context, cfg *config.Config, log *slog.Logger) (repo.Store, []capabilities.DependencyProbe, func(), error) {
	if cfg.Database.URL == "" {
		if cfg.IsProduction() && !cfg.Database.AllowMemoryStore {
			return nil, nil, nil, fmt.Errorf("database.url is required in production unless database.allow_memory_store is explicitly enabled")
		}
		log.Warn("DATABASE_URL unset — using in-memory store (dev/test only)")
		return repo.NewMemoryStore(), nil, nil, nil
	}
	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := repo.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, nil, nil, err
	}
	return repo.NewPostgresStore(pool), []capabilities.DependencyProbe{probes.Postgres("primary", pool)}, pool.Close, nil
}

func buildRuntimeRegistry(cfg *config.Config) (*executor.Registry, []capabilities.DependencyProbe, error) {
	cfg.Normalize()
	limits := executor.Limits{
		Timeout:              cfg.DefaultExecutorTimeout(),
		MemoryLimitMiB:       cfg.Executor.MemoryLimitMiB,
		MaxStdoutBytes:       cfg.Executor.MaxStdoutBytes,
		MaxStderrBytes:       cfg.Executor.MaxStderrBytes,
		AllowRemoteSourceURI: cfg.Executor.AllowRemoteSourceURI,
	}
	registry := executor.NewRegistry()
	var probes []capabilities.DependencyProbe
	for _, raw := range cfg.Executor.EnabledRuntimes {
		rt := models.Runtime(strings.TrimSpace(raw))
		if rt == "" {
			continue
		}
		if !rt.Valid() {
			return nil, nil, fmt.Errorf("executor.enabled_runtimes contains unsupported runtime %q", raw)
		}
		bin := runtimeBinary(cfg, rt)
		lookPathErr := runtimeBinaryAvailable(bin)
		if lookPathErr != nil && (cfg.IsProduction() || cfg.Executor.RequireBinariesOnStart) {
			return nil, nil, fmt.Errorf("runtime %q enabled but binary %q is unavailable: %w", rt, bin, lookPathErr)
		}
		switch rt {
		case models.RuntimeTypeScript:
			if lookPathErr != nil {
				registry.RegisterUnavailable(rt, lookPathErr)
			} else {
				registry.Register(rt, executor.NewTSProcessExecutor(cfg.Executor.NodeBinary, limits))
			}
		case models.RuntimePython:
			if lookPathErr != nil {
				registry.RegisterUnavailable(rt, lookPathErr)
			} else {
				registry.Register(rt, executor.NewPythonProcessExecutor(cfg.Executor.PythonBinary, limits))
			}
		}
		name := "runtime." + string(rt)
		probeBin := bin
		probes = append(probes, capabilities.DependencyProbe{
			Name: name,
			Kind: capabilities.DependencyKind("runtime"),
			Probe: func(context.Context) error {
				return runtimeBinaryAvailable(probeBin)
			},
			Timeout: time.Second,
		})
	}
	return registry, probes, nil
}

func runtimeBinary(cfg *config.Config, rt models.Runtime) string {
	switch rt {
	case models.RuntimeTypeScript:
		if cfg.Executor.NodeBinary != "" {
			return cfg.Executor.NodeBinary
		}
		return "node"
	case models.RuntimePython:
		if cfg.Executor.PythonBinary != "" {
			return cfg.Executor.PythonBinary
		}
		return "python3"
	default:
		return ""
	}
}

func runtimeBinaryAvailable(bin string) error {
	if bin == "" {
		return fmt.Errorf("empty binary path")
	}
	_, err := exec.LookPath(bin)
	return err
}
