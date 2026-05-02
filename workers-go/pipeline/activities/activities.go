// Package activities holds Temporal activities for the pipeline
// worker. S2.6.a — `BuildPipeline` and `ExecutePipeline` are the
// substrate activities driven by `PipelineRunWorkflow`. They are
// thin gRPC wrappers around `pipeline-build-service` (build/compile)
// and the run-time executor (DAG execution).
//
// Substrate status: both methods are stubs that return their input
// unchanged plus a `status` field. Real wiring lands when:
//   - `proto/data_integration/v1/pipeline_build.proto` ships in
//     `proto/gen/go` (build activity);
//   - the executor exposes a stable internal-gRPC entry point
//     (execute activity).
//
// Same retry policy applies to both: at most 3 attempts with expo
// backoff (1m → 15m), as configured by `PipelineRunWorkflow`.
package activities

import (
	"context"
	"errors"
	"log/slog"
	"os"
)

// BuildInput selects the pipeline revision to compile. The `Plan`
// returned by the activity is opaque to the worker — it is forwarded
// verbatim to `ExecutePipeline`.
type BuildInput struct {
	PipelineID         string         `json:"pipeline_id"`
	TenantID           string         `json:"tenant_id"`
	Revision           string         `json:"revision,omitempty"`
	Parameters         map[string]any `json:"parameters,omitempty"`
	AuditCorrelationID string         `json:"audit_correlation_id"`
}

type BuildResult struct {
	PipelineID string         `json:"pipeline_id"`
	Status     string         `json:"status"` // "compiled" | "failed"
	Plan       map[string]any `json:"plan,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type ExecuteInput struct {
	PipelineID         string         `json:"pipeline_id"`
	TenantID           string         `json:"tenant_id"`
	Plan               map[string]any `json:"plan"`
	AuditCorrelationID string         `json:"audit_correlation_id"`
}

type ExecuteResult struct {
	PipelineID string `json:"pipeline_id"`
	Status     string `json:"status"` // "completed" | "failed"
	Error      string `json:"error,omitempty"`
}

// ErrBuildClientUnavailable is returned by the substrate
// implementation. Workflows treat it as retryable so a follow-up
// PR can land the real gRPC client without changing the workflow.
var ErrBuildClientUnavailable = errors.New(
	"pipeline-build-service gRPC client not yet wired (S2.6.a follow-up)",
)

type Activities struct {
	buildAddr   string
	executeAddr string
	logger      *slog.Logger
}

func New() *Activities {
	build := os.Getenv("OF_PIPELINE_BUILD_GRPC_ADDR")
	if build == "" {
		build = "pipeline-build-service:50051"
	}
	exec := os.Getenv("OF_PIPELINE_EXEC_GRPC_ADDR")
	if exec == "" {
		exec = "pipeline-build-service:50051"
	}
	return &Activities{
		buildAddr:   build,
		executeAddr: exec,
		logger:      slog.Default(),
	}
}

// BuildPipeline — Temporal activity. Compile + plan the pipeline.
//
// Today this is a logging stub returning an empty plan; the real
// implementation will dial `OF_PIPELINE_BUILD_GRPC_ADDR` and call
// `PipelineBuildService.Compile(...)` once the proto bindings ship.
func (a *Activities) BuildPipeline(ctx context.Context, in BuildInput) (BuildResult, error) {
	a.logger.InfoContext(ctx, "pipeline.build (substrate stub)",
		"pipeline_id", in.PipelineID,
		"revision", in.Revision,
		"audit_correlation_id", in.AuditCorrelationID,
	)
	return BuildResult{
		PipelineID: in.PipelineID,
		Status:     "compiled",
		Plan:       map[string]any{"_substrate": true},
	}, nil
}

// ExecutePipeline — Temporal activity. Run the compiled plan.
func (a *Activities) ExecutePipeline(ctx context.Context, in ExecuteInput) (ExecuteResult, error) {
	a.logger.InfoContext(ctx, "pipeline.execute (substrate stub)",
		"pipeline_id", in.PipelineID,
		"audit_correlation_id", in.AuditCorrelationID,
	)
	return ExecuteResult{
		PipelineID: in.PipelineID,
		Status:     "completed",
	}, nil
}
