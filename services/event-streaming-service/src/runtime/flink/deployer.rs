//! Apache Flink Kubernetes Operator deployer.
//!
//! Materialises a topology as:
//!
//!   1. A `ConfigMap/{deployment}-sql` with the rendered Flink SQL.
//!   2. A `FlinkDeployment/{deployment}` running the `sql-runner.jar`
//!      image, with `args` pointing at the ConfigMap.
//!   3. A `FlinkSessionJob` is intentionally skipped — `mode: native`
//!      runs the job inside the same FlinkDeployment, which is the
//!      shape used by `infra/k8s/flink/flinkdeployment-cdc-iceberg.yaml`
//!      in production. The `FlinkSessionJob` path is left as a TODO.
//!
//! Both resources are upserted via SSA (`Patch::Apply`) so the
//! deployer is idempotent.

use kube::api::{Api, ApiResource, DynamicObject, Patch, PatchParams, PostParams};
use kube::Client;
use serde_json::{json, Value};
use sqlx::PgPool;
use uuid::Uuid;

use crate::models::stream::StreamDefinition;
use crate::models::topology::TopologyDefinition;
use crate::runtime::flink::sql::{render_flink_sql, RenderedFlinkSql};
use crate::runtime::flink::{FlinkJobCoords, FlinkRuntimeConfig};

const FIELD_MANAGER: &str = "event-streaming-service";

/// Outcome of a single deployment.
#[derive(Debug, Clone)]
pub struct DeploymentReport {
    pub coords: FlinkJobCoords,
    pub sql: RenderedFlinkSql,
}

/// Errors surfaced by the deployer. We collapse the wide
/// `kube::Error`/`sqlx::Error` graphs into strings at the boundary so the
/// REST handlers can map them to HTTP 5xx with a useful message.
#[derive(Debug, thiserror::Error)]
pub enum DeployerError {
    #[error("kube client: {0}")]
    Kube(String),
    #[error("database: {0}")]
    Db(#[from] sqlx::Error),
}

/// Deploy `topology` onto the Flink cluster reachable via the in-cluster
/// kube API. Updates `streaming_topologies.flink_deployment_name`
/// and `flink_namespace` on success.
pub async fn deploy_topology(
    db: &PgPool,
    cfg: &FlinkRuntimeConfig,
    topology: &TopologyDefinition,
    streams: &[StreamDefinition],
) -> Result<DeploymentReport, DeployerError> {
    let namespace = topology
        .flink_namespace
        .clone()
        .unwrap_or_else(|| cfg.default_namespace.clone());
    let deployment_name = topology
        .flink_deployment_name
        .clone()
        .or_else(|| topology.flink_job_name.clone())
        .unwrap_or_else(|| format!("topo-{}", topology.id.simple()));

    let sql = render_flink_sql(topology, streams);

    let client = Client::try_default()
        .await
        .map_err(|e| DeployerError::Kube(e.to_string()))?;

    apply_sql_configmap(&client, &namespace, &deployment_name, &sql.script).await?;
    apply_flink_deployment(&client, cfg, &namespace, &deployment_name, topology).await?;

    sqlx::query(
        "UPDATE streaming_topologies
            SET flink_deployment_name = $2,
                flink_namespace = $3,
                runtime_kind = 'flink',
                updated_at = now()
          WHERE id = $1",
    )
    .bind(topology.id)
    .bind(&deployment_name)
    .bind(&namespace)
    .execute(db)
    .await?;

    Ok(DeploymentReport {
        coords: FlinkJobCoords {
            deployment_name,
            namespace,
            job_id: None,
        },
        sql,
    })
}

async fn apply_sql_configmap(
    client: &Client,
    namespace: &str,
    deployment: &str,
    script: &str,
) -> Result<(), DeployerError> {
    let api: Api<k8s_openapi::api::core::v1::ConfigMap> =
        Api::namespaced(client.clone(), namespace);
    let cm_name = format!("{deployment}-sql");
    let cm = k8s_openapi::api::core::v1::ConfigMap {
        metadata: k8s_openapi::apimachinery::pkg::apis::meta::v1::ObjectMeta {
            name: Some(cm_name.clone()),
            namespace: Some(namespace.to_string()),
            labels: Some(
                [
                    ("app.kubernetes.io/managed-by".to_string(), FIELD_MANAGER.to_string()),
                    ("openfoundry.io/component".to_string(), "flink-sql".to_string()),
                ]
                .into_iter()
                .collect(),
            ),
            ..Default::default()
        },
        data: Some(
            [("topology.sql".to_string(), script.to_string())]
                .into_iter()
                .collect(),
        ),
        ..Default::default()
    };
    api.patch(
        &cm_name,
        &PatchParams::apply(FIELD_MANAGER).force(),
        &Patch::Apply(&cm),
    )
    .await
    .map_err(|e| DeployerError::Kube(format!("apply ConfigMap/{cm_name}: {e}")))?;
    Ok(())
}

async fn apply_flink_deployment(
    client: &Client,
    cfg: &FlinkRuntimeConfig,
    namespace: &str,
    deployment: &str,
    topology: &TopologyDefinition,
) -> Result<(), DeployerError> {
    let resource = ApiResource {
        group: "flink.apache.org".into(),
        version: "v1beta1".into(),
        api_version: "flink.apache.org/v1beta1".into(),
        kind: "FlinkDeployment".into(),
        plural: "flinkdeployments".into(),
    };
    let api: Api<DynamicObject> = Api::namespaced_with(client.clone(), namespace, &resource);

    let body = render_flink_deployment_manifest(cfg, namespace, deployment, topology);
    // We round-trip through DynamicObject so kube's typed Patch::Apply
    // gets the right TypeMeta wired up.
    let dyn_obj: DynamicObject = serde_json::from_value(body)
        .map_err(|e| DeployerError::Kube(format!("manifest serialise: {e}")))?;

    match api
        .patch(
            deployment,
            &PatchParams::apply(FIELD_MANAGER).force(),
            &Patch::Apply(&dyn_obj),
        )
        .await
    {
        Ok(_) => Ok(()),
        Err(kube::Error::Api(err)) if err.code == 404 => {
            // Some operator builds reject SSA on first creation; fall
            // back to a plain create.
            api.create(&PostParams::default(), &dyn_obj)
                .await
                .map_err(|e| DeployerError::Kube(format!("create FlinkDeployment: {e}")))?;
            Ok(())
        }
        Err(e) => Err(DeployerError::Kube(format!(
            "apply FlinkDeployment/{deployment}: {e}"
        ))),
    }
}

/// Pure helper: build the FlinkDeployment manifest as JSON. Exposed for
/// unit tests and for the `--render-only` debug endpoint.
pub fn render_flink_deployment_manifest(
    cfg: &FlinkRuntimeConfig,
    namespace: &str,
    deployment: &str,
    topology: &TopologyDefinition,
) -> Value {
    let checkpoint_dir = format!("{}/checkpoints/{}", cfg.state_bucket_uri, deployment);
    let savepoint_dir = format!("{}/savepoints/{}", cfg.state_bucket_uri, deployment);
    let ha_dir = format!("{}/ha/{}", cfg.state_bucket_uri, deployment);
    let checkpointing_mode = match topology.consistency_guarantee.as_str() {
        "exactly-once" => "EXACTLY_ONCE",
        _ => "AT_LEAST_ONCE",
    };

    json!({
        "apiVersion": "flink.apache.org/v1beta1",
        "kind": "FlinkDeployment",
        "metadata": {
            "name": deployment,
            "namespace": namespace,
            "labels": {
                "app.kubernetes.io/managed-by": FIELD_MANAGER,
                "openfoundry.io/topology-id": topology.id.to_string(),
                "openfoundry.io/topology-name": sanitize_label(&topology.name),
            }
        },
        "spec": {
            "image": cfg.sql_runner_image,
            "flinkVersion": cfg.flink_version,
            "mode": "native",
            "serviceAccount": "flink",
            "flinkConfiguration": {
                "taskmanager.numberOfTaskSlots": "2",
                "parallelism.default": "4",
                "high-availability.type": "kubernetes",
                "high-availability.storageDir": ha_dir,
                "state.backend.type": "rocksdb",
                "state.backend.incremental": "true",
                "state.checkpoints.dir": checkpoint_dir,
                "state.savepoints.dir": savepoint_dir,
                "execution.checkpointing.mode": checkpointing_mode,
                "execution.checkpointing.interval": format!("{}", topology.checkpoint_interval_ms),
                "execution.checkpointing.timeout": "600000",
                "metrics.reporter.prom.factory.class":
                    "org.apache.flink.metrics.prometheus.PrometheusReporterFactory",
                "metrics.reporter.prom.port": "9249",
            },
            "jobManager": {
                "replicas": 1,
                "resource": { "memory": "1024m", "cpu": 1 }
            },
            "taskManager": {
                "resource": { "memory": "2048m", "cpu": 1 }
            },
            "podTemplate": {
                "spec": {
                    "containers": [{
                        "name": "flink-main-container",
                        "volumeMounts": [{
                            "name": "topology-sql",
                            "mountPath": "/opt/flink/usrlib/sql",
                        }]
                    }],
                    "volumes": [{
                        "name": "topology-sql",
                        "configMap": { "name": format!("{deployment}-sql") }
                    }]
                }
            },
            "job": {
                "jarURI": "local:///opt/flink/usrlib/sql-runner.jar",
                "args": ["--script", "/opt/flink/usrlib/sql/topology.sql"],
                "parallelism": 2,
                "upgradeMode": "savepoint",
                "state": "running",
            }
        }
    })
}

fn sanitize_label(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for ch in s.chars().take(63) {
        if ch.is_ascii_alphanumeric() || ch == '-' || ch == '_' || ch == '.' {
            out.push(ch);
        } else {
            out.push('-');
        }
    }
    out
}

/// Tear down the resources owned by the topology. Idempotent.
pub async fn delete_topology(
    cfg: &FlinkRuntimeConfig,
    coords: &FlinkJobCoords,
) -> Result<(), DeployerError> {
    let client = Client::try_default()
        .await
        .map_err(|e| DeployerError::Kube(e.to_string()))?;
    let resource = ApiResource {
        group: "flink.apache.org".into(),
        version: "v1beta1".into(),
        api_version: "flink.apache.org/v1beta1".into(),
        kind: "FlinkDeployment".into(),
        plural: "flinkdeployments".into(),
    };
    let api: Api<DynamicObject> =
        Api::namespaced_with(client.clone(), &coords.namespace, &resource);
    let _ = api
        .delete(&coords.deployment_name, &Default::default())
        .await
        .map_err(|e| {
            tracing::warn!("delete FlinkDeployment failed (ignored): {e}");
            DeployerError::Kube(e.to_string())
        });
    let cm: Api<k8s_openapi::api::core::v1::ConfigMap> =
        Api::namespaced(client, &coords.namespace);
    let _ = cm
        .delete(
            &format!("{}-sql", coords.deployment_name),
            &Default::default(),
        )
        .await;
    let _ = cfg; // future: clean up state bucket prefixes
    let _: Option<Uuid> = None; // silence unused import lint
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::Value;

    #[test]
    fn manifest_carries_topology_metadata_and_checkpointing() {
        let cfg = FlinkRuntimeConfig {
            default_namespace: "flink".into(),
            sql_runner_image: "ghcr.io/x/sql-runner:1.19".into(),
            flink_version: "v1_19".into(),
            jobmanager_url_template: "http://{deployment}-rest.{namespace}.svc:8081".into(),
            metrics_poll_interval_ms: 15_000,
            state_bucket_uri: "s3://bucket/flink".into(),
        };
        let topology = sample_topology();
        let manifest =
            render_flink_deployment_manifest(&cfg, "flink", "topo-demo", &topology);
        assert_eq!(manifest["kind"], Value::String("FlinkDeployment".into()));
        assert_eq!(
            manifest["metadata"]["labels"]["openfoundry.io/topology-id"],
            Value::String(topology.id.to_string())
        );
        assert_eq!(
            manifest["spec"]["flinkConfiguration"]["execution.checkpointing.mode"],
            Value::String("EXACTLY_ONCE".into())
        );
        assert_eq!(
            manifest["spec"]["flinkConfiguration"]["state.checkpoints.dir"],
            Value::String("s3://bucket/flink/checkpoints/topo-demo".into())
        );
    }

    fn sample_topology() -> TopologyDefinition {
        use chrono::Utc;
        TopologyDefinition {
            id: Uuid::now_v7(),
            name: "demo".into(),
            description: String::new(),
            status: "active".into(),
            nodes: Vec::new(),
            edges: Vec::new(),
            join_definition: None,
            cep_definition: None,
            backpressure_policy: crate::models::topology::BackpressurePolicy::default(),
            source_stream_ids: Vec::new(),
            sink_bindings: Vec::new(),
            state_backend: "rocksdb".into(),
            checkpoint_interval_ms: 60_000,
            runtime_kind: "flink".into(),
            flink_job_name: None,
            flink_deployment_name: None,
            flink_job_id: None,
            flink_namespace: None,
            consistency_guarantee: "exactly-once".into(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }
}
