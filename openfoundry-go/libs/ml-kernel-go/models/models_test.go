package models

import (
	"encoding/json"
	"testing"

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
