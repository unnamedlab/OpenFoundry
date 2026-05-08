package interop

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestNormalizesExternalTrackingAndFrameworkNames(t *testing.T) {
	t.Parallel()
	tracking := NormalizeTrackingSource(models.ExternalTrackingSource{
		System:    "Weights & Biases",
		Framework: "sklearn",
		Flavor:    "MLflow.pyfunc",
		RunID:     "run-01",
	})
	assert.Equal(t, "wandb", tracking.System)
	assert.Equal(t, "scikit-learn", tracking.Framework)
	assert.Equal(t, "pyfunc", tracking.Flavor)
	assert.Equal(t, "pytorch", NormalizeFrameworkName("torch"))
}

func TestMergesExternalTrackingIntoRunParamsAndArtifacts(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{
		System:      "mlflow",
		Framework:   "onnx",
		ModelURI:    "models:/fraud-detector/12",
		ArtifactURI: "s3://mlflow-artifacts/fraud-detector/12",
		Params:      json.RawMessage(`{"max_depth":8}`),
		Artifacts: []models.ArtifactReference{{
			ID:           uuid.New(),
			Name:         "conda.yaml",
			URI:          "s3://mlflow-artifacts/fraud-detector/12/conda.yaml",
			ArtifactType: "environment",
		}},
	}
	params := MergeRunParams(json.RawMessage(`{"learning_rate":0.2}`), external)
	artifacts := MergeRunArtifacts(nil, external)

	var paramsObj map[string]any
	require.NoError(t, json.Unmarshal(params, &paramsObj))
	assert.Equal(t, "onnx", paramsObj["framework"])
	assert.Contains(t, paramsObj, "external_tracking")

	uris := []string{}
	for _, a := range artifacts {
		uris = append(uris, a.URI)
	}
	assert.Contains(t, uris, external.ModelURI)
	assert.Contains(t, uris, "s3://mlflow-artifacts/fraud-detector/12/conda.yaml")
}

func TestMergesExternalTrainingIntoTrainingConfig(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{
		System:    "mlflow",
		Framework: "xgboost",
		ModelURI:  "models:/churn-model/9",
	}
	configRaw := MergeTrainingConfigWithExternal(json.RawMessage(`{}`), external)
	var config map[string]any
	require.NoError(t, json.Unmarshal(configRaw, &config))
	assert.Equal(t, "xgboost", config["engine"])
	assert.Contains(t, config, "model_adapter")
	assert.Equal(t, "models:/churn-model/9", PreferredArtifactURI(external, configRaw))
}

func TestDedupesMetricsByName(t *testing.T) {
	t.Parallel()
	merged := MergeMetrics(
		[]models.MetricValue{{Name: "accuracy", Value: 0.91}},
		[]models.MetricValue{
			{Name: "accuracy", Value: 0.88},
			{Name: "roc_auc", Value: 0.94},
		},
	)
	require.Len(t, merged, 2)
	assert.Equal(t, "accuracy", merged[0].Name)
	assert.Equal(t, 0.91, merged[0].Value, "primary takes precedence")
	assert.Equal(t, "roc_auc", merged[1].Name)
}

func TestEffectiveFrameworkPriority(t *testing.T) {
	t.Parallel()
	// training_config.framework wins over engine + external
	cfg := json.RawMessage(`{"framework":"sklearn","engine":"pytorch","external_training":{"system":"mlflow","framework":"xgboost"}}`)
	assert.Equal(t, "scikit-learn", EffectiveFramework(cfg))

	// model_adapter.framework wins over engine when training_config.framework absent
	cfg = json.RawMessage(`{"model_adapter":{"framework":"torch"},"engine":"keras"}`)
	assert.Equal(t, "pytorch", EffectiveFramework(cfg))

	// engine fallback
	cfg = json.RawMessage(`{"engine":"tf"}`)
	assert.Equal(t, "tensorflow", EffectiveFramework(cfg))

	// external.framework when nothing else
	cfg = json.RawMessage(`{"external_training":{"system":"mlflow","framework":"lightgbm"}}`)
	assert.Equal(t, "lightgbm", EffectiveFramework(cfg))

	// default
	assert.Equal(t, "tabular-logistic", EffectiveFramework(json.RawMessage(`{}`)))
	assert.Equal(t, "tabular-logistic", EffectiveFramework(nil))
}

func TestInferModelAdapterFlavorDefaults(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{"framework":"scikit-learn"}`)
	adapter := InferModelAdapter(cfg, nil)
	assert.Equal(t, "scikit-learn", adapter.Framework)
	assert.Equal(t, "joblib", adapter.Flavor)
	assert.Equal(t, "native", adapter.Kind)
	assert.Equal(t, "in-process", adapter.Runtime, "native kind always runs in-process")
	assert.Equal(t, "joblib", adapter.Loader)

	cfg = json.RawMessage(`{"framework":"pytorch"}`)
	adapter = InferModelAdapter(cfg, nil)
	assert.Equal(t, "torchscript", adapter.Flavor)
	assert.Equal(t, "torch", adapter.Loader)

	cfg = json.RawMessage(`{"framework":"tensorflow"}`)
	adapter = InferModelAdapter(cfg, nil)
	assert.Equal(t, "savedmodel", adapter.Flavor)
	assert.Equal(t, "tensorflow", adapter.Loader)

	cfg = json.RawMessage(`{"framework":"onnx"}`)
	adapter = InferModelAdapter(cfg, nil)
	assert.Equal(t, "onnx", adapter.Flavor)
	assert.Equal(t, "onnx", adapter.Loader)
}

func TestInferModelAdapterExternalKindAndRuntime(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{System: "mlflow", Framework: "onnx"}
	adapter := InferModelAdapter(json.RawMessage(`{}`), external)
	assert.Equal(t, "external", adapter.Kind)
	assert.Equal(t, "onnxruntime", adapter.Runtime)
	assert.Equal(t, "mlflow", adapter.Loader, "mlflow tracking system always loads via mlflow loader")
}

func TestInferRegistrySourcePullsFromTracking(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{
		System:                 "mlflow",
		RegisteredModelName:    "fraud-detector",
		RegisteredModelVersion: "12",
		Stage:                  "production",
		ModelURI:               "models:/fraud-detector/12",
	}
	got := InferRegistrySource(external, nil)
	require.NotNil(t, got)
	assert.Equal(t, "mlflow", got.System)
	assert.Equal(t, "fraud-detector", got.ModelName)
	assert.Equal(t, "12", got.ModelVersion)
	assert.Equal(t, "production", got.Stage)
	assert.Equal(t, "models:/fraud-detector/12", got.URI)
}

func TestInferRegistrySourceExistingTakesPriority(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{
		System:              "wandb",
		RegisteredModelName: "fraud-detector",
	}
	existing := &models.RegistrySourceDescriptor{
		System:    "Weights & Biases", // gets normalised
		ModelName: "credit-scorer",     // overrides external
	}
	got := InferRegistrySource(external, existing)
	require.NotNil(t, got)
	assert.Equal(t, "wandb", got.System, "existing system gets re-normalised")
	assert.Equal(t, "credit-scorer", got.ModelName)
}

func TestInferRegistrySourceReturnsNilWhenEmpty(t *testing.T) {
	t.Parallel()
	assert.Nil(t, InferRegistrySource(nil, nil))
	empty := &models.ExternalTrackingSource{}
	assert.Nil(t, InferRegistrySource(empty, nil))
}

func TestNormalizeModelVersionSchemaSetsSignatureAndArtifactURI(t *testing.T) {
	t.Parallel()
	// model_state present → signature=tabular
	schema := NormalizeModelVersionSchema(
		json.RawMessage(`{"model_state":{"weights":[1,2]}}`),
		"s3://bucket/model.bin", nil, nil, nil, nil,
	)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(schema, &obj))
	assert.Equal(t, "tabular", obj["signature"])
	assert.Equal(t, "s3://bucket/model.bin", obj["artifact_uri"])

	// no model_state → signature=external-model
	schema = NormalizeModelVersionSchema(json.RawMessage(`{}`), "", nil, nil, nil, nil)
	require.NoError(t, json.Unmarshal(schema, &obj))
	assert.Equal(t, "external-model", obj["signature"])
}

func TestNormalizeModelVersionSchemaFoldsExternalTracking(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{
		System:    "mlflow",
		Framework: "xgboost",
		ModelURI:  "models:/churn-model/9",
	}
	schema := NormalizeModelVersionSchema(
		json.RawMessage(`{}`), "", nil, nil, nil, external,
	)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(schema, &obj))
	assert.Contains(t, obj, "external_tracking")
	assert.Contains(t, obj, "model_adapter")
	assert.Contains(t, obj, "registry_source")
	assert.Equal(t, "models:/churn-model/9", obj["artifact_uri"])
}

func TestPreferredArtifactURIPriority(t *testing.T) {
	t.Parallel()
	external := &models.ExternalTrackingSource{
		ModelURI:    "models:/a/1",
		ArtifactURI: "s3://a",
		RunURI:      "wandb://run/1",
	}
	assert.Equal(t, "models:/a/1", PreferredArtifactURI(external, nil), "model_uri wins")

	external = &models.ExternalTrackingSource{ArtifactURI: "s3://a"}
	assert.Equal(t, "s3://a", PreferredArtifactURI(external, nil), "artifact_uri when no model")

	external = &models.ExternalTrackingSource{RunURI: "wandb://run/1"}
	assert.Equal(t, "wandb://run/1", PreferredArtifactURI(external, nil))

	cfg := json.RawMessage(`{"artifact_uri":"s3://config"}`)
	assert.Equal(t, "s3://config", PreferredArtifactURI(nil, cfg))

	assert.Equal(t, "", PreferredArtifactURI(nil, nil))
}

func TestNormalizeTrackingSystemAliases(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                     "",
		"weightsandbiases":     "wandb",
		"Weights & Biases":     "wandb",
		"wandb":                "wandb",
		"mlflow":               "mlflow",
		"databricks-mlflow":    "mlflow",
		"sagemaker":            "sagemaker",
		"amazon-sagemaker":     "sagemaker",
		"AzureML":              "azureml",
		"azure-ml":             "azureml",
		"vertex-ai":            "vertexai",
		"comet":                "comet",
		"cometml":              "comet",
		"neptuneai":            "neptune",
		"unknown-system":       "unknown-system",
	}
	for in, want := range cases {
		assert.Equal(t, want, normalizeTrackingSystem(in), "system %q", in)
	}
}

func TestNormalizeFlavorAliases(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "pyfunc", normalizeFlavorName("MLflow.pyfunc"))
	assert.Equal(t, "joblib", normalizeFlavorName("sklearn"))
	assert.Equal(t, "torchscript", normalizeFlavorName("torch-script"))
	assert.Equal(t, "savedmodel", normalizeFlavorName("tensorflow.savedmodel"))
	assert.Equal(t, "pickle", normalizeFlavorName("cloudpickle"))
	assert.Equal(t, "", normalizeFlavorName("  "))
}

func TestRuntimeForAdapterMatrix(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "in-process", runtimeForAdapter("anything", "anything", "native"))
	assert.Equal(t, "onnxruntime", runtimeForAdapter("onnx", "", "external"))
	assert.Equal(t, "torchserve-compatible", runtimeForAdapter("pytorch", "", "external"))
	assert.Equal(t, "tensorflow-serving-compatible", runtimeForAdapter("tensorflow", "", "external"))
	assert.Equal(t, "transformers-runtime", runtimeForAdapter("huggingface", "", "external"))
	assert.Equal(t, "external-serving", runtimeForAdapter("", "", "external"))
	assert.Equal(t, "python-remote", runtimeForAdapter("xgboost", "", "external"))
}

func TestNormalizeObjectOrNullKinds(t *testing.T) {
	t.Parallel()
	// object passes through
	got := normalizeObjectOrNull(json.RawMessage(`{"k":1}`))
	assert.JSONEq(t, `{"k":1}`, string(got))

	// null returns "null" raw
	got = normalizeObjectOrNull(json.RawMessage(`null`))
	assert.Equal(t, "null", strings.TrimSpace(string(got)))

	// empty → nil
	assert.Nil(t, normalizeObjectOrNull(json.RawMessage(``)))

	// string wraps in {"value": <raw>}
	got = normalizeObjectOrNull(json.RawMessage(`"hello"`))
	assert.JSONEq(t, `{"value":"hello"}`, string(got))

	// number wraps too
	got = normalizeObjectOrNull(json.RawMessage(`42`))
	assert.JSONEq(t, `{"value":42}`, string(got))
}

func TestDedupeArtifactsKeepsFirstURI(t *testing.T) {
	t.Parallel()
	id1, id2 := uuid.New(), uuid.New()
	primary := []models.ArtifactReference{
		{ID: id1, URI: "s3://a"},
	}
	secondary := []models.ArtifactReference{
		{ID: id2, URI: "s3://a"}, // same URI → drop
		{ID: uuid.New(), URI: "s3://b"},
	}
	merged := dedupeArtifacts(primary, secondary)
	require.Len(t, merged, 2)
	assert.Equal(t, id1, merged[0].ID, "first URI wins")
	assert.Equal(t, "s3://b", merged[1].URI)
}
