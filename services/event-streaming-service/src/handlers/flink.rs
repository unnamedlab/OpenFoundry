//! REST handlers for the Flink runtime (Bloque D).

use axum::{Json, extract::Path};
use serde_json::Value;
use uuid::Uuid;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, not_found},
    models::topology::{TopologyDefinition, TopologyRow},
};
#[cfg(not(feature = "flink-runtime"))]
use crate::runtime::flink::sql::{render_flink_sql, RenderedFlinkSql};

async fn load_topology_row(db: &sqlx::PgPool, id: Uuid) -> Result<TopologyRow, sqlx::Error> {
    sqlx::query_as::<_, TopologyRow>(
        "SELECT id, name, description, status, nodes, edges, join_definition, cep_definition,
                backpressure_policy, source_stream_ids, sink_bindings, state_backend,
                checkpoint_interval_ms, runtime_kind, flink_job_name, flink_deployment_name,
                flink_job_id, flink_namespace, consistency_guarantee,
                created_at, updated_at
           FROM streaming_topologies
          WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
    .await
}

async fn load_streams(
    db: &sqlx::PgPool,
) -> Result<Vec<crate::models::stream::StreamDefinition>, sqlx::Error> {
    let rows = sqlx::query_as::<_, crate::models::stream::StreamRow>(
        "SELECT id, name, description, status, schema, source_binding, retention_hours, partitions, consistency_guarantee, stream_profile, created_at, updated_at
           FROM streaming_streams",
    )
    .fetch_all(db)
    .await?;
    Ok(rows.into_iter().map(Into::into).collect())
}

#[derive(Debug, serde::Serialize)]
pub struct DeployFlinkResponse {
    pub topology_id: Uuid,
    pub deployment_name: Option<String>,
    pub namespace: Option<String>,
    pub sql_warnings: Vec<String>,
    pub sql: String,
    pub message: String,
}

/// `POST /api/v1/streaming/topologies/:id/deploy`
///
/// Renders the Flink SQL for the topology and (when the binary was
/// built with `flink-runtime`) materialises it on the cluster.
pub async fn deploy_topology(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(id): Path<Uuid>,
) -> ServiceResult<DeployFlinkResponse> {
    let topology = match load_topology_row(&state.db, id).await {
        Ok(row) => TopologyDefinition::from(row),
        Err(sqlx::Error::RowNotFound) => return Err(not_found("topology not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    if topology.runtime_kind != "flink" {
        return Err(bad_request(
            "topology.runtime_kind must be 'flink' before deploy",
        ));
    }
    let streams = load_streams(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    deploy_inner(&state, topology, streams).await
}

#[cfg(feature = "flink-runtime")]
async fn deploy_inner(
    state: &AppState,
    topology: TopologyDefinition,
    streams: Vec<crate::models::stream::StreamDefinition>,
) -> ServiceResult<DeployFlinkResponse> {
    use crate::handlers::internal_error;
    use crate::runtime::flink::deployer;
    let report = deployer::deploy_topology(&state.db, &state.flink_config, &topology, &streams)
        .await
        .map_err(|e| internal_error(e.to_string()))?;
    Ok(Json(DeployFlinkResponse {
        topology_id: topology.id,
        deployment_name: Some(report.coords.deployment_name),
        namespace: Some(report.coords.namespace),
        sql_warnings: report.sql.warnings,
        sql: report.sql.script,
        message: "FlinkDeployment applied".to_string(),
    }))
}

#[cfg(not(feature = "flink-runtime"))]
async fn deploy_inner(
    _state: &AppState,
    topology: TopologyDefinition,
    streams: Vec<crate::models::stream::StreamDefinition>,
) -> ServiceResult<DeployFlinkResponse> {
    let RenderedFlinkSql { script, warnings } = render_flink_sql(&topology, &streams);
    Ok(Json(DeployFlinkResponse {
        topology_id: topology.id,
        deployment_name: topology.flink_deployment_name.clone(),
        namespace: topology.flink_namespace.clone(),
        sql_warnings: warnings,
        sql: script,
        message: "rendered SQL only; build with --features flink-runtime to apply"
            .to_string(),
    }))
}

/// `GET /api/v1/streaming/topologies/:id/job-graph`
pub async fn get_job_graph(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(id): Path<Uuid>,
) -> ServiceResult<Value> {
    let topology = match load_topology_row(&state.db, id).await {
        Ok(row) => TopologyDefinition::from(row),
        Err(sqlx::Error::RowNotFound) => return Err(not_found("topology not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    job_graph_inner(&state, &topology).await
}

#[cfg(feature = "flink-runtime")]
async fn job_graph_inner(
    state: &AppState,
    topology: &TopologyDefinition,
) -> ServiceResult<Value> {
    use crate::handlers::internal_error;
    use crate::runtime::flink::job_graph;
    let deployment = topology
        .flink_deployment_name
        .as_deref()
        .ok_or_else(|| bad_request("topology has no flink_deployment_name"))?;
    let namespace = topology
        .flink_namespace
        .clone()
        .unwrap_or_else(|| state.flink_config.default_namespace.clone());
    let payload = job_graph::fetch_job_graph(
        &state.flink_config,
        deployment,
        &namespace,
        topology.flink_job_id.as_deref(),
    )
    .await
    .map_err(|e| internal_error(e.to_string()))?;
    Ok(Json(payload))
}

#[cfg(not(feature = "flink-runtime"))]
async fn job_graph_inner(
    _state: &AppState,
    topology: &TopologyDefinition,
) -> ServiceResult<Value> {
    // Without the feature we still surface the topology-side DAG so the
    // UI can show *something*. We map nodes/edges into the same shape as
    // job_graph::normalise.
    let vertices: Vec<Value> = topology
        .nodes
        .iter()
        .map(|n| {
            serde_json::json!({
                "id": n.id,
                "name": n.label,
                "parallelism": Value::Null,
                "status": Value::Null,
            })
        })
        .collect();
    let edges: Vec<Value> = topology
        .edges
        .iter()
        .map(|e| {
            serde_json::json!({
                "source": e.source_node_id,
                "target": e.target_node_id,
            })
        })
        .collect();
    Ok(Json(serde_json::json!({
        "job_id": topology.flink_job_id,
        "vertices": vertices,
        "edges": edges,
        "raw": Value::Null,
        "message": "binary built without --features flink-runtime; returning topology DAG",
    })))
}
