package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

// ModelAssetNode is one node in the asset-lineage graph.
type ModelAssetNode struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Label    string          `json:"label"`
	Status   string          `json:"status"`
	Metadata json.RawMessage `json:"metadata"`
}

// ModelAssetEdge connects two nodes.
type ModelAssetEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"`
}

// ModelAssetLineageSummary is the counter aggregate.
type ModelAssetLineageSummary struct {
	DatasetCount     int      `json:"dataset_count"`
	RunCount         int      `json:"run_count"`
	TrainingJobCount int      `json:"training_job_count"`
	ModelCount       int      `json:"model_count"`
	VersionCount     int      `json:"version_count"`
	DeploymentCount  int      `json:"deployment_count"`
	Frameworks       []string `json:"frameworks"`
}

// ExperimentAssetLineageResponse is the GET .../lineage payload.
type ExperimentAssetLineageResponse struct {
	ExperimentID    uuid.UUID                 `json:"experiment_id"`
	ObjectiveStatus string                    `json:"objective_status"`
	Nodes           []ModelAssetNode          `json:"nodes"`
	Edges           []ModelAssetEdge          `json:"edges"`
	Summary         ModelAssetLineageSummary  `json:"summary"`
}
