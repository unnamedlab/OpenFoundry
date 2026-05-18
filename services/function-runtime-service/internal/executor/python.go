package executor

import (
	"context"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// PythonProcessExecutor runs Python-authored functions by launching
// `python3` (or whatever PythonBinary points at). v0 only — see
// README.md for the v1 replacement path.
type PythonProcessExecutor struct {
	PythonBinary string
	Limits       Limits
}

// NewPythonProcessExecutor returns a Python executor bound to pyBin
// (resolved from $PATH when empty).
func NewPythonProcessExecutor(pyBin string, lim Limits) *PythonProcessExecutor {
	if pyBin == "" {
		pyBin = "python3"
	}
	return &PythonProcessExecutor{PythonBinary: pyBin, Limits: lim}
}

// Execute runs fn under the python3 binary.
func (e *PythonProcessExecutor) Execute(ctx context.Context, _ models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error) {
	return runScript(ctx, e.PythonBinary, version.SourceURI, input, e.Limits)
}
