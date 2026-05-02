package workflows

import (
	"go.temporal.io/sdk/workflow"

	"github.com/open-foundry/open-foundry/workers-go/automation-ops/internal/contract"
)

// AutomationOpsTask is the substrate workflow for S2.7. Concrete
// task types (cleanup, retention sweep, schema rebuild, …) materialise
// per-PR as the legacy `automation-operations-service` Postgres rows
// migrate to durable workflow state.
func AutomationOpsTask(ctx workflow.Context, input contract.AutomationOpsInput) (contract.AutomationOpsResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("AutomationOpsTask started",
		"task_id", input.TaskID,
		"task_type", input.TaskType,
		"tenant_id", input.TenantID,
	)
	return contract.AutomationOpsResult{
		TaskID: input.TaskID,
		Status: "completed",
	}, nil
}
