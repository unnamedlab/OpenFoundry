package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/open-foundry/open-foundry/workers-go/pipeline/activities"
	"github.com/open-foundry/open-foundry/workers-go/pipeline/internal/contract"
)

// PipelineRun is the workflow type registered for both ad-hoc and
// scheduled pipeline runs. Temporal Schedules (see S2.4.a) call this
// same workflow type — exactly-once dispatch is the scheduler's job,
// not ours.
//
// S2.6.a — the workflow is now a 2-step DAG: `BuildPipeline` (compile
// + plan) followed by `ExecutePipeline` (run the compiled plan).
// Both steps are activities so Temporal owns retries, heartbeats and
// task-queue routing. The legacy in-process executor in
// `services/pipeline-build-service/src/domain/executor.rs` is kept as
// the activity implementation surface — the gRPC entry point lands
// PR-by-PR.
func PipelineRun(ctx workflow.Context, input contract.PipelineRunInput) (contract.PipelineRunResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("PipelineRun started",
		"pipeline_id", input.PipelineID,
		"tenant_id", input.TenantID,
		"revision", input.Revision,
	)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    1 * time.Minute,
			BackoffCoefficient: 2.0,
			MaximumInterval:    15 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	actx := workflow.WithActivityOptions(ctx, ao)
	auditID := workflow.GetInfo(ctx).WorkflowExecution.ID

	// Step 1 — build (compile + plan).
	var build activities.BuildResult
	err := workflow.ExecuteActivity(actx, (*activities.Activities).BuildPipeline, activities.BuildInput{
		PipelineID:         input.PipelineID,
		TenantID:           input.TenantID,
		Revision:           input.Revision,
		Parameters:         input.Parameters,
		AuditCorrelationID: auditID,
	}).Get(ctx, &build)
	if err != nil {
		logger.Error("build activity failed", "err", err, "pipeline_id", input.PipelineID)
		return contract.PipelineRunResult{
			PipelineID: input.PipelineID,
			Status:     "failed",
			Error:      err.Error(),
		}, nil
	}

	// Step 2 — execute the compiled plan.
	var exec activities.ExecuteResult
	err = workflow.ExecuteActivity(actx, (*activities.Activities).ExecutePipeline, activities.ExecuteInput{
		PipelineID:         input.PipelineID,
		TenantID:           input.TenantID,
		Plan:               build.Plan,
		AuditCorrelationID: auditID,
	}).Get(ctx, &exec)
	if err != nil {
		logger.Error("execute activity failed", "err", err, "pipeline_id", input.PipelineID)
		return contract.PipelineRunResult{
			PipelineID: input.PipelineID,
			Status:     "failed",
			Error:      err.Error(),
		}, nil
	}

	return contract.PipelineRunResult{
		PipelineID: input.PipelineID,
		Status:     exec.Status,
	}, nil
}
