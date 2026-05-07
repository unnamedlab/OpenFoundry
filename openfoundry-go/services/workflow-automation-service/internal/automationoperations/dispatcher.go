package automationoperations

import (
	"context"
	"encoding/json"
	"fmt"

	saga "github.com/openfoundry/openfoundry-go/libs/saga"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/automationoperations/steps"
)

// KnownSagaTypes is the pinned list of `task_type`s this service
// knows how to dispatch. Used by the HTTP handler to reject unknown
// saga types up-front.
var KnownSagaTypes = []string{"retention.sweep", "cleanup.workspace"}

// IsKnown returns true iff `taskType` has a registered step graph.
func IsKnown(taskType string) bool {
	for _, t := range KnownSagaTypes {
		if t == taskType {
			return true
		}
	}
	return false
}

// DispatchSaga drives `taskType`'s step graph to completion. Returns
// nil on the happy path; returns an error if any step (or the input
// parsing) failed — by then the runner has already run LIFO
// compensations and updated saga.state to its terminal value.
func DispatchSaga(ctx context.Context, runner *saga.SagaRunner, taskType string, input json.RawMessage) error {
	switch taskType {
	case "retention.sweep":
		return dispatchRetentionSweep(ctx, runner, input)
	case "cleanup.workspace":
		return dispatchCleanupWorkspace(ctx, runner, input)
	default:
		return saga.StepFailure("dispatch", fmt.Sprintf("unknown saga type %q; known: %v", taskType, KnownSagaTypes))
	}
}

func dispatchRetentionSweep(ctx context.Context, runner *saga.SagaRunner, raw json.RawMessage) error {
	var in steps.RetentionSweepInput
	if err := json.Unmarshal(rawOrNullObject(raw), &in); err != nil {
		return saga.StepFailure("retention.sweep", fmt.Sprintf("invalid input: %s", err))
	}
	if _, err := saga.ExecuteStep[steps.RetentionSweepInput, steps.RetentionSweepOutput](
		ctx, runner, steps.EvictRetentionEligible{}, in,
	); err != nil {
		return err
	}
	return runner.Finish(ctx)
}

func dispatchCleanupWorkspace(ctx context.Context, runner *saga.SagaRunner, raw json.RawMessage) error {
	var in steps.CleanupWorkspaceInput
	if err := json.Unmarshal(rawOrNullObject(raw), &in); err != nil {
		return saga.StepFailure("cleanup.workspace", fmt.Sprintf("invalid input: %s", err))
	}
	if _, err := saga.ExecuteStep[steps.CleanupWorkspaceInput, steps.MarkForDeletionOutput](
		ctx, runner, steps.MarkForDeletion{}, in,
	); err != nil {
		return err
	}
	if _, err := saga.ExecuteStep[steps.CleanupWorkspaceInput, steps.DropBlobsOutput](
		ctx, runner, steps.DropWorkspaceBlobs{}, in,
	); err != nil {
		return err
	}
	if _, err := saga.ExecuteStep[steps.CleanupWorkspaceInput, steps.FinalizeOutput](
		ctx, runner, steps.FinalizeWorkspaceDeletion{}, in,
	); err != nil {
		return err
	}
	return runner.Finish(ctx)
}

func rawOrNullObject(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return []byte(raw)
}
