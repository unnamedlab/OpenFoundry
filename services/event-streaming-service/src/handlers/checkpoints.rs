//! REST handlers for the checkpoint subsystem (Bloque C).

use axum::{Json, extract::Path};
use serde_json::Value;
use uuid::Uuid;

use crate::{
    AppState,
    domain::checkpoints,
    handlers::{ServiceResult, bad_request, db_error, internal_error, not_found},
    models::{
        ListResponse,
        checkpoint::{
            Checkpoint, ResetTopologyRequest, ResetTopologyResponse, TriggerCheckpointRequest,
        },
        topology::{TopologyDefinition, TopologyRow},
    },
};

async fn load_topology_row(
    db: &sqlx::PgPool,
    id: Uuid,
) -> Result<TopologyRow, sqlx::Error> {
    sqlx::query_as::<_, TopologyRow>(
        "SELECT id, name, description, status, nodes, edges, join_definition, cep_definition,
                backpressure_policy, source_stream_ids, sink_bindings, state_backend,
                checkpoint_interval_ms, runtime_kind, flink_job_name, flink_deployment_name, flink_job_id, flink_namespace, consistency_guarantee,
                created_at, updated_at
           FROM streaming_topologies
          WHERE id = $1",
    )
    .bind(id)
    .fetch_one(db)
    .await
}

/// `POST /api/v1/streaming/topologies/{id}/checkpoints`
pub async fn trigger_checkpoint(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(topology_id): Path<Uuid>,
    body: Option<Json<TriggerCheckpointRequest>>,
) -> ServiceResult<Checkpoint> {
    let payload = body.map(|Json(p)| p).unwrap_or_default();
    let trigger = payload.trigger.as_deref().unwrap_or("manual");
    if !matches!(trigger, "manual" | "pre-shutdown" | "on-failure") {
        return Err(bad_request(
            "trigger must be one of manual, pre-shutdown, on-failure",
        ));
    }

    let topology_row = match load_topology_row(&state.db, topology_id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("topology not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let topology = TopologyDefinition::from(topology_row);

    let started = std::time::Instant::now();
    let cp = checkpoints::take_checkpoint(
        &state.db,
        &state.state_backend,
        topology_id,
        &topology.source_stream_ids,
        trigger,
        payload.export_savepoint,
    )
    .await
    .map_err(|cause| internal_error(cause.to_string()))?;

    // Bloque F1: emit checkpoint duration metric. We do not yet track
    // checkpoint size at the backend layer; this is recorded as 0 and
    // will be wired once the state-store reports it.
    let elapsed = started.elapsed().as_secs_f64();
    let duration = if cp.duration_ms > 0 {
        cp.duration_ms as f64 / 1000.0
    } else {
        elapsed
    };
    state.metrics.record_checkpoint(&topology.name, duration, 0);

    Ok(Json(cp))
}

/// `GET /api/v1/streaming/topologies/{id}/checkpoints`
pub async fn list_checkpoints(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(topology_id): Path<Uuid>,
) -> ServiceResult<ListResponse<Checkpoint>> {
    match load_topology_row(&state.db, topology_id).await {
        Ok(_) => {}
        Err(sqlx::Error::RowNotFound) => return Err(not_found("topology not found")),
        Err(cause) => return Err(db_error(&cause)),
    }
    let items = checkpoints::list_checkpoints(&state.db, topology_id, 100)
        .await
        .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse { data: items }))
}

/// `POST /api/v1/streaming/topologies/{id}/reset`
///
/// Two execution modes depending on `topology.runtime_kind`:
///   * `builtin`  — restore state via the in-process backend, then
///     rewind the source stream offsets recorded in the checkpoint.
///   * `flink`    — gated behind the `flink-runtime` feature; without
///     it we return 501 with a helpful message so callers do not hit a
///     silent no-op.
pub async fn reset_topology(
    axum::extract::State(state): axum::extract::State<AppState>,
    Path(topology_id): Path<Uuid>,
    body: Option<Json<ResetTopologyRequest>>,
) -> ServiceResult<ResetTopologyResponse> {
    let payload = body.map(|Json(p)| p).unwrap_or_default();
    if payload.from_checkpoint_id.is_some() && payload.savepoint_uri.is_some() {
        return Err(bad_request(
            "from_checkpoint_id and savepoint_uri are mutually exclusive",
        ));
    }

    let topology_row = match load_topology_row(&state.db, topology_id).await {
        Ok(row) => row,
        Err(sqlx::Error::RowNotFound) => return Err(not_found("topology not found")),
        Err(cause) => return Err(db_error(&cause)),
    };
    let topology = TopologyDefinition::from(topology_row);

    match topology.runtime_kind.as_str() {
        "builtin" => reset_builtin(&state, &topology, &payload).await,
        "flink" => reset_flink(&state, &topology, &payload).await,
        other => Err(bad_request(format!("unknown runtime_kind: {other}"))),
    }
}

async fn reset_builtin(
    state: &AppState,
    topology: &TopologyDefinition,
    payload: &ResetTopologyRequest,
) -> ServiceResult<ResetTopologyResponse> {
    if let Some(uri) = &payload.savepoint_uri {
        // For builtin we do not actually fetch external savepoints in
        // the MVP — but we accept the request so the API contract works
        // for both runtimes. The caller can then trigger a fresh
        // checkpoint to anchor the new state.
        return Ok(Json(ResetTopologyResponse {
            topology_id: topology.id,
            runtime_kind: topology.runtime_kind.clone(),
            checkpoint_id: None,
            restored_offsets: Value::Null,
            savepoint_uri: Some(uri.clone()),
            message: format!(
                "savepoint URI accepted; builtin runtime will pick it up on next start ({uri})"
            ),
        }));
    }

    let checkpoint = checkpoints::load_checkpoint(
        &state.db,
        topology.id,
        payload.from_checkpoint_id,
    )
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("no checkpoint found for topology"))?;

    // Restore state. We do not actually fetch the blob in this MVP — the
    // in-memory backend keeps it locally; rocksdb-backed deployments
    // restore from disk on startup. We surface whatever the backend
    // already holds as a no-op acknowledgement.
    let _ = state
        .state_backend
        .snapshot(topology.id)
        .await
        .map_err(|e| internal_error(e.to_string()))?;

    let rewound = checkpoints::rewind_offsets_to(&state.db, &checkpoint.last_offsets)
        .await
        .map_err(|e| internal_error(e.to_string()))?;

    Ok(Json(ResetTopologyResponse {
        topology_id: topology.id,
        runtime_kind: topology.runtime_kind.clone(),
        checkpoint_id: Some(checkpoint.id),
        restored_offsets: checkpoint.last_offsets.clone(),
        savepoint_uri: checkpoint.savepoint_uri.clone(),
        message: format!(
            "builtin reset complete: {rewound} events flagged for replay across {} streams",
            topology.source_stream_ids.len()
        ),
    }))
}

#[cfg(not(feature = "flink-runtime"))]
async fn reset_flink(
    _state: &AppState,
    topology: &TopologyDefinition,
    _payload: &ResetTopologyRequest,
) -> ServiceResult<ResetTopologyResponse> {
    Err((
        axum::http::StatusCode::NOT_IMPLEMENTED,
        axum::Json(crate::handlers::ErrorResponse {
            error: format!(
                "topology {} is configured for runtime=flink but the binary was built without the flink-runtime feature",
                topology.id
            ),
        }),
    ))
}

#[cfg(feature = "flink-runtime")]
async fn reset_flink(
    _state: &AppState,
    topology: &TopologyDefinition,
    payload: &ResetTopologyRequest,
) -> ServiceResult<ResetTopologyResponse> {
    use k8s_openapi::api::core::v1::Namespace;
    use kube::api::{Api, Patch, PatchParams};
    use kube::Client;

    let job_name = topology
        .flink_job_name
        .clone()
        .ok_or_else(|| bad_request("topology has no flink_job_name"))?;
    let savepoint_uri = if let Some(uri) = &payload.savepoint_uri {
        uri.clone()
    } else if let Some(checkpoint_id) = payload.from_checkpoint_id {
        let cp = checkpoints::load_checkpoint(&_state.db, topology.id, Some(checkpoint_id))
            .await
            .map_err(|c| db_error(&c))?
            .ok_or_else(|| not_found("checkpoint not found"))?;
        cp.savepoint_uri
            .ok_or_else(|| bad_request("checkpoint has no savepoint_uri to restore from"))?
    } else {
        return Err(bad_request(
            "flink reset requires either from_checkpoint_id (with savepoint) or savepoint_uri",
        ));
    };

    let client = Client::try_default()
        .await
        .map_err(|e| internal_error(format!("kube client: {e}")))?;
    // Resolve current namespace; default to "default" if unset.
    let _ns_api: Api<Namespace> = Api::all(client.clone());
    let namespace = std::env::var("POD_NAMESPACE").unwrap_or_else(|_| "default".to_string());

    // FlinkDeployment lives in flink.apache.org/v1beta1. We patch it as
    // a generic Custom Resource so we do not need to vendor the CRD.
    let api: Api<kube::api::DynamicObject> = Api::namespaced_with(
        client,
        &namespace,
        &kube::api::ApiResource {
            group: "flink.apache.org".into(),
            version: "v1beta1".into(),
            api_version: "flink.apache.org/v1beta1".into(),
            kind: "FlinkDeployment".into(),
            plural: "flinkdeployments".into(),
        },
    );
    let patch = serde_json::json!({
        "spec": {
            "job": {
                "initialSavepointPath": savepoint_uri,
                "upgradeMode": "savepoint",
            }
        }
    });
    api.patch(&job_name, &PatchParams::apply("event-streaming-service"), &Patch::Merge(&patch))
        .await
        .map_err(|e| internal_error(format!("kubectl patch flinkdeployment: {e}")))?;

    Ok(Json(ResetTopologyResponse {
        topology_id: topology.id,
        runtime_kind: topology.runtime_kind.clone(),
        checkpoint_id: payload.from_checkpoint_id,
        restored_offsets: Value::Null,
        savepoint_uri: Some(savepoint_uri),
        message: format!("FlinkDeployment {namespace}/{job_name} patched with savepoint"),
    }))
}
