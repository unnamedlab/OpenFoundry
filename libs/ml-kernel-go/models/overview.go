// Package models is the wire-format DTO surface of the OpenFoundry
// ML plane (libs/ml-kernel in Rust).
//
// Foundation slice ports the entire models sub-domain (~800 LOC of
// Rust → ~1000 LOC of Go) in one iteration, mirroring the Rust
// per-file layout: feature, model, model_version, deployment,
// experiment, run, prediction, training_job, asset_lineage, interop.
//
// Wire-compat: snake_case JSON tags matching Rust serde defaults;
// defaults from Rust `default = "fn"` annotations pinned in tests.
package models

// MlStudioOverview is the canonical summary payload the ML platform
// landing card renders.
type MlStudioOverview struct {
	ExperimentCount       int64 `json:"experiment_count"`
	ActiveRunCount        int64 `json:"active_run_count"`
	ModelCount            int64 `json:"model_count"`
	ProductionModelCount  int64 `json:"production_model_count"`
	FeatureCount          int64 `json:"feature_count"`
	OnlineFeatureCount    int64 `json:"online_feature_count"`
	DeploymentCount       int64 `json:"deployment_count"`
	ABTestCount           int64 `json:"ab_test_count"`
	DriftAlertCount       int64 `json:"drift_alert_count"`
	QueuedTrainingJobs    int64 `json:"queued_training_jobs"`
}
