package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatureDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0 * * * *", DefaultBatchSchedule)
	assert.Equal(t, int32(60), DefaultFreshnessSLAMinutes)
}

func TestModelDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "classification", DefaultProblemType)
}

func TestDeploymentDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "single", DefaultDeploymentStrategyType)
	assert.Equal(t, "24h", DefaultDeploymentMonitoringWindow)
}

func TestExperimentDefaultsMatchRust(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "classification", DefaultExperimentTaskType)
	assert.Equal(t, "accuracy", DefaultExperimentPrimaryMetric)
	assert.Equal(t, "draft", DefaultObjectiveStatus)
}

func TestMlStudioOverviewSnakeCase(t *testing.T) {
	t.Parallel()
	o := MlStudioOverview{
		ExperimentCount: 3, ActiveRunCount: 1, ModelCount: 5,
		ProductionModelCount: 2, FeatureCount: 12, OnlineFeatureCount: 8,
		DeploymentCount: 4, ABTestCount: 1, DriftAlertCount: 0,
		QueuedTrainingJobs: 2,
	}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	for _, k := range []string{
		"experiment_count", "active_run_count", "model_count",
		"production_model_count", "feature_count", "online_feature_count",
		"deployment_count", "ab_test_count", "drift_alert_count",
		"queued_training_jobs",
	} {
		assert.Contains(t, got, k, "snake_case key %s missing", k)
	}
}

func TestExternalTrackingSourceHasSignal(t *testing.T) {
	t.Parallel()
	empty := ExternalTrackingSource{}
	assert.False(t, empty.HasSignal(), "blank source has no signal")

	withSystem := ExternalTrackingSource{System: "mlflow"}
	assert.True(t, withSystem.HasSignal())

	withMetrics := ExternalTrackingSource{Metrics: []MetricValue{{Name: "rmse", Value: 0.42}}}
	assert.True(t, withMetrics.HasSignal())

	withTags := ExternalTrackingSource{Tags: json.RawMessage(`{"env":"prod"}`)}
	assert.True(t, withTags.HasSignal())

	whitespace := ExternalTrackingSource{System: "   "}
	assert.False(t, whitespace.HasSignal(), "whitespace-only fields don't count (matches Rust trim().is_empty())")
}

func TestModelAdapterDescriptorHasSignal(t *testing.T) {
	t.Parallel()
	empty := ModelAdapterDescriptor{}
	assert.False(t, empty.HasSignal())

	withKind := ModelAdapterDescriptor{Kind: "lora"}
	assert.True(t, withKind.HasSignal())
}

func TestRegistrySourceDescriptorHasSignal(t *testing.T) {
	t.Parallel()
	empty := RegistrySourceDescriptor{}
	assert.False(t, empty.HasSignal())

	withName := RegistrySourceDescriptor{ModelName: "fraud-detector"}
	assert.True(t, withName.HasSignal())
}

func TestModelVersionOmitsOptionalFields(t *testing.T) {
	t.Parallel()
	v := ModelVersion{
		Hyperparameters: json.RawMessage(`{}`),
		Schema:          json.RawMessage(`{}`),
	}
	b, err := json.Marshal(v)
	require.NoError(t, err)
	s := string(b)
	assert.NotContains(t, s, "model_adapter", "model_adapter omitempty when nil")
	assert.NotContains(t, s, "registry_source", "registry_source omitempty when nil")
	assert.NotContains(t, s, "external_tracking", "external_tracking omitempty when nil")
}

func TestExperimentRunJSONShape(t *testing.T) {
	t.Parallel()
	r := ExperimentRun{Status: "completed", Metrics: []MetricValue{{Name: "acc", Value: 0.92}}}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "completed", got["status"])
}

func TestRealtimePredictionRequestOmitemptyExplain(t *testing.T) {
	t.Parallel()
	req := RealtimePredictionRequest{}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	// explain is bool with omitempty — false elides.
	assert.NotContains(t, string(b), "explain")
}

func TestTrainingJobAutoRegisterFalseOmits(t *testing.T) {
	t.Parallel()
	req := CreateTrainingJobRequest{Name: "fraud-train"}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "auto_register_model_version")
}

func TestCreateModelVersionJSONContract(t *testing.T) {
	t.Parallel()
	payload := []byte(`{
		"version_label":"v7",
		"stage":"production",
		"hyperparameters":{"depth":4},
		"metrics":[{"name":"accuracy","value":0.98,"step":3}],
		"artifact_uri":"s3://models/fraud/v7",
		"schema":{"inputs":[{"name":"amount","type":"float"}]},
		"model_adapter":{"framework":"xgboost","runtime":"external-serving"},
		"registry_source":{"system":"mlflow","model_name":"fraud"},
		"external_tracking":{"system":"mlflow","run_id":"run-7"}
	}`)

	var req CreateModelVersionRequest
	require.NoError(t, json.Unmarshal(payload, &req))
	require.NotNil(t, req.VersionLabel)
	assert.Equal(t, "v7", *req.VersionLabel)
	require.NotNil(t, req.Stage)
	assert.Equal(t, "production", *req.Stage)
	require.NotNil(t, req.Metrics)
	require.Len(t, *req.Metrics, 1)
	assert.Equal(t, "accuracy", (*req.Metrics)[0].Name)
	require.NotNil(t, req.ArtifactURI)
	assert.Equal(t, "s3://models/fraud/v7", *req.ArtifactURI)
	require.NotNil(t, req.ModelAdapter)
	assert.Equal(t, "xgboost", req.ModelAdapter.Framework)
	require.NotNil(t, req.RegistrySource)
	assert.Equal(t, "mlflow", req.RegistrySource.System)
	require.NotNil(t, req.ExternalTracking)
	assert.Equal(t, "run-7", req.ExternalTracking.RunID)
}

func TestModelVersionResponseJSONContract(t *testing.T) {
	t.Parallel()
	artifactURI := "s3://models/fraud/v7"
	createdAt := mustParseTime(t, "2026-05-07T12:00:00Z")
	promotedAt := mustParseTime(t, "2026-05-07T12:05:00Z")
	version := ModelVersion{
		ID:               mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		ModelID:          mustUUID(t, "22222222-2222-2222-2222-222222222222"),
		VersionNumber:    7,
		VersionLabel:     "v7",
		Stage:            "production",
		Hyperparameters:  json.RawMessage(`{"depth":4}`),
		Metrics:          []MetricValue{{Name: "accuracy", Value: 0.98}},
		ArtifactURI:      &artifactURI,
		Schema:           json.RawMessage(`{"inputs":[{"name":"amount","type":"float"}]}`),
		ModelAdapter:     &ModelAdapterDescriptor{Framework: "xgboost", Runtime: "external-serving"},
		RegistrySource:   &RegistrySourceDescriptor{System: "mlflow", ModelName: "fraud"},
		ExternalTracking: &ExternalTrackingSource{System: "mlflow", RunID: "run-7"},
		CreatedAt:        createdAt,
		PromotedAt:       &promotedAt,
	}

	b, err := json.Marshal(version)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	for _, key := range []string{
		"id", "model_id", "version_number", "version_label", "stage",
		"source_run_id", "training_job_id", "hyperparameters", "metrics",
		"artifact_uri", "schema", "model_adapter", "registry_source",
		"external_tracking", "created_at", "promoted_at",
	} {
		assert.Contains(t, got, key)
	}
	assert.Equal(t, "v7", got["version_label"])
	assert.Equal(t, "production", got["stage"])
	assert.Equal(t, "s3://models/fraud/v7", got["artifact_uri"])
	assert.Equal(t, float64(7), got["version_number"])
}

func mustUUID(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	require.NoError(t, err)
	return id
}

func mustParseTime(t *testing.T, raw string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, raw)
	require.NoError(t, err)
	return v
}
