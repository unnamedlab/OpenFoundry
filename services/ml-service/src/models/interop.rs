use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::models::run::{ArtifactReference, MetricValue};

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ExternalTrackingSource {
    #[serde(default)]
    pub system: String,
    #[serde(default)]
    pub project: String,
    #[serde(default)]
    pub experiment_name: String,
    #[serde(default)]
    pub run_id: String,
    #[serde(default)]
    pub run_name: String,
    #[serde(default)]
    pub run_uri: String,
    #[serde(default)]
    pub artifact_uri: String,
    #[serde(default)]
    pub model_uri: String,
    #[serde(default)]
    pub registered_model_name: String,
    #[serde(default)]
    pub registered_model_version: String,
    #[serde(default)]
    pub framework: String,
    #[serde(default)]
    pub flavor: String,
    #[serde(default)]
    pub stage: String,
    #[serde(default)]
    pub tags: Value,
    #[serde(default)]
    pub params: Value,
    #[serde(default)]
    pub metrics: Vec<MetricValue>,
    #[serde(default)]
    pub artifacts: Vec<ArtifactReference>,
    #[serde(default)]
    pub metadata: Value,
}

impl ExternalTrackingSource {
    pub fn has_signal(&self) -> bool {
        !self.system.trim().is_empty()
            || !self.project.trim().is_empty()
            || !self.experiment_name.trim().is_empty()
            || !self.run_id.trim().is_empty()
            || !self.run_name.trim().is_empty()
            || !self.run_uri.trim().is_empty()
            || !self.artifact_uri.trim().is_empty()
            || !self.model_uri.trim().is_empty()
            || !self.registered_model_name.trim().is_empty()
            || !self.registered_model_version.trim().is_empty()
            || !self.framework.trim().is_empty()
            || !self.flavor.trim().is_empty()
            || !self.stage.trim().is_empty()
            || !self.metrics.is_empty()
            || !self.artifacts.is_empty()
            || !self.params.is_null()
            || !self.tags.is_null()
            || !self.metadata.is_null()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ModelAdapterDescriptor {
    #[serde(default)]
    pub kind: String,
    #[serde(default)]
    pub framework: String,
    #[serde(default)]
    pub flavor: String,
    #[serde(default)]
    pub runtime: String,
    #[serde(default)]
    pub loader: String,
    #[serde(default)]
    pub artifact_uri: String,
    #[serde(default)]
    pub entrypoint: String,
    #[serde(default)]
    pub requirements_uri: String,
    #[serde(default)]
    pub metadata: Value,
}

impl ModelAdapterDescriptor {
    pub fn has_signal(&self) -> bool {
        !self.kind.trim().is_empty()
            || !self.framework.trim().is_empty()
            || !self.flavor.trim().is_empty()
            || !self.runtime.trim().is_empty()
            || !self.loader.trim().is_empty()
            || !self.artifact_uri.trim().is_empty()
            || !self.entrypoint.trim().is_empty()
            || !self.requirements_uri.trim().is_empty()
            || !self.metadata.is_null()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct RegistrySourceDescriptor {
    #[serde(default)]
    pub system: String,
    #[serde(default)]
    pub model_name: String,
    #[serde(default)]
    pub model_version: String,
    #[serde(default)]
    pub stage: String,
    #[serde(default)]
    pub uri: String,
    #[serde(default)]
    pub metadata: Value,
}

impl RegistrySourceDescriptor {
    pub fn has_signal(&self) -> bool {
        !self.system.trim().is_empty()
            || !self.model_name.trim().is_empty()
            || !self.model_version.trim().is_empty()
            || !self.stage.trim().is_empty()
            || !self.uri.trim().is_empty()
            || !self.metadata.is_null()
    }
}
