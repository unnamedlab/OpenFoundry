package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelAdapterJSONShape(t *testing.T) {
	t.Parallel()
	id, modelID := uuid.New(), uuid.New()
	a := ModelAdapter{
		ID: id, Slug: "llama2-7b", Name: "Llama 2 7B",
		AdapterKind: "lora", ArtifactURI: "s3://bucket/llama",
		ModelID: &modelID, Status: "registered",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	b, err := json.Marshal(a)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "llama2-7b", got["slug"])
	assert.Equal(t, "lora", got["adapter_kind"])
	assert.Equal(t, "s3://bucket/llama", got["artifact_uri"])
	assert.Equal(t, "registered", got["status"])
	assert.Equal(t, modelID.String(), got["model_id"])
}

func TestInferenceContractJSONShape(t *testing.T) {
	t.Parallel()
	c := InferenceContract{
		ID: uuid.New(), AdapterID: uuid.New(), Version: "1.0",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"array"}`),
		CreatedAt:    time.Now().UTC(),
	}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "1.0", got["version"])
	assert.NotNil(t, got["input_schema"])
	assert.NotNil(t, got["output_schema"])
}

func TestModelSubmissionJSONShape(t *testing.T) {
	t.Parallel()
	s := ModelSubmission{
		ID: uuid.New(), ModelID: uuid.New(), Version: "v1",
		Stage: "submitted", Status: "pending",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "submitted", got["stage"])
	assert.Equal(t, "pending", got["status"])
}

func TestModelingObjectiveJSONShape(t *testing.T) {
	t.Parallel()
	o := ModelingObjective{
		ID: uuid.New(), Slug: "fraud-recall-95",
		Name: "Fraud detection recall ≥ 95%",
		SuccessCriteria: json.RawMessage(`{"recall":0.95}`),
		CreatedAt:       time.Now().UTC(),
	}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "fraud-recall-95", got["slug"])
}
