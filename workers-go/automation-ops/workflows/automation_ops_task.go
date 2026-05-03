package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/open-foundry/open-foundry/workers-go/automation-ops/activities"
	"github.com/open-foundry/open-foundry/workers-go/automation-ops/internal/contract"
)

// AutomationOpsTask executes an operational task through the owning Rust
// service. Temporal owns retries and durability; automation-operations-service
// owns the operator-visible run projection during the cutover.
func AutomationOpsTask(ctx workflow.Context, input contract.AutomationOpsInput) (contract.AutomationOpsResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("AutomationOpsTask started",
		"task_id", input.TaskID,
		"task_type", input.TaskType,
		"tenant_id", input.TenantID,
	)
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    5,
		},
	}
	actx := workflow.WithActivityOptions(ctx, activityOptions)
	var run activities.RunResult
	if err := workflow.ExecuteActivity(actx, (*activities.Activities).ExecuteTask, input).Get(ctx, &run); err != nil {
		logger.Error("AutomationOpsTask activity failed", "err", err, "task_id", input.TaskID)
		return contract.AutomationOpsResult{
			TaskID: input.TaskID,
			Status: "failed",
			Error:  err.Error(),
		}, nil
	}
	return contract.AutomationOpsResult{
		TaskID: input.TaskID,
		Status: run.Status,
		RunID:  run.RunID,
	}, nil
}
