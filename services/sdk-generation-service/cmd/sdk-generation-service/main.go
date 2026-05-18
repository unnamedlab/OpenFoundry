// Command sdk-generation-service is the SDK + OpenAPI contract
// publisher. Wires the server + auth + metrics so the documented
// handler surface can be exercised end-to-end.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator/ts"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/ontologyclient"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/server"
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
	store := &repo.Repo{Pool: pool}
	h := &handlers.Handlers{Repo: store}
	gen := &handlers.GenerateHandler{Driver: &generator.Driver{
		RepoRoot: os.Getenv("OF_REPO_ROOT"),
	}}

	var ontology handlers.OntologyFetcher
	if cfg.OntologyServiceURL != "" {
		ontology = &ontologyclient.HTTPClient{
			BaseURL: cfg.OntologyServiceURL,
			Token:   cfg.OntologyServiceToken,
		}
	} else {
		log.Warn("ONTOLOGY_SERVICE_URL unset — using StubClient (dev only)")
		ontology = &ontologyclient.StubClient{}
	}
	artifactStore := &handlers.LocalArtifactStore{Dir: cfg.ArtifactDir}
	worker := &handlers.BuildWorker{
		Repo:        store,
		Ontology:    ontology,
		TSGenerator: &ts.Generator{},
		Artifacts:   artifactStore,
	}
	buildsAPI := &handlers.BuildHandlers{
		Repo:      store,
		Worker:    worker,
		Artifacts: artifactStore,
	}
	metrics := observability.NewMetrics()

	srv := server.New(cfg, jwt, h, gen, buildsAPI, metrics, probes.Postgres("primary", pool))
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
