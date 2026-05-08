package training

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestTrainsRealLogisticTrialFromInlineRecords(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
        "engine": "tabular-logistic",
        "label_field": "label",
        "positive_label": "positive",
        "records": [
            { "label": "positive", "tickets_open": 9, "usage_delta": -0.8, "nps": 2 },
            { "label": "positive", "tickets_open": 7, "usage_delta": -0.6, "nps": 3 },
            { "label": "negative", "tickets_open": 1, "usage_delta": 0.2, "nps": 9 },
            { "label": "negative", "tickets_open": 2, "usage_delta": 0.4, "nps": 8 }
        ]
    }`)
	hyper := json.RawMessage(`{"learning_rate":0.1,"epochs":400,"l2":0.0}`)

	assert.True(t, HasInlineTrainingData(cfg))
	outcome, err := TrainTrial(cfg, hyper, "f1", 0)
	require.NoError(t, err)
	assert.Equal(t, "completed", outcome.Trial.Status)
	assert.Equal(t, "trial-1", outcome.Trial.ID)
	assert.GreaterOrEqual(t, outcome.Trial.ObjectiveMetric.Value, 0.8)

	var schemaObj map[string]any
	require.NoError(t, json.Unmarshal(outcome.Schema, &schemaObj))
	modelState, ok := schemaObj["model_state"].(map[string]any)
	require.True(t, ok)
	weights, ok := modelState["weights"].([]any)
	require.True(t, ok)
	assert.NotEmpty(t, weights)
}

func TestHasInlineTrainingDataFalsePaths(t *testing.T) {
	t.Parallel()
	assert.False(t, HasInlineTrainingData(nil))
	assert.False(t, HasInlineTrainingData(json.RawMessage(`{}`)))
	assert.False(t, HasInlineTrainingData(json.RawMessage(`{"records":[]}`)))
	assert.False(t, HasInlineTrainingData(json.RawMessage(`not-json`)))
	assert.True(t, HasInlineTrainingData(json.RawMessage(`{"records":[{"a":1}]}`)))
}

func TestParseDatasetRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := parseDataset(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty array")

	_, err = parseDataset(json.RawMessage(`{"records":[]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one row")
}

func TestParseDatasetMissingLabelField(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"records":[{"a":1}]}`)
	_, err := parseDataset(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing label field 'label'")
}

func TestParseDatasetCustomLabelField(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
        "label_field": "is_churn",
        "positive_label": "yes",
        "records":[{"is_churn":"yes","x":1.0,"y":2.0},{"is_churn":"no","x":3.0,"y":4.0}]
    }`)
	ds, err := parseDataset(cfg)
	require.NoError(t, err)
	assert.Equal(t, "is_churn", ds.LabelField)
	assert.Equal(t, "yes", ds.PositiveLabel)
	assert.Equal(t, []float64{1.0, 0.0}, ds.Labels)
}

func TestScalarFeatureCoercion(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 7.5, scalarFeature(7.5))
	assert.Equal(t, 1.0, scalarFeature(true))
	assert.Equal(t, 0.0, scalarFeature(false))
	assert.Equal(t, 3.14, scalarFeature("3.14"))
	// non-numeric strings hash deterministically into [0, 1)
	v := scalarFeature("hello")
	assert.GreaterOrEqual(t, v, 0.0)
	assert.Less(t, v, 1.0)
	assert.Equal(t, 0.0, scalarFeature(nil))
}

func TestBinaryLabelMappings(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1.0, binaryLabel(true, "positive"))
	assert.Equal(t, 0.0, binaryLabel(false, "positive"))
	assert.Equal(t, 1.0, binaryLabel(0.6, "positive"))
	assert.Equal(t, 0.0, binaryLabel(0.4, "positive"))
	assert.Equal(t, 1.0, binaryLabel("positive", "positive"))
	assert.Equal(t, 1.0, binaryLabel("True", "positive"), "case-insensitive true match")
	assert.Equal(t, 1.0, binaryLabel("1", "positive"))
	assert.Equal(t, 0.0, binaryLabel("negative", "positive"))
	assert.Equal(t, 0.0, binaryLabel(nil, "positive"))
}

func TestRoundMetricFourDecimals(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0.6667, roundMetric(0.66666))
	assert.Equal(t, 0.5, roundMetric(0.5))
	assert.Equal(t, 1.0, roundMetric(0.99996))
}

func TestEvaluateMetricsBoundaryCases(t *testing.T) {
	t.Parallel()
	// Empty dataset → all metrics 0; total clamps to 1 to avoid div/0
	ds := &trainingDataset{}
	metrics := evaluateMetrics(ds, nil, 0)
	require.Len(t, metrics, 5)
	for _, m := range metrics {
		assert.Equal(t, 0.0, m.Value, "metric %s", m.Name)
	}

	// Perfectly-separable single-feature dataset → accuracy=1
	ds = &trainingDataset{
		Rows:   [][]float64{{1.0}, {1.0}, {-1.0}, {-1.0}},
		Labels: []float64{1, 1, 0, 0},
	}
	metrics = evaluateMetrics(ds, []float64{10.0}, 0)
	got := selectMetric(metrics, "accuracy")
	require.NotNil(t, got)
	assert.Equal(t, 1.0, got.Value)
}

func TestSelectMetricFallback(t *testing.T) {
	t.Parallel()
	metrics := []models.MetricValue{
		{Name: "accuracy", Value: 0.9},
		{Name: "f1", Value: 0.85},
	}
	got := selectMetric(metrics, "f1")
	require.NotNil(t, got)
	assert.Equal(t, 0.85, got.Value)
	assert.Nil(t, selectMetric(metrics, "nope"))
}
