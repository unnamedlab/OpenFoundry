// Package steps holds the saga step graphs registered in this
// service. Each module defines one saga's step types
// (`saga.SagaStep[Input, Output]` impls) and their inputs / outputs.
//
// Dispatch from a `task_type` string (the saga type carried on
// `saga.step.requested.v1` events) to a step graph lives in
// `automationoperations/dispatcher.go`.
package steps

import (
	"context"
	"time"

	"github.com/google/uuid"

	saga "github.com/openfoundry/openfoundry-go/libs/saga"
)

// CleanupWorkspaceInput mirrors the Rust struct of the same name.
//
// Three-step example with compensations, used by the chaos test
// (LIFO compensation verification).
type CleanupWorkspaceInput struct {
	TenantID        string    `json:"tenant_id"`
	WorkspaceID     uuid.UUID `json:"workspace_id"`
	ForceFailureAt  *string   `json:"force_failure_at,omitempty"`
}

func (in CleanupWorkspaceInput) mustFailHere(step string) bool {
	return in.ForceFailureAt != nil && *in.ForceFailureAt == step
}

// MarkForDeletion is step 1: tombstone the workspace row.
type MarkForDeletion struct{}

// MarkForDeletionOutput mirrors the Rust struct.
type MarkForDeletionOutput struct {
	WorkspaceID    uuid.UUID `json:"workspace_id"`
	TombstonedAt   time.Time `json:"tombstoned_at"`
}

// StepName satisfies SagaStep.
func (MarkForDeletion) StepName() string { return "mark_for_deletion" }

// Execute satisfies SagaStep.
func (s MarkForDeletion) Execute(_ context.Context, in CleanupWorkspaceInput) (MarkForDeletionOutput, error) {
	if in.mustFailHere(s.StepName()) {
		return MarkForDeletionOutput{}, saga.StepFailure(s.StepName(), "forced failure (chaos test)")
	}
	return MarkForDeletionOutput{
		WorkspaceID:  in.WorkspaceID,
		TombstonedAt: time.Now().UTC(),
	}, nil
}

// Compensate satisfies SagaStep — real impl would clear the
// tombstone flag on the workspace row.
func (MarkForDeletion) Compensate(_ context.Context, _ CleanupWorkspaceInput) error { return nil }

// DropWorkspaceBlobs is step 2: delete object-store payload.
type DropWorkspaceBlobs struct{}

// DropBlobsOutput mirrors the Rust struct.
type DropBlobsOutput struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	BlobCount   uint64    `json:"blob_count"`
}

// StepName satisfies SagaStep.
func (DropWorkspaceBlobs) StepName() string { return "drop_workspace_blobs" }

// Execute satisfies SagaStep.
func (s DropWorkspaceBlobs) Execute(_ context.Context, in CleanupWorkspaceInput) (DropBlobsOutput, error) {
	if in.mustFailHere(s.StepName()) {
		return DropBlobsOutput{}, saga.StepFailure(s.StepName(), "forced failure (chaos test)")
	}
	return DropBlobsOutput{WorkspaceID: in.WorkspaceID, BlobCount: 0}, nil
}

// Compensate satisfies SagaStep — real impl would HTTP-call the
// storage service to restore soft-deleted keys.
func (DropWorkspaceBlobs) Compensate(_ context.Context, _ CleanupWorkspaceInput) error { return nil }

// FinalizeWorkspaceDeletion is step 3: emit lineage + audit events.
type FinalizeWorkspaceDeletion struct{}

// FinalizeOutput mirrors the Rust struct.
type FinalizeOutput struct {
	WorkspaceID  uuid.UUID `json:"workspace_id"`
	FinalizedAt  time.Time `json:"finalized_at"`
}

// StepName satisfies SagaStep.
func (FinalizeWorkspaceDeletion) StepName() string { return "finalize_workspace_deletion" }

// Execute satisfies SagaStep.
func (s FinalizeWorkspaceDeletion) Execute(_ context.Context, in CleanupWorkspaceInput) (FinalizeOutput, error) {
	if in.mustFailHere(s.StepName()) {
		return FinalizeOutput{}, saga.StepFailure(s.StepName(), "forced failure (chaos test)")
	}
	return FinalizeOutput{WorkspaceID: in.WorkspaceID, FinalizedAt: time.Now().UTC()}, nil
}

// Compensate satisfies SagaStep — terminal step, no compensation.
func (FinalizeWorkspaceDeletion) Compensate(_ context.Context, _ CleanupWorkspaceInput) error { return nil }
