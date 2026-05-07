// Package runtime wires the pipeline-build service to the
// openfoundry-pyruntime sidecar via libs/python-sidecar. The HTTP
// handler layer is still substrate-only; this file provides the
// Manager-backed runtime so the transform-execute port can drop in
// without revisiting wiring.
package runtime

import (
	"context"

	pythonsidecar "github.com/openfoundry/openfoundry-go/libs/python-sidecar"
)

// PythonTransform is the slice of the sidecar contract pipeline handlers
// need: stateless transform execution with prepared inputs.
type PythonTransform interface {
	Execute(ctx context.Context, source string, configJSON, preparedInputsJSON []byte, inputDatasetIDs []string, outputDatasetID string, timeoutSeconds uint32) (*pythonsidecar.PipelineTransformResult, error)
}

// SidecarTransform adapts *pythonsidecar.Manager to PythonTransform.
type SidecarTransform struct{ Mgr *pythonsidecar.Manager }

func (s SidecarTransform) Execute(ctx context.Context, source string, configJSON, preparedInputsJSON []byte, inputDatasetIDs []string, outputDatasetID string, timeoutSeconds uint32) (*pythonsidecar.PipelineTransformResult, error) {
	return s.Mgr.ExecutePipeline(ctx, source, configJSON, preparedInputsJSON, inputDatasetIDs, outputDatasetID, timeoutSeconds)
}
