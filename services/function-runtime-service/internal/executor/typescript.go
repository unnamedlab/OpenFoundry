package executor

import (
	"context"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// TSStubExecutor runs TypeScript-authored functions by shelling out to
// `node` (or whatever NodeBinary points at). v0 only — see README.md
// for the v1 replacement path.
type TSStubExecutor struct {
	NodeBinary string
	Limits     Limits
}

// NewTSStubExecutor returns a TS executor bound to nodeBin (resolved
// from $PATH when empty).
func NewTSStubExecutor(nodeBin string, lim Limits) *TSStubExecutor {
	if nodeBin == "" {
		nodeBin = "node"
	}
	return &TSStubExecutor{NodeBinary: nodeBin, Limits: lim}
}

// Execute runs fn under the node binary.
func (e *TSStubExecutor) Execute(ctx context.Context, _ models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error) {
	return runScript(ctx, e.NodeBinary, version.SourceURI, input, e.Limits)
}
