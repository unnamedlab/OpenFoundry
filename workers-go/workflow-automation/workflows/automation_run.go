// Package workflows hosts the Temporal workflow definitions for the
// `workflow-automation` task queue. S2.3.b instructs us to translate
// every "tipo de workflow" of the legacy Rust executor
// (`services/workflow-automation-service/src/domain/executor.rs`,
// archived in S2.3.a) into a Temporal workflow definition here.
//
// Patterns enforced:
//
//   - Workflows MUST be deterministic. No `time.Now()`, no `rand`,
//     no direct I/O — use `workflow.Now(ctx)`, `workflow.NewRandom`,
//     and activities for everything that talks to the outside world.
//   - Activities MUST be thin gRPC clients of Rust services. They
//     never read or write Cassandra/Postgres directly. The owning
//     Rust service is the single source of truth (audit + Cedar
//     authz).
//   - Every activity propagates the `audit_correlation_id` from the
//     workflow input as the `x-audit-correlation-id` gRPC header.
package workflows

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/internal/contract"
)

// AutomationRun is the entry-point workflow for every business
// automation triggered by the Rust adapter
// (`services/workflow-automation-service`). The body is a placeholder
// today — concrete branches (`branching`, `parallel`, `compensation`,
// `human_in_loop`, `simulation`) land per-PR alongside the matching
// activities, mirroring the structure of the archived Rust executor.
func AutomationRun(ctx workflow.Context, input contract.AutomationRunInput) (contract.AutomationRunResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("AutomationRun started",
		"run_id", input.RunID,
		"definition_id", input.DefinitionID,
		"tenant_id", input.TenantID,
	)

	// Default activity options — child workflows / specific activities
	// are free to override. Retry policy mirrors the Rust executor's
	// defaults (5 attempts, 30 s initial backoff, 2x exponential).
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Real branching lands in subsequent PRs (S2.3.b per workflow
	// type). Today we materialise the substrate so the worker
	// compiles, registers, and the Rust client side can be wired
	// against a deployed Temporal cluster.
	logger.Info("AutomationRun substrate-only completion (no steps registered yet)")

	return contract.AutomationRunResult{
		RunID:  input.RunID,
		Status: "completed",
	}, nil
}
