use std::collections::BTreeSet;

use serde::de::DeserializeOwned;
use serde_json::{Map, Value, json};
use uuid::Uuid;

use crate::{
    handlers::to_json,
    models::{
        interop::{ExternalTrackingSource, ModelAdapterDescriptor, RegistrySourceDescriptor},
        run::{ArtifactReference, MetricValue},
    },
};

pub fn normalize_tracking_source(source: ExternalTrackingSource) -> ExternalTrackingSource {
    ExternalTrackingSource {
        system: normalize_tracking_system(&source.system),
        project: source.project.trim().to_string(),
        experiment_name: source.experiment_name.trim().to_string(),
        run_id: source.run_id.trim().to_string(),
        run_name: source.run_name.trim().to_string(),
        run_uri: source.run_uri.trim().to_string(),
        artifact_uri: source.artifact_uri.trim().to_string(),
        model_uri: source.model_uri.trim().to_string(),
        registered_model_name: source.registered_model_name.trim().to_string(),
        registered_model_version: source.registered_model_version.trim().to_string(),
        framework: normalize_framework_name(&source.framework),
        flavor: normalize_flavor_name(&source.flavor),
        stage: source.stage.trim().to_string(),
        tags: normalize_object_or_null(source.tags),
        params: normalize_object_or_null(source.params),
        metrics: dedupe_metrics(&source.metrics, &[]),
        artifacts: dedupe_artifacts(&source.artifacts, &[]),
        metadata: normalize_object_or_null(source.metadata),
    }
}

pub fn tracking_source_from_params(params: &Value) -> Option<ExternalTrackingSource> {
    params
        .get("external_tracking")
        .cloned()
        .and_then(parse_json_value::<ExternalTrackingSource>)
        .filter(|source| source.has_signal())
        .map(normalize_tracking_source)
}

pub fn tracking_source_from_training_config(
    training_config: &Value,
) -> Option<ExternalTrackingSource> {
    training_config
        .get("external_training")
        .cloned()
        .and_then(parse_json_value::<ExternalTrackingSource>)
        .filter(|source| source.has_signal())
        .map(normalize_tracking_source)
}

pub fn tracking_source_from_schema(schema: &Value) -> Option<ExternalTrackingSource> {
    schema
        .get("external_tracking")
        .cloned()
        .and_then(parse_json_value::<ExternalTrackingSource>)
        .filter(|source| source.has_signal())
        .map(normalize_tracking_source)
}

pub fn model_adapter_from_schema(schema: &Value) -> Option<ModelAdapterDescriptor> {
    schema
        .get("model_adapter")
        .cloned()
        .and_then(parse_json_value::<ModelAdapterDescriptor>)
        .filter(|adapter| adapter.has_signal())
        .map(normalize_model_adapter_descriptor)
}

pub fn registry_source_from_schema(schema: &Value) -> Option<RegistrySourceDescriptor> {
    schema
        .get("registry_source")
        .cloned()
        .and_then(parse_json_value::<RegistrySourceDescriptor>)
        .filter(|registry| registry.has_signal())
        .map(normalize_registry_source)
}

pub fn merge_training_config_with_external(
    training_config: Value,
    external: Option<&ExternalTrackingSource>,
) -> Value {
    let mut object = as_object(training_config);

    let existing_tracking = object
        .get("external_training")
        .cloned()
        .and_then(parse_json_value::<ExternalTrackingSource>);
    let merged_tracking = external
        .cloned()
        .or(existing_tracking)
        .filter(|tracking| tracking.has_signal())
        .map(normalize_tracking_source);

    if let Some(tracking) = merged_tracking.as_ref() {
        if !object.contains_key("engine") {
            object.insert(
                "engine".to_string(),
                Value::String(if tracking.framework.is_empty() {
                    "external-import".to_string()
                } else {
                    tracking.framework.clone()
                }),
            );
        }
        if !object.contains_key("framework") && !tracking.framework.is_empty() {
            object.insert(
                "framework".to_string(),
                Value::String(tracking.framework.clone()),
            );
        }
        if !object.contains_key("artifact_uri") {
            if let Some(uri) = preferred_artifact_uri(Some(tracking), None) {
                object.insert("artifact_uri".to_string(), Value::String(uri));
            }
        }
        object.insert("external_training".to_string(), to_json(tracking));
    }

    let adapter = infer_model_adapter(
        Some(&Value::Object(object.clone())),
        merged_tracking.as_ref(),
    );
    if adapter.has_signal() {
        object.insert("model_adapter".to_string(), to_json(&adapter));
    }

    if let Some(registry) = infer_registry_source(merged_tracking.as_ref(), None) {
        object.insert("registry_source".to_string(), to_json(&registry));
    }

    Value::Object(object)
}

pub fn merge_run_params(params: Value, external: Option<&ExternalTrackingSource>) -> Value {
    let mut object = as_object(params);

    if let Some(tracking) = external.filter(|tracking| tracking.has_signal()) {
        let normalized = normalize_tracking_source(tracking.clone());
        merge_object_if_absent(&mut object, normalized.params.clone());
        object.insert("external_tracking".to_string(), to_json(&normalized));
        if !normalized.framework.is_empty() && !object.contains_key("framework") {
            object.insert(
                "framework".to_string(),
                Value::String(normalized.framework.clone()),
            );
        }
        if !normalized.system.is_empty() && !object.contains_key("tracking_system") {
            object.insert(
                "tracking_system".to_string(),
                Value::String(normalized.system.clone()),
            );
        }
    }

    Value::Object(object)
}

pub fn merge_run_artifacts(
    artifacts: Vec<ArtifactReference>,
    external: Option<&ExternalTrackingSource>,
) -> Vec<ArtifactReference> {
    let mut merged = dedupe_artifacts(&artifacts, &[]);
    let Some(tracking) = external.filter(|tracking| tracking.has_signal()) else {
        return merged;
    };
    let normalized = normalize_tracking_source(tracking.clone());

    for artifact in normalized.artifacts {
        maybe_push_artifact(&mut merged, artifact);
    }
    if !normalized.model_uri.is_empty() {
        maybe_push_artifact(
            &mut merged,
            artifact_reference("External Model", &normalized.model_uri, "model_uri"),
        );
    }
    if !normalized.artifact_uri.is_empty() {
        maybe_push_artifact(
            &mut merged,
            artifact_reference(
                "Artifact Bundle",
                &normalized.artifact_uri,
                "artifact_bundle",
            ),
        );
    }
    if !normalized.run_uri.is_empty() {
        maybe_push_artifact(
            &mut merged,
            artifact_reference("Tracking Run", &normalized.run_uri, "tracking_run"),
        );
    }

    merged
}

pub fn merge_metrics(primary: &[MetricValue], external: &[MetricValue]) -> Vec<MetricValue> {
    dedupe_metrics(primary, external)
}

pub fn effective_framework(training_config: &Value) -> String {
    let external_tracking = tracking_source_from_training_config(training_config);
    let candidates = [
        training_config.get("framework").and_then(Value::as_str),
        training_config
            .get("model_adapter")
            .and_then(|value| value.get("framework"))
            .and_then(Value::as_str),
        training_config.get("engine").and_then(Value::as_str),
        external_tracking
            .as_ref()
            .map(|source| source.framework.as_str()),
    ];

    for candidate in candidates.into_iter().flatten() {
        let normalized = normalize_framework_name(candidate);
        if !normalized.is_empty() {
            return normalized;
        }
    }

    "tabular-logistic".to_string()
}

pub fn infer_model_adapter(
    training_config: Option<&Value>,
    external_tracking: Option<&ExternalTrackingSource>,
) -> ModelAdapterDescriptor {
    let requested = training_config
        .and_then(|config| config.get("model_adapter").cloned())
        .and_then(parse_json_value::<ModelAdapterDescriptor>)
        .unwrap_or_default();

    let framework = if !requested.framework.trim().is_empty() {
        normalize_framework_name(&requested.framework)
    } else if let Some(external) = external_tracking {
        normalize_framework_name(&external.framework)
    } else if let Some(config) = training_config {
        effective_framework(config)
    } else {
        String::new()
    };
    let flavor = if !requested.flavor.trim().is_empty() {
        normalize_flavor_name(&requested.flavor)
    } else if let Some(external) = external_tracking {
        normalize_flavor_name(&external.flavor)
    } else if framework == "scikit-learn" {
        "joblib".to_string()
    } else if framework == "pytorch" {
        "torchscript".to_string()
    } else if framework == "tensorflow" {
        "savedmodel".to_string()
    } else if framework == "onnx" {
        "onnx".to_string()
    } else {
        String::new()
    };
    let kind = if !requested.kind.trim().is_empty() {
        requested.kind.trim().to_string()
    } else if external_tracking.is_some() {
        "external".to_string()
    } else {
        "native".to_string()
    };
    let runtime = if !requested.runtime.trim().is_empty() {
        requested.runtime.trim().to_string()
    } else {
        runtime_for_adapter(&framework, &flavor, &kind)
    };
    let loader = if !requested.loader.trim().is_empty() {
        requested.loader.trim().to_string()
    } else {
        loader_for_adapter(&framework, &flavor, external_tracking)
    };
    let artifact_uri = if !requested.artifact_uri.trim().is_empty() {
        requested.artifact_uri.trim().to_string()
    } else {
        preferred_artifact_uri(external_tracking, training_config).unwrap_or_default()
    };

    ModelAdapterDescriptor {
        kind,
        framework,
        flavor,
        runtime,
        loader,
        artifact_uri,
        entrypoint: requested.entrypoint.trim().to_string(),
        requirements_uri: requested.requirements_uri.trim().to_string(),
        metadata: normalize_object_or_null(requested.metadata),
    }
}

pub fn infer_registry_source(
    external_tracking: Option<&ExternalTrackingSource>,
    existing: Option<&RegistrySourceDescriptor>,
) -> Option<RegistrySourceDescriptor> {
    let requested = existing.cloned().unwrap_or_default();
    let inferred = external_tracking.cloned().unwrap_or_default();
    let registry = RegistrySourceDescriptor {
        system: if !requested.system.trim().is_empty() {
            normalize_tracking_system(&requested.system)
        } else {
            normalize_tracking_system(&inferred.system)
        },
        model_name: if !requested.model_name.trim().is_empty() {
            requested.model_name.trim().to_string()
        } else {
            inferred.registered_model_name.trim().to_string()
        },
        model_version: if !requested.model_version.trim().is_empty() {
            requested.model_version.trim().to_string()
        } else {
            inferred.registered_model_version.trim().to_string()
        },
        stage: if !requested.stage.trim().is_empty() {
            requested.stage.trim().to_string()
        } else {
            inferred.stage.trim().to_string()
        },
        uri: if !requested.uri.trim().is_empty() {
            requested.uri.trim().to_string()
        } else if !inferred.model_uri.trim().is_empty() {
            inferred.model_uri.trim().to_string()
        } else if !inferred.artifact_uri.trim().is_empty() {
            inferred.artifact_uri.trim().to_string()
        } else {
            String::new()
        },
        metadata: normalize_object_or_null(requested.metadata),
    };

    registry.has_signal().then_some(registry)
}

pub fn normalize_model_version_schema(
    schema: Option<Value>,
    artifact_uri: Option<&str>,
    training_config: Option<&Value>,
    model_adapter: Option<&ModelAdapterDescriptor>,
    registry_source: Option<&RegistrySourceDescriptor>,
    external_tracking: Option<&ExternalTrackingSource>,
) -> Value {
    let mut object = as_object(schema.unwrap_or_else(|| json!({})));

    let schema_tracking = object
        .get("external_tracking")
        .cloned()
        .and_then(parse_json_value::<ExternalTrackingSource>);
    let merged_tracking = external_tracking
        .cloned()
        .or(schema_tracking)
        .filter(|tracking| tracking.has_signal())
        .map(normalize_tracking_source);

    let effective_artifact_uri = artifact_uri
        .filter(|value| !value.trim().is_empty())
        .map(|value| value.trim().to_string())
        .or_else(|| {
            object
                .get("artifact_uri")
                .and_then(Value::as_str)
                .map(|value| value.to_string())
        })
        .or_else(|| preferred_artifact_uri(merged_tracking.as_ref(), training_config));

    let existing_adapter = object
        .get("model_adapter")
        .cloned()
        .and_then(parse_json_value::<ModelAdapterDescriptor>);
    let effective_adapter = model_adapter
        .cloned()
        .or(existing_adapter)
        .filter(|adapter| adapter.has_signal())
        .map(normalize_model_adapter_descriptor)
        .unwrap_or_else(|| infer_model_adapter(training_config, merged_tracking.as_ref()));

    let existing_registry = object
        .get("registry_source")
        .cloned()
        .and_then(parse_json_value::<RegistrySourceDescriptor>);
    let effective_registry = infer_registry_source(
        merged_tracking.as_ref(),
        registry_source.or(existing_registry.as_ref()),
    );

    if let Some(engine) = training_config
        .map(effective_framework)
        .filter(|framework| !framework.is_empty())
    {
        object
            .entry("engine".to_string())
            .or_insert_with(|| Value::String(engine));
    }
    if let Some(uri) = effective_artifact_uri {
        object.insert("artifact_uri".to_string(), Value::String(uri));
    }

    if effective_adapter.has_signal() {
        let mut adapter = effective_adapter.clone();
        if adapter.artifact_uri.is_empty() {
            adapter.artifact_uri = object
                .get("artifact_uri")
                .and_then(Value::as_str)
                .unwrap_or_default()
                .to_string();
        }
        object.insert("model_adapter".to_string(), to_json(&adapter));
    }
    if let Some(registry) = effective_registry {
        object.insert("registry_source".to_string(), to_json(&registry));
    }
    if let Some(tracking) = merged_tracking {
        object.insert("external_tracking".to_string(), to_json(&tracking));
    }
    if !object.contains_key("signature") {
        object.insert(
            "signature".to_string(),
            Value::String(if object.contains_key("model_state") {
                "tabular".to_string()
            } else {
                "external-model".to_string()
            }),
        );
    }

    Value::Object(object)
}

pub fn preferred_artifact_uri(
    external_tracking: Option<&ExternalTrackingSource>,
    training_config: Option<&Value>,
) -> Option<String> {
    external_tracking
        .and_then(|tracking| {
            [
                tracking.model_uri.as_str(),
                tracking.artifact_uri.as_str(),
                tracking.run_uri.as_str(),
            ]
            .into_iter()
            .find(|value| !value.trim().is_empty())
            .map(|value| value.to_string())
        })
        .or_else(|| {
            training_config
                .and_then(|config| config.get("artifact_uri").and_then(Value::as_str))
                .filter(|value| !value.trim().is_empty())
                .map(|value| value.to_string())
        })
}

pub fn normalize_model_adapter_descriptor(
    adapter: ModelAdapterDescriptor,
) -> ModelAdapterDescriptor {
    ModelAdapterDescriptor {
        kind: adapter.kind.trim().to_string(),
        framework: normalize_framework_name(&adapter.framework),
        flavor: normalize_flavor_name(&adapter.flavor),
        runtime: adapter.runtime.trim().to_string(),
        loader: adapter.loader.trim().to_string(),
        artifact_uri: adapter.artifact_uri.trim().to_string(),
        entrypoint: adapter.entrypoint.trim().to_string(),
        requirements_uri: adapter.requirements_uri.trim().to_string(),
        metadata: normalize_object_or_null(adapter.metadata),
    }
}

pub fn normalize_registry_source(registry: RegistrySourceDescriptor) -> RegistrySourceDescriptor {
    RegistrySourceDescriptor {
        system: normalize_tracking_system(&registry.system),
        model_name: registry.model_name.trim().to_string(),
        model_version: registry.model_version.trim().to_string(),
        stage: registry.stage.trim().to_string(),
        uri: registry.uri.trim().to_string(),
        metadata: normalize_object_or_null(registry.metadata),
    }
}

fn normalize_tracking_system(raw: &str) -> String {
    match raw.trim().to_ascii_lowercase().as_str() {
        "" => String::new(),
        "weightsandbiases" | "weights & biases" | "wandb" => "wandb".to_string(),
        "mlflow" | "databricks-mlflow" => "mlflow".to_string(),
        "sagemaker" | "amazon-sagemaker" => "sagemaker".to_string(),
        "azureml" | "azure-ml" => "azureml".to_string(),
        "vertexai" | "vertex-ai" => "vertexai".to_string(),
        "comet" | "cometml" => "comet".to_string(),
        "neptune" | "neptuneai" => "neptune".to_string(),
        other => other.to_string(),
    }
}

pub fn normalize_framework_name(raw: &str) -> String {
    match raw.trim().to_ascii_lowercase().as_str() {
        "" => String::new(),
        "sklearn" | "scikit" | "scikit_learn" | "scikit-learn" => "scikit-learn".to_string(),
        "torch" | "pytorch" => "pytorch".to_string(),
        "tf" | "keras" | "tensorflow" => "tensorflow".to_string(),
        "xgb" | "xgboost" => "xgboost".to_string(),
        "lightgbm" | "lgbm" => "lightgbm".to_string(),
        "catboost" => "catboost".to_string(),
        "onnx" => "onnx".to_string(),
        "huggingface" | "transformers" => "huggingface".to_string(),
        other => other.to_string(),
    }
}

fn normalize_flavor_name(raw: &str) -> String {
    match raw.trim().to_ascii_lowercase().as_str() {
        "" => String::new(),
        "pyfunc" | "mlflow.pyfunc" => "pyfunc".to_string(),
        "sklearn" | "joblib" => "joblib".to_string(),
        "torchscript" | "torch-script" => "torchscript".to_string(),
        "savedmodel" | "saved_model" | "tensorflow.savedmodel" => "savedmodel".to_string(),
        "onnx" => "onnx".to_string(),
        "pickle" | "cloudpickle" => "pickle".to_string(),
        other => other.to_string(),
    }
}

fn runtime_for_adapter(framework: &str, flavor: &str, kind: &str) -> String {
    if kind == "native" {
        return "in-process".to_string();
    }

    if framework == "onnx" || flavor == "onnx" {
        "onnxruntime".to_string()
    } else if framework == "pytorch" || flavor == "torchscript" {
        "torchserve-compatible".to_string()
    } else if framework == "tensorflow" || flavor == "savedmodel" {
        "tensorflow-serving-compatible".to_string()
    } else if framework == "huggingface" {
        "transformers-runtime".to_string()
    } else if framework.is_empty() {
        "external-serving".to_string()
    } else {
        "python-remote".to_string()
    }
}

fn loader_for_adapter(
    framework: &str,
    flavor: &str,
    external_tracking: Option<&ExternalTrackingSource>,
) -> String {
    if external_tracking
        .map(|tracking| tracking.system.as_str())
        .unwrap_or_default()
        == "mlflow"
    {
        return "mlflow".to_string();
    }
    if framework == "onnx" || flavor == "onnx" {
        "onnx".to_string()
    } else if framework == "pytorch" || flavor == "torchscript" {
        "torch".to_string()
    } else if framework == "tensorflow" || flavor == "savedmodel" {
        "tensorflow".to_string()
    } else if framework == "scikit-learn" && (flavor == "joblib" || flavor == "pickle") {
        "joblib".to_string()
    } else if framework == "xgboost" {
        "xgboost".to_string()
    } else if framework == "lightgbm" {
        "lightgbm".to_string()
    } else {
        "artifact-reference".to_string()
    }
}

fn normalize_object_or_null(value: Value) -> Value {
    match value {
        Value::Object(object) => Value::Object(object),
        Value::Null => Value::Null,
        other => json!({ "value": other }),
    }
}

fn dedupe_metrics(primary: &[MetricValue], secondary: &[MetricValue]) -> Vec<MetricValue> {
    let mut names = BTreeSet::new();
    let mut merged = Vec::new();
    for metric in primary.iter().chain(secondary.iter()) {
        if names.insert(metric.name.clone()) {
            merged.push(metric.clone());
        }
    }
    merged
}

fn dedupe_artifacts(
    primary: &[ArtifactReference],
    secondary: &[ArtifactReference],
) -> Vec<ArtifactReference> {
    let mut uris = BTreeSet::new();
    let mut merged = Vec::new();
    for artifact in primary.iter().chain(secondary.iter()) {
        if uris.insert(artifact.uri.clone()) {
            merged.push(artifact.clone());
        }
    }
    merged
}

fn artifact_reference(name: &str, uri: &str, artifact_type: &str) -> ArtifactReference {
    ArtifactReference {
        id: Uuid::now_v7(),
        name: name.to_string(),
        uri: uri.to_string(),
        artifact_type: artifact_type.to_string(),
        size_bytes: 0,
    }
}

fn maybe_push_artifact(artifacts: &mut Vec<ArtifactReference>, candidate: ArtifactReference) {
    if !artifacts
        .iter()
        .any(|artifact| artifact.uri == candidate.uri)
    {
        artifacts.push(candidate);
    }
}

fn merge_object_if_absent(target: &mut Map<String, Value>, source: Value) {
    let Value::Object(source) = source else {
        return;
    };
    for (key, value) in source {
        target.entry(key).or_insert(value);
    }
}

fn as_object(value: Value) -> Map<String, Value> {
    match value {
        Value::Object(object) => object,
        Value::Null => Map::new(),
        other => {
            let mut object = Map::new();
            object.insert("raw".to_string(), other);
            object
        }
    }
}

fn parse_json_value<T>(value: Value) -> Option<T>
where
    T: DeserializeOwned,
{
    serde_json::from_value(value).ok()
}

#[cfg(test)]
mod tests {
    use serde_json::{Value, json};
    use uuid::Uuid;

    use crate::models::run::{ArtifactReference, MetricValue};

    use super::{
        ExternalTrackingSource, merge_metrics, merge_run_artifacts, merge_run_params,
        merge_training_config_with_external, normalize_framework_name, normalize_tracking_source,
        preferred_artifact_uri,
    };

    #[test]
    fn normalizes_external_tracking_and_framework_names() {
        let tracking = normalize_tracking_source(ExternalTrackingSource {
            system: "Weights & Biases".to_string(),
            framework: "sklearn".to_string(),
            flavor: "MLflow.pyfunc".to_string(),
            run_id: "run-01".to_string(),
            ..ExternalTrackingSource::default()
        });

        assert_eq!(tracking.system, "wandb");
        assert_eq!(tracking.framework, "scikit-learn");
        assert_eq!(tracking.flavor, "pyfunc");
        assert_eq!(normalize_framework_name("torch"), "pytorch");
    }

    #[test]
    fn merges_external_tracking_into_run_params_and_artifacts() {
        let external = ExternalTrackingSource {
            system: "mlflow".to_string(),
            framework: "onnx".to_string(),
            model_uri: "models:/fraud-detector/12".to_string(),
            artifact_uri: "s3://mlflow-artifacts/fraud-detector/12".to_string(),
            params: json!({ "max_depth": 8 }),
            artifacts: vec![ArtifactReference {
                id: Uuid::now_v7(),
                name: "conda.yaml".to_string(),
                uri: "s3://mlflow-artifacts/fraud-detector/12/conda.yaml".to_string(),
                artifact_type: "environment".to_string(),
                size_bytes: 0,
            }],
            ..ExternalTrackingSource::default()
        };

        let params = merge_run_params(json!({ "learning_rate": 0.2 }), Some(&external));
        let artifacts = merge_run_artifacts(Vec::new(), Some(&external));

        assert_eq!(
            params.get("framework").and_then(Value::as_str),
            Some("onnx")
        );
        assert!(params.get("external_tracking").is_some());
        assert!(
            artifacts
                .iter()
                .any(|artifact| artifact.uri == external.model_uri)
        );
        assert!(
            artifacts.iter().any(
                |artifact| artifact.uri == "s3://mlflow-artifacts/fraud-detector/12/conda.yaml"
            )
        );
    }

    #[test]
    fn merges_external_training_into_training_config() {
        let external = ExternalTrackingSource {
            system: "mlflow".to_string(),
            framework: "xgboost".to_string(),
            model_uri: "models:/churn-model/9".to_string(),
            ..ExternalTrackingSource::default()
        };
        let config = merge_training_config_with_external(json!({}), Some(&external));

        assert_eq!(
            config.get("engine").and_then(Value::as_str),
            Some("xgboost")
        );
        assert!(config.get("model_adapter").is_some());
        assert_eq!(
            preferred_artifact_uri(Some(&external), Some(&config)).as_deref(),
            Some("models:/churn-model/9")
        );
    }

    #[test]
    fn dedupes_metrics_by_name() {
        let merged = merge_metrics(
            &[MetricValue {
                name: "accuracy".to_string(),
                value: 0.91,
            }],
            &[
                MetricValue {
                    name: "accuracy".to_string(),
                    value: 0.88,
                },
                MetricValue {
                    name: "roc_auc".to_string(),
                    value: 0.94,
                },
            ],
        );

        assert_eq!(merged.len(), 2);
        assert_eq!(merged[0].name, "accuracy");
        assert_eq!(merged[1].name, "roc_auc");
    }
}
