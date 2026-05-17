// Command agent-runtime-service hosts the agent runtime + tool
// registry plane (S8.1.b ADR-0030 absorbed tool-registry-service).
//
// Foundation port: full agents CRUD + runs + steps + human-approval,
// plus OpenAI-style chat-completions and copilot ask routes backed by
// the injectable ai-kernel-go LLM runtime. The tool-registry HTTP
// routes (`/api/v1/agent-runtime/tools`) port alongside
// libs/ai-kernel-go/handlers/tools in a follow-up slice.
//
// AI-event Kafka producer wires alongside libs/event-bus-data-go in a
// follow-up slice; the Topic, TxnIDPrefix, AiEventKind enum and
// envelope shape are pinned now in internal/aievents.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm"
	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/domain/llm/anthropic"
	aimodels "github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/capabilities/probes"
	"github.com/openfoundry/openfoundry-go/libs/observability"
	"github.com/openfoundry/openfoundry-go/services/agent-runtime-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/agent-runtime-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/agent-runtime-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/agent-runtime-service/internal/server"
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

	if cfg.KafkaBootstrap == "" {
		log.Warn("KAFKA_BOOTSTRAP_SERVERS unset — ai.events.v1 producer wires in follow-up slice")
	}

	var purposeCheckpoint *authmw.PurposeCheckpointClient
	if cfg.PurposeCheckpointURL != "" {
		purposeCheckpoint = authmw.NewPurposeCheckpointClient(cfg.PurposeCheckpointURL)
	} else {
		log.Warn("AUTHORIZATION_POLICY_SERVICE_URL/PURPOSE_CHECKPOINT_URL unset — sensitive AI chat purpose checks are disabled")
	}

	jwt := authmw.NewJWTConfig(cfg.JWTSecret)
	h := &handlers.Handlers{Repo: &repo.Repo{Pool: pool}, AllowFakeLLMProvider: cfg.AllowFakeLLMProvider, PurposeCheckpoint: purposeCheckpoint}
	wireLLMRuntime(h, log)
	metrics := observability.NewMetrics()

	srv := server.New(cfg, jwt, h, metrics, probes.Postgres("primary", pool))
	if err := server.Run(ctx, srv, log); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// wireLLMRuntime selects the LLM runtime based on env: ANTHROPIC_API_KEY
// engages the real Anthropic provider; otherwise we fall back to the
// in-process FakeRuntime so dev/test setups keep working, and log a
// WARN so operators notice the missing production credential.
func wireLLMRuntime(h *handlers.Handlers, log *slog.Logger) {
	if provider, ok := anthropic.FromEnv(); ok {
		h.Runtime = provider
		h.Provider = anthropicCatalogEntry(provider.Model)
		log.Info("anthropic llm runtime engaged",
			slog.String("model", provider.Model),
			slog.String("base_url", provider.BaseURL),
		)
		return
	}
	log.Warn("ANTHROPIC_API_KEY unset — falling back to in-process FakeRuntime (non-production)")
	h.Runtime = &llm.FakeRuntime{}
	h.AllowFakeLLMProvider = true
}

// anthropicCatalogEntry mints a synthetic LlmProvider so the chat
// handler's provider-required gate passes without a DB-provisioned row.
// The api_mode/model fields drive estimated-cost calculations and the
// provider name surfaced in copilot responses.
func anthropicCatalogEntry(model string) *aimodels.LlmProvider {
	rules := aimodels.DefaultProviderRoutingRules()
	return &aimodels.LlmProvider{
		ID:              uuid.MustParse("00000000-0000-0000-0000-0000000a17c1"),
		Name:            "anthropic-env",
		ProviderType:    "anthropic",
		ModelName:       model,
		EndpointURL:     anthropic.DefaultBaseURL,
		APIMode:         "messages",
		Enabled:         true,
		MaxOutputTokens: 1024,
		RouteRules:      rules,
	}
}
