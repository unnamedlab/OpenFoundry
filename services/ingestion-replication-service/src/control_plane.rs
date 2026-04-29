//! Pure-function layer that renders [`IngestJobSpec`](crate::proto::IngestJobSpec)
//! values into the Kubernetes Custom Resources documented in
//! [`crate::crds`], plus the apply/delete glue against a `kube::Client`.
//!
//! Keeping the rendering side-effect-free makes it trivial to unit-test
//! without spinning up a Kubernetes cluster.

use std::collections::BTreeMap;

use anyhow::{Context, Result, anyhow, bail};
use k8s_openapi::apimachinery::pkg::apis::meta::v1::ObjectMeta;
use kube::api::{Api, DeleteParams, Patch, PatchParams, PostParams};
use kube::Client;
use serde_json::json;

use crate::crds::{
    FlinkDeployment, FlinkDeploymentSpec, JobSpec, KafkaConnector, KafkaConnectorSpec,
    ResourceLimits, ResourceSpec,
};
use crate::proto::{IcebergSink, IngestJobSpec, PostgresSource};

/// Field manager used for server-side apply patches.
pub const FIELD_MANAGER: &str = "ingestion-replication-service";

/// Default container image used when [`IcebergSink::flink_image`] is empty.
pub const DEFAULT_FLINK_IMAGE: &str = "apache/flink:1.18-scala_2.12-java11";

/// Default Flink version label sent to the operator when not specified.
pub const DEFAULT_FLINK_VERSION: &str = "v1_18";

/// Output of [`render_resources`]: the CRDs the control plane will apply.
#[derive(Debug, Clone)]
pub struct RenderedResources {
    pub kafka_connector: KafkaConnector,
    pub flink_deployment: Option<FlinkDeployment>,
}

/// Validate an [`IngestJobSpec`] and convert it into Kubernetes resources.
///
/// Currently the only supported `source` value is `"postgres"`. The function
/// does **not** perform any I/O — it produces value objects ready to be sent
/// to the API server.
pub fn render_resources(spec: &IngestJobSpec) -> Result<RenderedResources> {
    if spec.name.trim().is_empty() {
        bail!("IngestJobSpec.name must not be empty");
    }
    if spec.kafka_connect_cluster.trim().is_empty() {
        bail!("IngestJobSpec.kafka_connect_cluster must not be empty");
    }
    let namespace = if spec.namespace.trim().is_empty() {
        "default".to_string()
    } else {
        spec.namespace.clone()
    };

    let kafka_connector = match spec.source.as_str() {
        "postgres" => {
            let postgres = spec
                .postgres
                .as_ref()
                .ok_or_else(|| anyhow!("postgres source selected but `postgres` block missing"))?;
            render_postgres_kafka_connector(spec, postgres, &namespace)?
        }
        other => bail!("unsupported source kind '{other}'"),
    };

    let flink_deployment = spec
        .iceberg_sink
        .as_ref()
        .map(|sink| render_iceberg_flink_deployment(spec, sink, &namespace))
        .transpose()?;

    Ok(RenderedResources {
        kafka_connector,
        flink_deployment,
    })
}

/// Build a Strimzi `KafkaConnector` running the Debezium PostgreSQL
/// connector (`io.debezium.connector.postgresql.PostgresConnector`).
fn render_postgres_kafka_connector(
    spec: &IngestJobSpec,
    postgres: &PostgresSource,
    namespace: &str,
) -> Result<KafkaConnector> {
    if postgres.hostname.trim().is_empty() || postgres.database.trim().is_empty() {
        bail!("postgres source requires hostname and database");
    }
    let port = if postgres.port == 0 { 5432 } else { postgres.port };
    let topic_prefix = if postgres.topic_prefix.trim().is_empty() {
        spec.name.clone()
    } else {
        postgres.topic_prefix.clone()
    };
    let slot = if postgres.slot_name.trim().is_empty() {
        format!("{}_slot", spec.name.replace('-', "_"))
    } else {
        postgres.slot_name.clone()
    };
    let publication = if postgres.publication_name.trim().is_empty() {
        format!("{}_pub", spec.name.replace('-', "_"))
    } else {
        postgres.publication_name.clone()
    };

    let mut config: BTreeMap<String, serde_json::Value> = BTreeMap::new();
    config.insert("database.hostname".into(), json!(postgres.hostname));
    config.insert("database.port".into(), json!(port.to_string()));
    config.insert("database.user".into(), json!(postgres.user));
    config.insert("database.dbname".into(), json!(postgres.database));
    if !postgres.password_secret.trim().is_empty() {
        // Standard Strimzi/KafkaConnect external secret reference syntax.
        config.insert(
            "database.password".into(),
            json!(format!("${{secrets:{}/password}}", postgres.password_secret)),
        );
    }
    config.insert("plugin.name".into(), json!("pgoutput"));
    config.insert("slot.name".into(), json!(slot));
    config.insert("publication.name".into(), json!(publication));
    config.insert("topic.prefix".into(), json!(topic_prefix));
    if !postgres.tables.is_empty() {
        config.insert(
            "table.include.list".into(),
            json!(postgres.tables.join(",")),
        );
    }

    let mut labels = BTreeMap::new();
    labels.insert(
        "strimzi.io/cluster".to_string(),
        spec.kafka_connect_cluster.clone(),
    );
    labels.insert(
        "app.kubernetes.io/managed-by".to_string(),
        FIELD_MANAGER.to_string(),
    );
    labels.insert(
        "ingestion.openfoundry.io/job".to_string(),
        spec.name.clone(),
    );

    Ok(KafkaConnector {
        metadata: ObjectMeta {
            name: Some(format!("{}-debezium-pg", spec.name)),
            namespace: Some(namespace.to_string()),
            labels: Some(labels),
            ..Default::default()
        },
        spec: KafkaConnectorSpec {
            class: "io.debezium.connector.postgresql.PostgresConnector".to_string(),
            tasks_max: 1,
            config,
            state: None,
        },
    })
}

/// Build a `FlinkDeployment` that runs the Iceberg sink job. The job
/// arguments are deliberately small and human-readable: a real deployment
/// will normally bake the SQL/job into the container image referenced by
/// [`IcebergSink::flink_image`], but the control plane forwards the topic
/// + warehouse coordinates so the runtime can locate them.
fn render_iceberg_flink_deployment(
    spec: &IngestJobSpec,
    sink: &IcebergSink,
    namespace: &str,
) -> Result<FlinkDeployment> {
    if sink.warehouse.trim().is_empty()
        || sink.catalog_name.trim().is_empty()
        || sink.database.trim().is_empty()
        || sink.table.trim().is_empty()
    {
        bail!("iceberg sink requires warehouse, catalog_name, database and table");
    }
    let image = if sink.flink_image.trim().is_empty() {
        DEFAULT_FLINK_IMAGE.to_string()
    } else {
        sink.flink_image.clone()
    };
    let flink_version = if sink.flink_version.trim().is_empty() {
        DEFAULT_FLINK_VERSION.to_string()
    } else {
        sink.flink_version.clone()
    };
    let topic_prefix = spec
        .postgres
        .as_ref()
        .map(|p| {
            if p.topic_prefix.trim().is_empty() {
                spec.name.clone()
            } else {
                p.topic_prefix.clone()
            }
        })
        .unwrap_or_else(|| spec.name.clone());

    let mut labels = BTreeMap::new();
    labels.insert(
        "app.kubernetes.io/managed-by".to_string(),
        FIELD_MANAGER.to_string(),
    );
    labels.insert(
        "ingestion.openfoundry.io/job".to_string(),
        spec.name.clone(),
    );

    let mut flink_configuration = BTreeMap::new();
    flink_configuration.insert("taskmanager.numberOfTaskSlots".into(), "2".into());

    Ok(FlinkDeployment {
        metadata: ObjectMeta {
            name: Some(format!("{}-iceberg-sink", spec.name)),
            namespace: Some(namespace.to_string()),
            labels: Some(labels),
            ..Default::default()
        },
        spec: FlinkDeploymentSpec {
            image,
            flink_version,
            flink_configuration,
            service_account: Some("flink".to_string()),
            job_manager: ResourceSpec {
                resource: ResourceLimits {
                    memory: "1024m".into(),
                    cpu: 1.0,
                },
                replicas: Some(1),
            },
            task_manager: ResourceSpec {
                resource: ResourceLimits {
                    memory: "2048m".into(),
                    cpu: 1.0,
                },
                replicas: None,
            },
            job: JobSpec {
                jar_uri: "local:///opt/flink/usrlib/iceberg-sink.jar".into(),
                args: vec![
                    "--source-topic-prefix".into(),
                    topic_prefix,
                    "--iceberg-warehouse".into(),
                    sink.warehouse.clone(),
                    "--iceberg-catalog".into(),
                    sink.catalog_name.clone(),
                    "--iceberg-database".into(),
                    sink.database.clone(),
                    "--iceberg-table".into(),
                    sink.table.clone(),
                ],
                parallelism: 1,
                upgrade_mode: Some("last-state".into()),
            },
        },
    })
}

/// Server-side apply both rendered resources to the cluster pointed at by
/// `client`. Idempotent — safe to call from the reconcile loop.
pub async fn apply_resources(client: &Client, rendered: &RenderedResources) -> Result<()> {
    let ns = rendered
        .kafka_connector
        .metadata
        .namespace
        .as_deref()
        .unwrap_or("default");

    let kc_api: Api<KafkaConnector> = Api::namespaced(client.clone(), ns);
    let name = rendered
        .kafka_connector
        .metadata
        .name
        .as_deref()
        .ok_or_else(|| anyhow!("kafka connector without name"))?;
    kc_api
        .patch(
            name,
            &PatchParams::apply(FIELD_MANAGER).force(),
            &Patch::Apply(&rendered.kafka_connector),
        )
        .await
        .with_context(|| format!("apply KafkaConnector {ns}/{name}"))?;

    if let Some(flink) = &rendered.flink_deployment {
        let fl_api: Api<FlinkDeployment> = Api::namespaced(client.clone(), ns);
        let fname = flink
            .metadata
            .name
            .as_deref()
            .ok_or_else(|| anyhow!("flink deployment without name"))?;
        fl_api
            .patch(
                fname,
                &PatchParams::apply(FIELD_MANAGER).force(),
                &Patch::Apply(flink),
            )
            .await
            .with_context(|| format!("apply FlinkDeployment {ns}/{fname}"))?;
    }
    Ok(())
}

/// Best-effort delete the resources associated with a job from the cluster.
/// Missing resources are ignored.
pub async fn delete_resources(
    client: &Client,
    namespace: &str,
    kafka_connector_name: Option<&str>,
    flink_deployment_name: Option<&str>,
) -> Result<()> {
    let dp = DeleteParams::default();
    if let Some(name) = kafka_connector_name {
        let api: Api<KafkaConnector> = Api::namespaced(client.clone(), namespace);
        if let Err(err) = api.delete(name, &dp).await {
            if !is_not_found(&err) {
                return Err(err).with_context(|| format!("delete KafkaConnector {namespace}/{name}"));
            }
        }
    }
    if let Some(name) = flink_deployment_name {
        let api: Api<FlinkDeployment> = Api::namespaced(client.clone(), namespace);
        if let Err(err) = api.delete(name, &dp).await {
            if !is_not_found(&err) {
                return Err(err)
                    .with_context(|| format!("delete FlinkDeployment {namespace}/{name}"));
            }
        }
    }
    Ok(())
}

fn is_not_found(err: &kube::Error) -> bool {
    matches!(
        err,
        kube::Error::Api(resp) if resp.code == 404
    )
}

// `PostParams` is re-exported here because some downstream callers may want
// the default value without depending on `kube` directly.
#[allow(dead_code)]
pub fn default_post_params() -> PostParams {
    PostParams::default()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::{IcebergSink, IngestJobSpec, PostgresSource};

    fn sample_spec() -> IngestJobSpec {
        IngestJobSpec {
            name: "orders".into(),
            namespace: "data".into(),
            source: "postgres".into(),
            kafka_connect_cluster: "main-connect".into(),
            postgres: Some(PostgresSource {
                hostname: "pg.example.com".into(),
                port: 0,
                database: "shop".into(),
                user: "debezium".into(),
                password_secret: "pg-password".into(),
                slot_name: String::new(),
                publication_name: String::new(),
                tables: vec!["public.orders".into(), "public.line_items".into()],
                topic_prefix: String::new(),
            }),
            iceberg_sink: Some(IcebergSink {
                warehouse: "s3://lake/warehouse".into(),
                catalog_name: "lake".into(),
                database: "ops".into(),
                table: "orders".into(),
                flink_image: String::new(),
                flink_version: String::new(),
            }),
        }
    }

    #[test]
    fn renders_postgres_debezium_connector() {
        let rendered = render_resources(&sample_spec()).unwrap();
        let kc = &rendered.kafka_connector;
        assert_eq!(kc.metadata.name.as_deref(), Some("orders-debezium-pg"));
        assert_eq!(kc.metadata.namespace.as_deref(), Some("data"));
        assert_eq!(
            kc.metadata
                .labels
                .as_ref()
                .and_then(|l| l.get("strimzi.io/cluster"))
                .map(String::as_str),
            Some("main-connect"),
        );
        assert_eq!(
            kc.spec.class,
            "io.debezium.connector.postgresql.PostgresConnector"
        );
        assert_eq!(kc.spec.tasks_max, 1);
        assert_eq!(
            kc.spec.config.get("database.hostname").and_then(|v| v.as_str()),
            Some("pg.example.com"),
        );
        // Default port applied.
        assert_eq!(
            kc.spec.config.get("database.port").and_then(|v| v.as_str()),
            Some("5432"),
        );
        // Plugin pinned to logical-replication friendly default.
        assert_eq!(
            kc.spec.config.get("plugin.name").and_then(|v| v.as_str()),
            Some("pgoutput"),
        );
        assert_eq!(
            kc.spec
                .config
                .get("table.include.list")
                .and_then(|v| v.as_str()),
            Some("public.orders,public.line_items"),
        );
        assert!(
            kc.spec
                .config
                .get("database.password")
                .and_then(|v| v.as_str())
                .unwrap()
                .contains("pg-password"),
        );
    }

    #[test]
    fn renders_iceberg_flink_deployment() {
        let rendered = render_resources(&sample_spec()).unwrap();
        let flink = rendered
            .flink_deployment
            .expect("iceberg sink should produce a FlinkDeployment");
        assert_eq!(flink.metadata.name.as_deref(), Some("orders-iceberg-sink"));
        assert_eq!(flink.spec.image, DEFAULT_FLINK_IMAGE);
        assert_eq!(flink.spec.flink_version, DEFAULT_FLINK_VERSION);
        assert!(flink.spec.job.args.iter().any(|a| a == "s3://lake/warehouse"));
        assert!(flink.spec.job.args.iter().any(|a| a == "ops"));
        assert!(flink.spec.job.args.iter().any(|a| a == "orders"));
    }

    #[test]
    fn no_flink_when_no_iceberg_sink() {
        let mut spec = sample_spec();
        spec.iceberg_sink = None;
        let rendered = render_resources(&spec).unwrap();
        assert!(rendered.flink_deployment.is_none());
    }

    #[test]
    fn rejects_unsupported_source() {
        let mut spec = sample_spec();
        spec.source = "mysql".into();
        let err = render_resources(&spec).unwrap_err();
        assert!(err.to_string().contains("unsupported source"));
    }

    #[test]
    fn rejects_postgres_without_block() {
        let mut spec = sample_spec();
        spec.postgres = None;
        let err = render_resources(&spec).unwrap_err();
        assert!(err.to_string().contains("postgres"));
    }

    #[test]
    fn rejects_empty_name() {
        let mut spec = sample_spec();
        spec.name = String::new();
        let err = render_resources(&spec).unwrap_err();
        assert!(err.to_string().contains("name"));
    }
}
