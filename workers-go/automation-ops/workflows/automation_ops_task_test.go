package workflows

import (
	"context"
	"testing"

	"go.temporal.io/sdk/testsuite"

	"github.com/open-foundry/open-foundry/workers-go/automation-ops/activities"
	"github.com/open-foundry/open-foundry/workers-go/automation-ops/internal/contract"
)

type workflowOpsClient struct {
	inputs []activities.RunInput
	err    error
}

func (c *workflowOpsClient) RecordRun(_ context.Context, in activities.RunInput) (activities.RunResult, error) {
	c.inputs = append(c.inputs, in)
	if c.err != nil {
		return activities.RunResult{}, c.err
	}
	return activities.RunResult{
		TaskID: in.TaskID,
		RunID:  "run-1",
		Status: "completed",
		Run:    map[string]any{"id": "run-1"},
	}, nil
}

func TestAutomationOpsTaskRecordsRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	client := &workflowOpsClient{}
	env.RegisterWorkflow(AutomationOpsTask)
	env.RegisterActivity(activities.NewWithClient(client))

	env.ExecuteWorkflow(AutomationOpsTask, contract.AutomationOpsInput{
		TaskID:   "task-1",
		TenantID: "tenant-a",
		TaskType: "retention_sweep",
		Payload:  map[string]any{"scope": "datasets"},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result contract.AutomationOpsResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != "completed" || result.RunID != "run-1" {
		t.Fatalf("result = %#v", result)
	}
	if len(client.inputs) != 1 {
		t.Fatalf("inputs len = %d", len(client.inputs))
	}
	if client.inputs[0].TaskType != "retention_sweep" {
		t.Fatalf("task type = %q", client.inputs[0].TaskType)
	}
}
