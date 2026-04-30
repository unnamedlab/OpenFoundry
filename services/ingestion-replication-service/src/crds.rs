//! Strongly-typed Kubernetes Custom Resource definitions consumed by the
//! control plane.
//!
//! * [`KafkaConnector`] — Strimzi (`kafka.strimzi.io/v1beta2`). Used to
//!   declare a Debezium connector running on a `KafkaConnect` cluster.
//!   Upstream CRD docs:
//!   <https://strimzi.io/docs/operators/latest/configuring.html#type-KafkaConnector-reference>
//! * [`FlinkDeployment`] — Apache Flink Kubernetes Operator
//!   (`flink.apache.org/v1beta1`, Apache-2.0). Used to declare the streaming
//!   job that reads the CDC topic and sinks into Iceberg via the
//!   `iceberg-flink` runtime (Apache-2.0).
//!
//! Only the subset of fields the control plane actually fills in is modelled.
//! Everything else is left to operator defaults.

use std::collections::BTreeMap;

use kube::CustomResource;
use schemars::JsonSchema;
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Strimzi KafkaConnector
// ---------------------------------------------------------------------------

#[derive(CustomResource, Debug, Clone, Serialize, Deserialize, JsonSchema, PartialEq, Eq)]
#[kube(
    group = "kafka.strimzi.io",
    version = "v1beta2",
    kind = "KafkaConnector",
    plural = "kafkaconnectors",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct KafkaConnectorSpec {
    /// Fully qualified Java connector class.
    pub class: String,
    /// Number of tasks Strimzi should request from KafkaConnect.
    pub tasks_max: i32,
    /// Free-form connector configuration (passed verbatim to KafkaConnect).
    pub config: BTreeMap<String, serde_json::Value>,
    /// `running` (default) or `paused`.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub state: Option<String>,
}

// ---------------------------------------------------------------------------
// Apache Flink Kubernetes Operator FlinkDeployment
// ---------------------------------------------------------------------------

#[derive(CustomResource, Debug, Clone, Serialize, Deserialize, JsonSchema, PartialEq)]
#[kube(
    group = "flink.apache.org",
    version = "v1beta1",
    kind = "FlinkDeployment",
    plural = "flinkdeployments",
    namespaced
)]
#[serde(rename_all = "camelCase")]
pub struct FlinkDeploymentSpec {
    /// Container image with the Flink runtime (and the iceberg-flink jars).
    pub image: String,
    /// Apache Flink version label, e.g. "v1_18".
    pub flink_version: String,
    /// Free-form Flink configuration map (`flink-conf.yaml` overrides).
    #[serde(skip_serializing_if = "BTreeMap::is_empty", default)]
    pub flink_configuration: BTreeMap<String, String>,
    pub job_manager: ResourceSpec,
    pub task_manager: ResourceSpec,
    pub job: JobSpec,
    /// Service account the operator should use for the deployed pods.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub service_account: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ResourceSpec {
    pub resource: ResourceLimits,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub replicas: Option<i32>,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct ResourceLimits {
    pub memory: String,
    pub cpu: f32,
}

#[derive(Debug, Clone, Serialize, Deserialize, JsonSchema, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct JobSpec {
    /// Path inside the container to the job JAR (or `local:///` URI).
    pub jar_uri: String,
    /// CLI arguments forwarded to the Flink job.
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub args: Vec<String>,
    /// Initial parallelism.
    pub parallelism: i32,
    /// `stateless` (default) or `last-state`.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub upgrade_mode: Option<String>,
}
