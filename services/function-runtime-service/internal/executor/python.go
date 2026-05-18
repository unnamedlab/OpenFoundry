package executor

import (
	"context"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// PythonStubExecutor runs Python-authored functions by shelling out to
// `python3` (or whatever PythonBinary points at). v0 only — see
// README.md for the v1 replacement path.
type PythonStubExecutor struct {
	PythonBinary string
	Limits       Limits
}

// NewPythonStubExecutor returns a Python executor bound to pyBin
// (resolved from $PATH when empty).
func NewPythonStubExecutor(pyBin string, lim Limits) *PythonStubExecutor {
	if pyBin == "" {
		pyBin = "python3"
	}
	return &PythonStubExecutor{PythonBinary: pyBin, Limits: lim}
}

// Execute runs fn under the python3 binary.
func (e *PythonStubExecutor) Execute(ctx context.Context, _ models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error) {
	return runScript(ctx, e.PythonBinary, version.SourceURI, input, e.Limits)
}
