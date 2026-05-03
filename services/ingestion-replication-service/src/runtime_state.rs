//! Non-Postgres runtime state for ingestion jobs.
//!
//! Desired state (`IngestJobSpec`) remains in Postgres so the reconcile loop
//! has a declarative inventory to work from. High-frequency runtime status,
//! last materialisation result and transient errors live in Kubernetes
//! `ConfigMap`s instead so the control plane no longer treats CNPG as the
//! authoritative hot-path store.

use std::collections::BTreeMap;

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use k8s_openapi::api::core::v1::ConfigMap;
use kube::Client;
use kube::api::{Api, DeleteParams, Patch, PatchParams};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::control_plane::FIELD_MANAGER;
use crate::repository::{self, IngestJobRecord};

const RUNTIME_DATA_KEY: &str = "runtime-state.json";
const MANAGED_BY_LABEL: &str = "app.kubernetes.io/managed-by";
const JOB_ID_LABEL: &str = "ingestion.openfoundry.io/job-id";
const JOB_NAME_LABEL: &str = "ingestion.openfoundry.io/job";

pub mod status {
    pub const PENDING: &str = "pending";
    pub const MATERIALIZED: &str = "materialized";
    pub const FAILED: &str = "failed";
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IngestJobRuntimeState {
    pub status: String,
    pub kafka_connector_name: Option<String>,
    pub flink_deployment_name: Option<String>,
    pub error: Option<String>,
    pub observed_at: DateTime<Utc>,
}

impl IngestJobRuntimeState {
    pub fn pending() -> Self {
        Self {
            status: status::PENDING.to_string(),
            kafka_connector_name: None,
            flink_deployment_name: None,
            error: None,
            observed_at: Utc::now(),
        }
    }

    pub fn materialized(
        kafka_connector_name: impl Into<String>,
        flink_deployment_name: Option<&str>,
    ) -> Self {
        Self {
            status: status::MATERIALIZED.to_string(),
            kafka_connector_name: Some(kafka_connector_name.into()),
            flink_deployment_name: flink_deployment_name.map(ToOwned::to_owned),
            error: None,
            observed_at: Utc::now(),
        }
    }

    pub fn failed(error: impl Into<String>) -> Self {
        Self {
            status: status::FAILED.to_string(),
            kafka_connector_name: None,
            flink_deployment_name: None,
            error: Some(error.into()),
            observed_at: Utc::now(),
        }
    }
}

pub fn runtime_configmap_name(job_id: Uuid) -> String {
    format!("ingest-job-{job_id}-runtime")
}

pub async fn upsert_job_runtime_state(
    client: &Client,
    record: &IngestJobRecord,
    state: &IngestJobRuntimeState,
) -> Result<()> {
    let api: Api<ConfigMap> = Api::namespaced(client.clone(), &record.namespace);
    let name = runtime_configmap_name(record.id);
    let mut labels = BTreeMap::new();
    labels.insert(MANAGED_BY_LABEL.to_string(), FIELD_MANAGER.to_string());
    labels.insert(JOB_ID_LABEL.to_string(), record.id.to_string());
    labels.insert(JOB_NAME_LABEL.to_string(), record.name.clone());

    let mut data = BTreeMap::new();
    data.insert(
        RUNTIME_DATA_KEY.to_string(),
        serde_json::to_string(state).context("serialize ingest runtime state")?,
    );

    let patch = serde_json::json!({
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {
            "name": name,
            "namespace": record.namespace,
            "labels": labels,
        },
        "data": data,
    });

    api.patch(
        &name,
        &PatchParams::apply(FIELD_MANAGER).force(),
        &Patch::Apply(&patch),
    )
    .await
    .with_context(|| {
        format!(
            "upsert runtime ConfigMap {}",
            runtime_configmap_name(record.id)
        )
    })?;

    Ok(())
}

pub async fn get_job_runtime_state(
    client: &Client,
    record: &IngestJobRecord,
) -> Result<Option<IngestJobRuntimeState>> {
    let api: Api<ConfigMap> = Api::namespaced(client.clone(), &record.namespace);
    let Some(cm) = api
        .get_opt(&runtime_configmap_name(record.id))
        .await
        .with_context(|| {
            format!(
                "get runtime ConfigMap {}",
                runtime_configmap_name(record.id)
            )
        })?
    else {
        return Ok(None);
    };

    let Some(raw) = cm
        .data
        .as_ref()
        .and_then(|data| data.get(RUNTIME_DATA_KEY))
        .cloned()
    else {
        return Ok(None);
    };

    let state = serde_json::from_str(&raw).context("deserialize ingest runtime state")?;
    Ok(Some(state))
}

pub async fn delete_job_runtime_state(
    client: &Client,
    namespace: &str,
    job_id: Uuid,
) -> Result<()> {
    let api: Api<ConfigMap> = Api::namespaced(client.clone(), namespace);
    match api
        .delete(&runtime_configmap_name(job_id), &DeleteParams::default())
        .await
    {
        Ok(_) => Ok(()),
        Err(kube::Error::Api(err)) if err.code == 404 => Ok(()),
        Err(err) => Err(err).context("delete runtime ConfigMap"),
    }
}

pub async fn hydrate_job(
    client: &Client,
    record: IngestJobRecord,
) -> Result<crate::proto::IngestJob> {
    let runtime = get_job_runtime_state(client, &record).await?;
    let fallback = runtime_state_from_record(&record);
    let runtime = runtime.unwrap_or(fallback);

    Ok(crate::proto::IngestJob {
        id: record.id.to_string(),
        spec: Some(record.spec),
        status: runtime.status,
        kafka_connector_name: runtime
            .kafka_connector_name
            .or(record.kafka_connector_name)
            .unwrap_or_default(),
        flink_deployment_name: runtime
            .flink_deployment_name
            .or(record.flink_deployment_name)
            .unwrap_or_default(),
        created_at: record.created_at.to_rfc3339(),
        updated_at: runtime.observed_at.to_rfc3339(),
        error: runtime.error.unwrap_or_default(),
    })
}

fn runtime_state_from_record(record: &IngestJobRecord) -> IngestJobRuntimeState {
    let status = match record.status.as_str() {
        repository::status::MATERIALIZED => status::MATERIALIZED,
        repository::status::FAILED => status::FAILED,
        _ => status::PENDING,
    };

    IngestJobRuntimeState {
        status: status.to_string(),
        kafka_connector_name: record.kafka_connector_name.clone(),
        flink_deployment_name: record.flink_deployment_name.clone(),
        error: record.error.clone(),
        observed_at: record.updated_at,
    }
}
