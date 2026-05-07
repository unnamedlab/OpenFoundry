package lineage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// StartPipelineRun ports `domain::executor::start_pipeline_run`.
//
// The Rust source is also a stub:
//
//	"pipeline lineage build handoff is not wired in lineage-service for pipeline {id}"
//
// `lineage-service` owns lineage reads + workflow fan-out; the
// pipeline build engine lives in pipeline-build-service. The lineage
// build trigger therefore reports the pipeline as `failed` with the
// same error string until a handoff RPC lands.
func StartPipelineRun(_ context.Context, _ *AppState, pipeline *models.Pipeline, _ *uuid.UUID, _, _ string, _ *string, _ *uuid.UUID, _ int32, _ int, _ bool, _ json.RawMessage) (*models.PipelineRun, error) {
	return nil, fmt.Errorf("pipeline lineage build handoff is not wired in lineage-service for pipeline %s", pipeline.ID)
}
