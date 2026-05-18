package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	// Side-effect import registers the S3 / GCS / Azure / in-memory
	// scheme handlers with apache/iceberg-go's io subsystem. Without
	// it any Scan whose table metadata lives on s3:// fails with
	// `ErrIOSchemeNotFound: scheme s3`. Phase A discovered this gap.
	_ "github.com/apache/iceberg-go/io/gocloud"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
	pipelineruntime "github.com/openfoundry/openfoundry-go/libs/pipeline-runtime"
	"github.com/openfoundry/openfoundry-go/services/pipeline-runner/internal/providers"
	"github.com/openfoundry/openfoundry-go/services/pipeline-runner/internal/server"
)

// EnvPipelinePlanB64 is the env var the dispatcher (ADR-0045 Phase
// C.4.a) populates with the base64-encoded pipelineplan.Plan JSON.
// The runner decodes it on boot and executes via libs/pipeline-runtime.
const EnvPipelinePlanB64 = "PIPELINE_PLAN_B64"

// EnvPipelinePlanFile is the env var that points at a JSON file
// containing the pipelineplan.Plan. Dev YAMLs (infra/dev/*.yaml,
// Phase C.6) prefer this shape because a ConfigMap-mounted file
// stays human-readable in `kubectl describe configmap` — the
// base64 env var path is intended for the dispatcher's generated
// Jobs.
const EnvPipelinePlanFile = "PIPELINE_PLAN_FILE"

// Run drives the entire orchestration: argument logging, plan
// decoding, provider wiring, and pipelineruntime.Executor invocation.
// `--smoke` short-circuits the providers and only validates the plan;
// integration CI runs with that mode so it does not need a live
// Iceberg catalog.
func Run(args Args) error {
	log := buildLogger(args)
	log.Info("pipeline-runner starting",
		slog.Bool("smoke", args.Smoke),
		slog.String("version", args.Version),
		slog.String("pipeline_id", args.PipelineID),
		slog.String("run_id", args.RunID),
		slog.String("input_dataset", args.InputDataset),
		slog.String("output_dataset", args.OutputDataset),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	httpSrv := server.New(args.HealthAddr, "pipeline-runner", args.Version)
	log.Info("health/metrics listening", slog.String("addr", args.HealthAddr))

	var wg sync.WaitGroup
	httpErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpSrv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			httpErr <- err
		}
		close(httpErr)
	}()

	runErr := runWork(ctx, args, log)
	cancel()
	wg.Wait()

	if runErr != nil {
		return runErr
	}
	if err, ok := <-httpErr; ok && err != nil {
		return fmt.Errorf("health server: %w", err)
	}
	return nil
}

func runWork(ctx context.Context, args Args, log *slog.Logger) error {
	plan, err := loadPlan(args)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if errs := plan.Validate(); errs != nil {
		return fmt.Errorf("plan invalid: %w", errs)
	}
	log.Info("plan decoded",
		slog.Int("ops", len(plan.Ops)),
		slog.String("plan_pipeline_id", plan.PipelineID),
		slog.String("plan_run_id", plan.RunID),
	)
	if args.Smoke {
		log.Info("smoke mode: skipping execution after plan validation")
		return nil
	}

	reader, err := providers.OpenIcebergReader(ctx, providers.IcebergReaderConfig{
		CatalogName:   firstCatalogNameInPlan(plan),
		CatalogURI:    args.CatalogURI,
		Warehouse:     args.CatalogWarehouse,
		Credential:    args.CatalogCredential,
		OAuthTokenURI: args.OAuthTokenURI,
		OAuthScope:    args.OAuthScope,
	})
	if err != nil {
		return fmt.Errorf("open iceberg reader: %w", err)
	}
	defer func() { _ = reader.Close() }()

	writer, err := providers.NewHTTPWriter(providers.HTTPWriterConfig{
		TableWriterURL: args.TableWriterURL,
		CatalogURL:     args.CatalogURI,
		Warehouse:      args.CatalogWarehouse,
		InternalToken:  args.InternalToken,
	})
	if err != nil {
		return fmt.Errorf("open http writer: %w", err)
	}

	exec := &pipelineruntime.Executor{Reader: reader, Writer: writer}
	start := time.Now()
	if err := exec.Run(ctx, plan); err != nil {
		return fmt.Errorf("execute plan after %s: %w", time.Since(start).Round(time.Millisecond), err)
	}
	log.Info("plan executed", slog.Duration("duration", time.Since(start).Round(time.Millisecond)))
	return nil
}

// loadPlan resolves the Plan from the three accepted sources in
// priority order: --plan-file flag, PIPELINE_PLAN_FILE env var, then
// PIPELINE_PLAN_B64 env var. The dispatcher uses the env-var
// base64 path; dev YAMLs prefer --plan-file (ConfigMap mount) so
// the Plan JSON stays readable in kubectl describe.
func loadPlan(args Args) (pipelineplan.Plan, error) {
	if path := firstNonEmpty(args.PlanFile, os.Getenv(EnvPipelinePlanFile)); path != "" {
		return loadPlanFromFile(path)
	}
	return loadPlanFromEnv()
}

// loadPlanFromFile reads the JSON plan from `path`.
func loadPlanFromFile(path string) (pipelineplan.Plan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pipelineplan.Plan{}, fmt.Errorf("read plan file %s: %w", path, err)
	}
	var plan pipelineplan.Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return pipelineplan.Plan{}, fmt.Errorf("unmarshal plan from %s: %w", path, err)
	}
	return plan, nil
}

// loadPlanFromEnv decodes the base64-JSON env var the dispatcher
// populates. Empty or malformed values surface as a typed error so
// the operator runbook can pinpoint the bad Job.
func loadPlanFromEnv() (pipelineplan.Plan, error) {
	raw := os.Getenv(EnvPipelinePlanB64)
	if raw == "" {
		return pipelineplan.Plan{}, fmt.Errorf("no Plan source: set --plan-file, %s, or %s (dispatcher populates the base64 env var)", EnvPipelinePlanFile, EnvPipelinePlanB64)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return pipelineplan.Plan{}, fmt.Errorf("decode %s base64: %w", EnvPipelinePlanB64, err)
	}
	var plan pipelineplan.Plan
	if err := json.Unmarshal(decoded, &plan); err != nil {
		return pipelineplan.Plan{}, fmt.Errorf("unmarshal plan JSON: %w", err)
	}
	return plan, nil
}

// firstCatalogNameInPlan returns the catalog the Plan's first source
// op references. The IcebergReader is constructed with that catalog
// name; mismatched ops surface as providers.ErrUnknownCatalog at
// Scan time. v2 may support multiple catalogs per Plan.
func firstCatalogNameInPlan(plan pipelineplan.Plan) string {
	for _, op := range plan.Ops {
		if op.Kind == pipelineplan.KindReadTable && op.ReadTable != nil {
			return op.ReadTable.Catalog
		}
	}
	return ""
}

func buildLogger(args Args) *slog.Logger {
	var h slog.Handler
	if args.LogFormat == "json" {
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		h = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	return slog.New(h).With(
		slog.String("service", "pipeline-runner"),
		slog.String("pipeline_id", args.PipelineID),
		slog.String("run_id", args.RunID),
	)
}
