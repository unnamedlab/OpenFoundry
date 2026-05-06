package training

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportsExternalTrackingRunsIntoTrainingExecution(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
        "external_training": {
            "system": "mlflow",
            "run_id": "run-42",
            "framework": "xgboost",
            "model_uri": "models:/fraud-detector/12",
            "params": { "max_depth": 8, "eta": 0.12 },
            "metrics": [
                { "name": "roc_auc", "value": 0.94 },
                { "name": "log_loss", "value": 0.18 }
            ]
        }
    }`)
	exec, err := ExecuteTraining(cfg, json.RawMessage(`{"strategy":"external-import"}`), "roc_auc")
	require.NoError(t, err)
	require.NotNil(t, exec)

	require.Len(t, exec.Trials, 1)
	assert.Equal(t, "imported-run-42", exec.Trials[0].ID)
	assert.Equal(t, "roc_auc", exec.Trials[0].ObjectiveMetric.Name)
	assert.Equal(t, "models:/fraud-detector/12", exec.BestArtifactURI)

	require.NotNil(t, exec.BestSchema)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(exec.BestSchema, &schema))
	adapter, ok := schema["model_adapter"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "xgboost", adapter["framework"])
}

func TestSyntheticTrialsWhenNoInlineRecords(t *testing.T) {
	t.Parallel()
	exec, err := ExecuteTraining(json.RawMessage(`{}`), nil, "accuracy")
	require.NoError(t, err)
	require.NotNil(t, exec)
	require.Len(t, exec.Trials, 3)
	for i, trial := range exec.Trials {
		assert.Equal(t, "completed", trial.Status)
		assert.Equal(t, "trial-"+itoa(i+1), trial.ID)
		assert.Equal(t, "accuracy", trial.ObjectiveMetric.Name)
		assert.InDelta(t, 0.5+float64(i)*0.05, trial.ObjectiveMetric.Value, 0.0001)
	}
	assert.Empty(t, exec.BestMetrics, "synthetic trials don't carry metrics")
}

func TestExecuteTrainingInlineRecordsSortByObjectiveDesc(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
        "label_field": "label",
        "positive_label": "positive",
        "records": [
            { "label": "positive", "x": 1, "y": -0.8 },
            { "label": "positive", "x": 0.9, "y": -0.7 },
            { "label": "negative", "x": -1, "y": 0.8 },
            { "label": "negative", "x": -0.9, "y": 0.7 }
        ]
    }`)
	search := json.RawMessage(`{"candidates":[
        {"learning_rate":0.01,"epochs":50},
        {"learning_rate":0.5,"epochs":400}
    ]}`)
	exec, err := ExecuteTraining(cfg, search, "f1")
	require.NoError(t, err)
	require.Len(t, exec.Trials, 2)
	assert.GreaterOrEqual(t, exec.Trials[0].ObjectiveMetric.Value, exec.Trials[1].ObjectiveMetric.Value)
	assert.NotEmpty(t, exec.BestMetrics)
	assert.NotNil(t, exec.BestSchema)
	assert.NotEmpty(t, exec.BestHyperparameters)
}

func TestIsJSONObjectDetection(t *testing.T) {
	t.Parallel()
	assert.True(t, isJSONObject(json.RawMessage(`{"k":1}`)))
	assert.True(t, isJSONObject(json.RawMessage(`  {"k":1}`)))
	assert.False(t, isJSONObject(json.RawMessage(`[1,2]`)))
	assert.False(t, isJSONObject(json.RawMessage(`null`)))
	assert.False(t, isJSONObject(json.RawMessage(``)))
}
