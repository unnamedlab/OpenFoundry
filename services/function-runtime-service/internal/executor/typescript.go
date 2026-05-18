package executor

import (
	"context"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// TSProcessExecutor runs TypeScript-authored functions by launching
// `node` (or whatever NodeBinary points at). v0 only — see README.md
// for the v1 replacement path.
type TSProcessExecutor struct {
	NodeBinary string
	Limits     Limits
}

// NewTSProcessExecutor returns a TS executor bound to nodeBin (resolved
// from $PATH when empty).
func NewTSProcessExecutor(nodeBin string, lim Limits) *TSProcessExecutor {
	if nodeBin == "" {
		nodeBin = "node"
	}
	return &TSProcessExecutor{NodeBinary: nodeBin, Limits: lim}
}

// Execute runs fn under the node binary.
func (e *TSProcessExecutor) Execute(ctx context.Context, _ models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error) {
	return runScript(ctx, e.NodeBinary, version.SourceURI, input, e.Limits)
}
