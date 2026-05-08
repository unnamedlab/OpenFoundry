// Package interop hosts the model-version schema normalisation +
// tracking-source merging + framework / adapter inference helpers.
//
// Mirrors libs/ml-kernel/src/domain/interop.rs verbatim. Every public
// function pairs 1:1 with the Rust side and the wire-format output
// is byte-identical (modulo map iteration order, which is a Rust
// BTreeMap on serialisation; Go's encoding/json sorts map keys
// alphabetically by default → identical output).
package interop

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// NormalizeTrackingSource mirrors fn normalize_tracking_source.
// Trims every string field, normalises system / framework / flavor
// vocabularies, dedupes metrics + artifacts.
func NormalizeTrackingSource(s models.ExternalTrackingSource) models.ExternalTrackingSource {
	return models.ExternalTrackingSource{
		System:                 normalizeTrackingSystem(s.System),
		Project:                strings.TrimSpace(s.Project),
		ExperimentName:         strings.TrimSpace(s.ExperimentName),
		RunID:                  strings.TrimSpace(s.RunID),
		RunName:                strings.TrimSpace(s.RunName),
		RunURI:                 strings.TrimSpace(s.RunURI),
		ArtifactURI:            strings.TrimSpace(s.ArtifactURI),
		ModelURI:               strings.TrimSpace(s.ModelURI),
		RegisteredModelName:    strings.TrimSpace(s.RegisteredModelName),
		RegisteredModelVersion: strings.TrimSpace(s.RegisteredModelVersion),
		Framework:              NormalizeFrameworkName(s.Framework),
		Flavor:                 normalizeFlavorName(s.Flavor),
		Stage:                  strings.TrimSpace(s.Stage),
		Tags:                   normalizeObjectOrNull(s.Tags),
		Params:                 normalizeObjectOrNull(s.Params),
		Metrics:                dedupeMetrics(s.Metrics, nil),
		Artifacts:              dedupeArtifacts(s.Artifacts, nil),
		Metadata:               normalizeObjectOrNull(s.Metadata),
	}
}

// TrackingSourceFromParams mirrors fn tracking_source_from_params —
// reads "external_tracking" off the run params object, filters on
// HasSignal, normalises.
func TrackingSourceFromParams(params json.RawMessage) *models.ExternalTrackingSource {
	return trackingSourceFromField(params, "external_tracking")
}

// TrackingSourceFromTrainingConfig mirrors fn tracking_source_from_
// training_config — same as above but reads the "external_training"
// key.
func TrackingSourceFromTrainingConfig(trainingConfig json.RawMessage) *models.ExternalTrackingSource {
	return trackingSourceFromField(trainingConfig, "external_training")
}

// TrackingSourceFromSchema mirrors fn tracking_source_from_schema.
func TrackingSourceFromSchema(schema json.RawMessage) *models.ExternalTrackingSource {
	return trackingSourceFromField(schema, "external_tracking")
}

func trackingSourceFromField(raw json.RawMessage, field string) *models.ExternalTrackingSource {
	if len(raw) == 0 {
		return nil
	}
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(raw, &holder); err != nil {
		return nil
	}
	sub, ok := holder[field]
	if !ok || len(sub) == 0 {
		return nil
	}
	var src models.ExternalTrackingSource
	if err := json.Unmarshal(sub, &src); err != nil {
		return nil
	}
	if !src.HasSignal() {
		return nil
	}
	normalised := NormalizeTrackingSource(src)
	return &normalised
}

// ModelAdapterFromSchema mirrors fn model_adapter_from_schema.
func ModelAdapterFromSchema(schema json.RawMessage) *models.ModelAdapterDescriptor {
	if len(schema) == 0 {
		return nil
	}
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(schema, &holder); err != nil {
		return nil
	}
	sub, ok := holder["model_adapter"]
	if !ok || len(sub) == 0 {
		return nil
	}
	var adapter models.ModelAdapterDescriptor
	if err := json.Unmarshal(sub, &adapter); err != nil {
		return nil
	}
	if !adapter.HasSignal() {
		return nil
	}
	normalised := NormalizeModelAdapterDescriptor(adapter)
	return &normalised
}

// RegistrySourceFromSchema mirrors fn registry_source_from_schema.
func RegistrySourceFromSchema(schema json.RawMessage) *models.RegistrySourceDescriptor {
	if len(schema) == 0 {
		return nil
	}
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(schema, &holder); err != nil {
		return nil
	}
	sub, ok := holder["registry_source"]
	if !ok || len(sub) == 0 {
		return nil
	}
	var registry models.RegistrySourceDescriptor
	if err := json.Unmarshal(sub, &registry); err != nil {
		return nil
	}
	if !registry.HasSignal() {
		return nil
	}
	normalised := NormalizeRegistrySource(registry)
	return &normalised
}

// MergeTrainingConfigWithExternal mirrors fn merge_training_config_
// with_external — folds the external tracking source into the
// training config, sets default engine / framework / artifact_uri
// when missing, and inserts inferred model_adapter + registry_source.
func MergeTrainingConfigWithExternal(trainingConfig json.RawMessage, external *models.ExternalTrackingSource) json.RawMessage {
	object := asObject(trainingConfig)

	// Existing tracking from the training config (if any) feeds the
	// fall-through.
	var existingTracking *models.ExternalTrackingSource
	if rawExisting, ok := object["external_training"]; ok {
		var src models.ExternalTrackingSource
		if rawObj, _ := json.Marshal(rawExisting); rawObj != nil {
			if err := json.Unmarshal(rawObj, &src); err == nil {
				existingTracking = &src
			}
		}
	}

	var mergedTracking *models.ExternalTrackingSource
	source := external
	if source == nil {
		source = existingTracking
	}
	if source != nil && source.HasSignal() {
		n := NormalizeTrackingSource(*source)
		mergedTracking = &n
	}

	if mergedTracking != nil {
		if _, has := object["engine"]; !has {
			engine := "external-import"
			if mergedTracking.Framework != "" {
				engine = mergedTracking.Framework
			}
			object["engine"] = engine
		}
		if _, has := object["framework"]; !has && mergedTracking.Framework != "" {
			object["framework"] = mergedTracking.Framework
		}
		if _, has := object["artifact_uri"]; !has {
			if uri := PreferredArtifactURI(mergedTracking, nil); uri != "" {
				object["artifact_uri"] = uri
			}
		}
		object["external_training"] = mergedTracking
	}

	// adapter inference walks the object back as JSON
	configJSON, _ := json.Marshal(object)
	adapter := InferModelAdapter(configJSON, mergedTracking)
	if adapter.HasSignal() {
		object["model_adapter"] = adapter
	}
	if registry := InferRegistrySource(mergedTracking, nil); registry != nil {
		object["registry_source"] = *registry
	}

	out, _ := json.Marshal(object)
	return out
}

// MergeRunParams mirrors fn merge_run_params. Folds external
// tracking params into the run params (without overwriting existing
// keys), tags the result with framework + tracking_system + the full
// external_tracking object.
func MergeRunParams(params json.RawMessage, external *models.ExternalTrackingSource) json.RawMessage {
	object := asObject(params)

	if external != nil && external.HasSignal() {
		normalised := NormalizeTrackingSource(*external)

		// Merge normalized.params into object without overwriting.
		if len(normalised.Params) > 0 {
			var paramsObj map[string]any
			if err := json.Unmarshal(normalised.Params, &paramsObj); err == nil {
				for k, v := range paramsObj {
					if _, has := object[k]; !has {
						object[k] = v
					}
				}
			}
		}
		object["external_tracking"] = normalised
		if normalised.Framework != "" {
			if _, has := object["framework"]; !has {
				object["framework"] = normalised.Framework
			}
		}
		if normalised.System != "" {
			if _, has := object["tracking_system"]; !has {
				object["tracking_system"] = normalised.System
			}
		}
	}

	out, _ := json.Marshal(object)
	return out
}

// MergeRunArtifacts mirrors fn merge_run_artifacts. Dedupes by URI,
// then appends external artifacts + model/artifact/run URI synthetic
// references when present.
func MergeRunArtifacts(artifacts []models.ArtifactReference, external *models.ExternalTrackingSource) []models.ArtifactReference {
	merged := dedupeArtifacts(artifacts, nil)
	if external == nil || !external.HasSignal() {
		return merged
	}
	normalised := NormalizeTrackingSource(*external)

	for _, a := range normalised.Artifacts {
		merged = maybePushArtifact(merged, a)
	}
	if normalised.ModelURI != "" {
		merged = maybePushArtifact(merged, artifactReference("External Model", normalised.ModelURI, "model_uri"))
	}
	if normalised.ArtifactURI != "" {
		merged = maybePushArtifact(merged, artifactReference("Artifact Bundle", normalised.ArtifactURI, "artifact_bundle"))
	}
	if normalised.RunURI != "" {
		merged = maybePushArtifact(merged, artifactReference("Tracking Run", normalised.RunURI, "tracking_run"))
	}
	return merged
}

// MergeMetrics mirrors fn merge_metrics — dedupe by name, primary
// list takes precedence.
func MergeMetrics(primary, external []models.MetricValue) []models.MetricValue {
	return dedupeMetrics(primary, external)
}

// EffectiveFramework mirrors fn effective_framework — walks the
// candidate ordering (training_config.framework → model_adapter.
// framework → engine → external.framework) and returns the first
// non-empty normalised value, falling back to "tabular-logistic".
func EffectiveFramework(trainingConfig json.RawMessage) string {
	external := TrackingSourceFromTrainingConfig(trainingConfig)
	candidates := []string{}

	var configObj map[string]any
	if len(trainingConfig) > 0 {
		_ = json.Unmarshal(trainingConfig, &configObj)
	}
	if configObj != nil {
		if v, ok := configObj["framework"].(string); ok {
			candidates = append(candidates, v)
		}
		if adapterRaw, ok := configObj["model_adapter"].(map[string]any); ok {
			if v, ok := adapterRaw["framework"].(string); ok {
				candidates = append(candidates, v)
			}
		}
		if v, ok := configObj["engine"].(string); ok {
			candidates = append(candidates, v)
		}
	}
	if external != nil {
		candidates = append(candidates, external.Framework)
	}

	for _, c := range candidates {
		if normalised := NormalizeFrameworkName(c); normalised != "" {
			return normalised
		}
	}
	return "tabular-logistic"
}

// InferModelAdapter mirrors fn infer_model_adapter — walks
// training_config.model_adapter as the requested overrides, falls
// through framework / flavor / kind / runtime / loader / artifact_uri
// from external tracking + the framework-specific defaults.
func InferModelAdapter(trainingConfig json.RawMessage, external *models.ExternalTrackingSource) models.ModelAdapterDescriptor {
	var requested models.ModelAdapterDescriptor
	if len(trainingConfig) > 0 {
		var configObj map[string]json.RawMessage
		if err := json.Unmarshal(trainingConfig, &configObj); err == nil {
			if rawAdapter, ok := configObj["model_adapter"]; ok {
				_ = json.Unmarshal(rawAdapter, &requested)
			}
		}
	}

	framework := ""
	switch {
	case strings.TrimSpace(requested.Framework) != "":
		framework = NormalizeFrameworkName(requested.Framework)
	case external != nil:
		framework = NormalizeFrameworkName(external.Framework)
	case len(trainingConfig) > 0:
		framework = EffectiveFramework(trainingConfig)
	}

	flavor := ""
	switch {
	case strings.TrimSpace(requested.Flavor) != "":
		flavor = normalizeFlavorName(requested.Flavor)
	case external != nil:
		flavor = normalizeFlavorName(external.Flavor)
	case framework == "scikit-learn":
		flavor = "joblib"
	case framework == "pytorch":
		flavor = "torchscript"
	case framework == "tensorflow":
		flavor = "savedmodel"
	case framework == "onnx":
		flavor = "onnx"
	}

	kind := ""
	switch {
	case strings.TrimSpace(requested.Kind) != "":
		kind = strings.TrimSpace(requested.Kind)
	case external != nil:
		kind = "external"
	default:
		kind = "native"
	}

	runtime := strings.TrimSpace(requested.Runtime)
	if runtime == "" {
		runtime = runtimeForAdapter(framework, flavor, kind)
	}

	loader := strings.TrimSpace(requested.Loader)
	if loader == "" {
		loader = loaderForAdapter(framework, flavor, external)
	}

	artifactURI := strings.TrimSpace(requested.ArtifactURI)
	if artifactURI == "" {
		artifactURI = PreferredArtifactURI(external, trainingConfig)
	}

	return models.ModelAdapterDescriptor{
		Kind:            kind,
		Framework:       framework,
		Flavor:          flavor,
		Runtime:         runtime,
		Loader:          loader,
		ArtifactURI:     artifactURI,
		Entrypoint:      strings.TrimSpace(requested.Entrypoint),
		RequirementsURI: strings.TrimSpace(requested.RequirementsURI),
		Metadata:        normalizeObjectOrNull(requested.Metadata),
	}
}

// InferRegistrySource mirrors fn infer_registry_source(external,
// existing). Merges the requested (existing) registry over the
// inferred one (from external tracking), returning nil if the
// result has no signal.
func InferRegistrySource(tracking *models.ExternalTrackingSource, existing *models.RegistrySourceDescriptor) *models.RegistrySourceDescriptor {
	var requested models.RegistrySourceDescriptor
	if existing != nil {
		requested = *existing
	}
	var inferred models.ExternalTrackingSource
	if tracking != nil {
		inferred = *tracking
	}

	system := requested.System
	if strings.TrimSpace(system) != "" {
		system = normalizeTrackingSystem(system)
	} else {
		system = normalizeTrackingSystem(inferred.System)
	}

	modelName := strings.TrimSpace(requested.ModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(inferred.RegisteredModelName)
	}

	modelVersion := strings.TrimSpace(requested.ModelVersion)
	if modelVersion == "" {
		modelVersion = strings.TrimSpace(inferred.RegisteredModelVersion)
	}

	stage := strings.TrimSpace(requested.Stage)
	if stage == "" {
		stage = strings.TrimSpace(inferred.Stage)
	}

	uri := strings.TrimSpace(requested.URI)
	if uri == "" {
		switch {
		case strings.TrimSpace(inferred.ModelURI) != "":
			uri = strings.TrimSpace(inferred.ModelURI)
		case strings.TrimSpace(inferred.ArtifactURI) != "":
			uri = strings.TrimSpace(inferred.ArtifactURI)
		}
	}

	registry := models.RegistrySourceDescriptor{
		System:       system,
		ModelName:    modelName,
		ModelVersion: modelVersion,
		Stage:        stage,
		URI:          uri,
		Metadata:     normalizeObjectOrNull(requested.Metadata),
	}
	if !registry.HasSignal() {
		return nil
	}
	return &registry
}

// NormalizeModelVersionSchema mirrors fn normalize_model_version_
// schema verbatim. Folds artifact_uri / model_adapter / registry_
// source / external_tracking into the schema object, fills in
// engine / signature defaults.
func NormalizeModelVersionSchema(
	schema json.RawMessage,
	artifactURI string,
	trainingConfig json.RawMessage,
	modelAdapter *models.ModelAdapterDescriptor,
	registrySource *models.RegistrySourceDescriptor,
	externalTracking *models.ExternalTrackingSource,
) json.RawMessage {
	object := asObject(schema)

	// Pull external_tracking off the schema if not provided.
	var schemaTracking *models.ExternalTrackingSource
	if rawTracking, ok := object["external_tracking"]; ok {
		var src models.ExternalTrackingSource
		if b, _ := json.Marshal(rawTracking); b != nil {
			if err := json.Unmarshal(b, &src); err == nil {
				schemaTracking = &src
			}
		}
	}

	source := externalTracking
	if source == nil {
		source = schemaTracking
	}
	var mergedTracking *models.ExternalTrackingSource
	if source != nil && source.HasSignal() {
		n := NormalizeTrackingSource(*source)
		mergedTracking = &n
	}

	effectiveArtifactURI := strings.TrimSpace(artifactURI)
	if effectiveArtifactURI == "" {
		if v, ok := object["artifact_uri"].(string); ok {
			effectiveArtifactURI = v
		}
	}
	if effectiveArtifactURI == "" {
		effectiveArtifactURI = PreferredArtifactURI(mergedTracking, trainingConfig)
	}

	var existingAdapter *models.ModelAdapterDescriptor
	if rawAdapter, ok := object["model_adapter"]; ok {
		var ad models.ModelAdapterDescriptor
		if b, _ := json.Marshal(rawAdapter); b != nil {
			if err := json.Unmarshal(b, &ad); err == nil {
				existingAdapter = &ad
			}
		}
	}

	var effectiveAdapter models.ModelAdapterDescriptor
	candidate := modelAdapter
	if candidate == nil {
		candidate = existingAdapter
	}
	if candidate != nil && candidate.HasSignal() {
		effectiveAdapter = NormalizeModelAdapterDescriptor(*candidate)
	} else {
		effectiveAdapter = InferModelAdapter(trainingConfig, mergedTracking)
	}

	var existingRegistry *models.RegistrySourceDescriptor
	if rawRegistry, ok := object["registry_source"]; ok {
		var reg models.RegistrySourceDescriptor
		if b, _ := json.Marshal(rawRegistry); b != nil {
			if err := json.Unmarshal(b, &reg); err == nil {
				existingRegistry = &reg
			}
		}
	}
	var registryHint *models.RegistrySourceDescriptor
	if registrySource != nil {
		registryHint = registrySource
	} else {
		registryHint = existingRegistry
	}
	effectiveRegistry := InferRegistrySource(mergedTracking, registryHint)

	if len(trainingConfig) > 0 {
		engine := EffectiveFramework(trainingConfig)
		if engine != "" {
			if _, has := object["engine"]; !has {
				object["engine"] = engine
			}
		}
	}
	if effectiveArtifactURI != "" {
		object["artifact_uri"] = effectiveArtifactURI
	}

	if effectiveAdapter.HasSignal() {
		adapter := effectiveAdapter
		if adapter.ArtifactURI == "" {
			if v, ok := object["artifact_uri"].(string); ok {
				adapter.ArtifactURI = v
			}
		}
		object["model_adapter"] = adapter
	}
	if effectiveRegistry != nil {
		object["registry_source"] = *effectiveRegistry
	}
	if mergedTracking != nil {
		object["external_tracking"] = *mergedTracking
	}
	if _, has := object["signature"]; !has {
		signature := "external-model"
		if _, hasState := object["model_state"]; hasState {
			signature = "tabular"
		}
		object["signature"] = signature
	}

	out, _ := json.Marshal(object)
	return out
}

// PreferredArtifactURI mirrors fn preferred_artifact_uri. Returns
// the first non-empty trimmed URI from external.{model_uri,
// artifact_uri, run_uri} or training_config.artifact_uri. Returns
// empty string when nothing matches.
func PreferredArtifactURI(external *models.ExternalTrackingSource, trainingConfig json.RawMessage) string {
	if external != nil {
		for _, candidate := range []string{external.ModelURI, external.ArtifactURI, external.RunURI} {
			if v := strings.TrimSpace(candidate); v != "" {
				return v
			}
		}
	}
	if len(trainingConfig) > 0 {
		var obj map[string]any
		if err := json.Unmarshal(trainingConfig, &obj); err == nil {
			if v, ok := obj["artifact_uri"].(string); ok {
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

// NormalizeModelAdapterDescriptor mirrors fn normalize_model_adapter_
// descriptor.
func NormalizeModelAdapterDescriptor(d models.ModelAdapterDescriptor) models.ModelAdapterDescriptor {
	return models.ModelAdapterDescriptor{
		Kind:            strings.TrimSpace(d.Kind),
		Framework:       NormalizeFrameworkName(d.Framework),
		Flavor:          normalizeFlavorName(d.Flavor),
		Runtime:         strings.TrimSpace(d.Runtime),
		Loader:          strings.TrimSpace(d.Loader),
		ArtifactURI:     strings.TrimSpace(d.ArtifactURI),
		Entrypoint:      strings.TrimSpace(d.Entrypoint),
		RequirementsURI: strings.TrimSpace(d.RequirementsURI),
		Metadata:        normalizeObjectOrNull(d.Metadata),
	}
}

// NormalizeRegistrySource mirrors fn normalize_registry_source.
func NormalizeRegistrySource(r models.RegistrySourceDescriptor) models.RegistrySourceDescriptor {
	return models.RegistrySourceDescriptor{
		System:       normalizeTrackingSystem(r.System),
		ModelName:    strings.TrimSpace(r.ModelName),
		ModelVersion: strings.TrimSpace(r.ModelVersion),
		Stage:        strings.TrimSpace(r.Stage),
		URI:          strings.TrimSpace(r.URI),
		Metadata:     normalizeObjectOrNull(r.Metadata),
	}
}

func normalizeTrackingSystem(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "weightsandbiases", "weights & biases", "wandb":
		return "wandb"
	case "mlflow", "databricks-mlflow":
		return "mlflow"
	case "sagemaker", "amazon-sagemaker":
		return "sagemaker"
	case "azureml", "azure-ml":
		return "azureml"
	case "vertexai", "vertex-ai":
		return "vertexai"
	case "comet", "cometml":
		return "comet"
	case "neptune", "neptuneai":
		return "neptune"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// NormalizeFrameworkName mirrors pub fn normalize_framework_name.
func NormalizeFrameworkName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "sklearn", "scikit", "scikit_learn", "scikit-learn":
		return "scikit-learn"
	case "torch", "pytorch":
		return "pytorch"
	case "tf", "keras", "tensorflow":
		return "tensorflow"
	case "xgb", "xgboost":
		return "xgboost"
	case "lightgbm", "lgbm":
		return "lightgbm"
	case "catboost":
		return "catboost"
	case "onnx":
		return "onnx"
	case "huggingface", "transformers":
		return "huggingface"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func normalizeFlavorName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "pyfunc", "mlflow.pyfunc":
		return "pyfunc"
	case "sklearn", "joblib":
		return "joblib"
	case "torchscript", "torch-script":
		return "torchscript"
	case "savedmodel", "saved_model", "tensorflow.savedmodel":
		return "savedmodel"
	case "onnx":
		return "onnx"
	case "pickle", "cloudpickle":
		return "pickle"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func runtimeForAdapter(framework, flavor, kind string) string {
	if kind == "native" {
		return "in-process"
	}
	switch {
	case framework == "onnx" || flavor == "onnx":
		return "onnxruntime"
	case framework == "pytorch" || flavor == "torchscript":
		return "torchserve-compatible"
	case framework == "tensorflow" || flavor == "savedmodel":
		return "tensorflow-serving-compatible"
	case framework == "huggingface":
		return "transformers-runtime"
	case framework == "":
		return "external-serving"
	default:
		return "python-remote"
	}
}

func loaderForAdapter(framework, flavor string, external *models.ExternalTrackingSource) string {
	if external != nil && external.System == "mlflow" {
		return "mlflow"
	}
	switch {
	case framework == "onnx" || flavor == "onnx":
		return "onnx"
	case framework == "pytorch" || flavor == "torchscript":
		return "torch"
	case framework == "tensorflow" || flavor == "savedmodel":
		return "tensorflow"
	case framework == "scikit-learn" && (flavor == "joblib" || flavor == "pickle"):
		return "joblib"
	case framework == "xgboost":
		return "xgboost"
	case framework == "lightgbm":
		return "lightgbm"
	default:
		return "artifact-reference"
	}
}

// normalizeObjectOrNull mirrors fn normalize_object_or_null. Object
// passes through; null stays null; everything else is wrapped in
// {"value": <raw>}. Empty raw messages are treated as null.
func normalizeObjectOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("null")
	}
	if strings.HasPrefix(trimmed, "{") {
		return raw
	}
	wrapped, _ := json.Marshal(map[string]json.RawMessage{"value": raw})
	return wrapped
}

func dedupeMetrics(primary, secondary []models.MetricValue) []models.MetricValue {
	seen := map[string]struct{}{}
	merged := make([]models.MetricValue, 0, len(primary)+len(secondary))
	for _, m := range primary {
		if _, has := seen[m.Name]; has {
			continue
		}
		seen[m.Name] = struct{}{}
		merged = append(merged, m)
	}
	for _, m := range secondary {
		if _, has := seen[m.Name]; has {
			continue
		}
		seen[m.Name] = struct{}{}
		merged = append(merged, m)
	}
	return merged
}

func dedupeArtifacts(primary, secondary []models.ArtifactReference) []models.ArtifactReference {
	seen := map[string]struct{}{}
	merged := make([]models.ArtifactReference, 0, len(primary)+len(secondary))
	for _, a := range primary {
		if _, has := seen[a.URI]; has {
			continue
		}
		seen[a.URI] = struct{}{}
		merged = append(merged, a)
	}
	for _, a := range secondary {
		if _, has := seen[a.URI]; has {
			continue
		}
		seen[a.URI] = struct{}{}
		merged = append(merged, a)
	}
	return merged
}

func artifactReference(name, uri, artifactType string) models.ArtifactReference {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return models.ArtifactReference{
		ID:           id,
		Name:         name,
		URI:          uri,
		ArtifactType: artifactType,
		SizeBytes:    0,
	}
}

func maybePushArtifact(artifacts []models.ArtifactReference, candidate models.ArtifactReference) []models.ArtifactReference {
	for _, a := range artifacts {
		if a.URI == candidate.URI {
			return artifacts
		}
	}
	return append(artifacts, candidate)
}

// asObject mirrors fn as_object. Object passes through; null becomes
// empty map; anything else is wrapped under "raw".
func asObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}
	}
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err == nil {
			return obj
		}
	}
	var other any
	_ = json.Unmarshal(raw, &other)
	return map[string]any{"raw": other}
}

// SortedKeys returns an alphabetically-sorted copy of the keys in
// the given object — useful when callers want deterministic
// iteration. Not used internally (encoding/json already sorts), but
// exposed for tests.
func SortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
