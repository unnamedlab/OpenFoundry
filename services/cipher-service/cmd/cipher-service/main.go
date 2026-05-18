// Command cipher-service is the entrypoint for the OpenFoundry
// cipher microservice. It owns the cipher key registry and the
// encrypt/decrypt path described in
// docs/migration/foundry-cipher-1to1-checklist.md (Milestone A).
//
// Boot sequence:
//   - load config (yaml + env)
//   - initialise observability (slog + OTel)
//   - open the Postgres pool and apply migrations
//   - choose a KMS backend (local env-var KEK in dev, AWS stub
//     otherwise)
//   - assemble the handler.State and start the HTTP server
//
// Mandatory env at start-up:
//   - DATABASE_URL or OF_DATABASE__URL — Postgres DSN.
//   - OF_CIPHER_LOCAL_KEK (32 hex bytes) when KMS backend is "local".
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	"github.com/openfoundry/openfoundry-go/libs/observability"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/anomaly"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/audit"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/config"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/kms"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/server"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfgPath := flag.String("config", "services/cipher-service/config.yaml", "path to config file")
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
	if cfg.Database.URL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	log := observability.InitLogging(cfg.Service.Name, cfg.Service.Version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	shutdownTracing, err := observability.InitTracing(ctx, cfg.Service.Name, cfg.Service.Version)
	if err != nil {
		log.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		_ = shutdownTracing(context.Background())
	}()

	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		log.Error("pgx pool failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()
	if err := repo.Migrate(ctx, pool); err != nil {
		log.Error("migrations failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	kmsImpl, err := buildKMS(cfg, log)
	if err != nil {
		log.Error("kms init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Milestone A wires a no-op audit emitter so the recorder is
	// safe to call from every handler. A real audittrail.KafkaEmitter
	// drops in once the cipher service joins the audit bus (cf.
	// audit-compliance-service); the Recorder interface is stable
	// so handlers stay unchanged.
	recorder := audit.NewRecorder(audittrail.NopEmitter{}, log)

	budgetWindow, _ := time.ParseDuration(cfg.Governance.BudgetWindow)
	anomalyWindow, _ := time.ParseDuration(cfg.Governance.AnomalyWindow)

	state := &handler.State{
		Repo:    repo.New(pool),
		KMS:     kmsImpl,
		Audit:   recorder,
		Budgets: handler.NewDecryptBudgetManager(cfg.Governance.DefaultDecryptBudget, budgetWindow),
		Anomaly: anomaly.NewDetector(cfg.Governance.AnomalyBurstLimit, anomalyWindow, nil),
	}

	metrics := observability.NewMetrics()

	srv, err := server.New(cfg, state, metrics, log)
	if err != nil {
		log.Error("server build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// buildKMS picks the wrapping backend declared in config.
//
// "local" is the only fully-functional backend in Milestone A; "aws"
// returns a stub honouring the interface so the deployment can
// declare intent today and swap to the real client in Milestone C.
func buildKMS(cfg *config.Config, log *slog.Logger) (kms.KMS, error) {
	switch cfg.KMS.Backend {
	case "local", "":
		k, err := kms.NewLocalKMSFromEnv()
		if err != nil {
			return nil, err
		}
		log.Info("kms backend ready", slog.String("backend", "local"), slog.String("ref", k.Ref()))
		return k, nil
	case "aws", "aws_kms":
		k := kms.NewAWSKMSStub(cfg.KMS.AWSKeyARN)
		log.Warn("kms backend is an AWS KMS stub", slog.String("ref", k.Ref()))
		return k, nil
	case "vault_transit":
		k := kms.NewExternalStub(kms.BackendVaultTransit, cfg.KMS.VaultKey)
		log.Warn("kms backend is a Vault Transit stub", slog.String("ref", k.Ref()))
		return k, nil
	case "gcp_kms":
		k := kms.NewExternalStub(kms.BackendGCPKMS, cfg.KMS.GCPKey)
		log.Warn("kms backend is a GCP KMS stub", slog.String("ref", k.Ref()))
		return k, nil
	case "azure_key_vault":
		k := kms.NewExternalStub(kms.BackendAzureKeyVault, cfg.KMS.AzureKey)
		log.Warn("kms backend is an Azure Key Vault stub", slog.String("ref", k.Ref()))
		return k, nil
	case "pkcs11":
		k := kms.NewExternalStub(kms.BackendPKCS11, cfg.KMS.PKCS11Key)
		log.Warn("kms backend is a PKCS#11 HSM stub", slog.String("ref", k.Ref()))
		return k, nil
	default:
		return nil, errUnknownBackend(cfg.KMS.Backend)
	}
}

type errUnknownBackendT string

func (e errUnknownBackendT) Error() string { return "unknown KMS backend: " + string(e) }

func errUnknownBackend(b string) error { return errUnknownBackendT(b) }
