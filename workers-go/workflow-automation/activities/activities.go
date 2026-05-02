// Package activities hosts the activity implementations called by
// workflows in this module. Activities are **thin gRPC clients** of
// Rust services — they never touch Cassandra/Postgres directly.
//
// Each activity:
//
//   - Reads its target service URL from an env var
//     (`OF_<SERVICE>_GRPC_ADDR`).
//   - Propagates the audit correlation ID from the workflow context
//     as the `x-audit-correlation-id` gRPC header.
//   - Returns an idempotent error type so retries are safe.
package activities

import (
	"context"
	"errors"
	"os"

	"google.golang.org/grpc/metadata"

	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/internal/contract"
)

// Activities groups every activity wired into the worker. The struct
// is registered as a single bundle so methods become activity names
// of the form `Activities.<MethodName>`.
type Activities struct{}

// ExecuteOntologyAction is the canonical activity that translates an
// automation step into a call against `ontology-actions-service`. The
// real proto client lands once `proto/ontology/actions.proto` is
// regenerated for Go (`buf generate`); the activity body below is a
// substrate stub that returns ErrNotImplemented so the worker
// compiles + registers.
func (a *Activities) ExecuteOntologyAction(
	ctx context.Context,
	input contract.AutomationRunInput,
) (map[string]any, error) {
	_ = withAuditCorrelation(ctx, input.RunID)
	addr := os.Getenv("OF_ONTOLOGY_ACTIONS_GRPC_ADDR")
	if addr == "" {
		return nil, errors.New("OF_ONTOLOGY_ACTIONS_GRPC_ADDR not set")
	}
	// TODO(S2.3.c): instantiate the generated gRPC client and call
	// ontology.actions.v1.ActionsService/ExecuteAction.
	return nil, ErrNotImplemented
}

// ErrNotImplemented marks substrate stubs. Workflows currently do
// not call these activities — the legacy Rust executor branches will
// be reified PR-by-PR (S2.3.b) and each PR replaces one stub with a
// real gRPC client.
var ErrNotImplemented = errors.New("activity stub: implementation pending S2.3.c")

func withAuditCorrelation(ctx context.Context, correlationID string) context.Context {
	if correlationID == "" {
		return ctx
	}
	md := metadata.Pairs(contract.HeaderAuditCorrelation, correlationID)
	return metadata.NewOutgoingContext(ctx, md)
}
