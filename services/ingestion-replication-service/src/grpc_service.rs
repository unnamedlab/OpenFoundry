//! Tonic implementation of the `IngestionControlPlane` gRPC service.

#![allow(clippy::result_large_err)] // `tonic::Status` is large; idiomatic for gRPC handlers.

use std::sync::Arc;

use kube::Client;
use sqlx::PgPool;
use tonic::{Request, Response, Status};
use uuid::Uuid;

use crate::control_plane::{apply_resources, delete_resources, render_resources};
use crate::proto::ingestion_control_plane_server::IngestionControlPlane;
use crate::proto::{
    CreateIngestJobRequest, DeleteIngestJobRequest, DeleteIngestJobResponse, GetIngestJobRequest,
    IngestJob, ListIngestJobsRequest, ListIngestJobsResponse,
};
use crate::repository;
use crate::runtime_state::{self, IngestJobRuntimeState};

/// Concrete state injected into the gRPC service.
#[derive(Clone)]
pub struct ControlPlaneState {
    pub db: PgPool,
    pub kube: Client,
    pub default_namespace: Arc<str>,
}

#[derive(Clone)]
pub struct ControlPlaneService {
    state: ControlPlaneState,
}

impl ControlPlaneService {
    pub fn new(state: ControlPlaneState) -> Self {
        Self { state }
    }
}

fn parse_id(raw: &str) -> Result<Uuid, Status> {
    Uuid::parse_str(raw).map_err(|_| Status::invalid_argument("id is not a valid UUID"))
}

fn internal<E: std::fmt::Display>(label: &str, err: E) -> Status {
    tracing::error!("{label}: {err}");
    Status::internal(format!("{label}: {err}"))
}

#[tonic::async_trait]
impl IngestionControlPlane for ControlPlaneService {
    async fn create_ingest_job(
        &self,
        request: Request<CreateIngestJobRequest>,
    ) -> Result<Response<IngestJob>, Status> {
        let mut spec = request
            .into_inner()
            .spec
            .ok_or_else(|| Status::invalid_argument("spec is required"))?;
        if spec.namespace.trim().is_empty() {
            spec.namespace = self.state.default_namespace.to_string();
        }

        let rendered =
            render_resources(&spec).map_err(|err| Status::invalid_argument(err.to_string()))?;

        let record = repository::insert_job(&self.state.db, &spec.namespace, &spec.name, &spec)
            .await
            .map_err(|e| internal("persist ingest_job", e))?;

        runtime_state::upsert_job_runtime_state(
            &self.state.kube,
            &record,
            &IngestJobRuntimeState::pending(),
        )
        .await
        .map_err(|e| internal("persist runtime state", e))?;

        if let Err(err) = apply_resources(&self.state.kube, &rendered).await {
            let _ = runtime_state::upsert_job_runtime_state(
                &self.state.kube,
                &record,
                &IngestJobRuntimeState::failed(err.to_string()),
            )
            .await;
            return Err(internal("apply kubernetes resources", err));
        }

        let kc_name = rendered
            .kafka_connector
            .metadata
            .name
            .clone()
            .unwrap_or_default();
        let fl_name = rendered
            .flink_deployment
            .as_ref()
            .and_then(|f| f.metadata.name.clone());

        repository::mark_materialized(&self.state.db, record.id, &kc_name, fl_name.as_deref())
            .await
            .map_err(|e| internal("mark_materialized", e))?;

        runtime_state::upsert_job_runtime_state(
            &self.state.kube,
            &record,
            &IngestJobRuntimeState::materialized(&kc_name, fl_name.as_deref()),
        )
        .await
        .map_err(|e| internal("persist runtime state", e))?;

        let updated = repository::get_job(&self.state.db, record.id)
            .await
            .map_err(|e| internal("get_job", e))?
            .ok_or_else(|| Status::internal("job vanished after insert"))?;
        let response = runtime_state::hydrate_job(&self.state.kube, updated)
            .await
            .map_err(|e| internal("hydrate runtime state", e))?;
        Ok(Response::new(response))
    }

    async fn get_ingest_job(
        &self,
        request: Request<GetIngestJobRequest>,
    ) -> Result<Response<IngestJob>, Status> {
        let id = parse_id(&request.into_inner().id)?;
        let record = repository::get_job(&self.state.db, id)
            .await
            .map_err(|e| internal("get_job", e))?
            .ok_or_else(|| Status::not_found("ingest job not found"))?;
        let response = runtime_state::hydrate_job(&self.state.kube, record)
            .await
            .map_err(|e| internal("hydrate runtime state", e))?;
        Ok(Response::new(response))
    }

    async fn list_ingest_jobs(
        &self,
        _request: Request<ListIngestJobsRequest>,
    ) -> Result<Response<ListIngestJobsResponse>, Status> {
        let rows = repository::list_jobs(&self.state.db)
            .await
            .map_err(|e| internal("list_jobs", e))?;
        let mut jobs = Vec::with_capacity(rows.len());
        for row in rows {
            jobs.push(
                runtime_state::hydrate_job(&self.state.kube, row)
                    .await
                    .map_err(|e| internal("hydrate runtime state", e))?,
            );
        }
        Ok(Response::new(ListIngestJobsResponse { jobs }))
    }

    async fn delete_ingest_job(
        &self,
        request: Request<DeleteIngestJobRequest>,
    ) -> Result<Response<DeleteIngestJobResponse>, Status> {
        let id = parse_id(&request.into_inner().id)?;
        let deleted = repository::delete_job(&self.state.db, id)
            .await
            .map_err(|e| internal("delete_job", e))?;
        let Some(record) = deleted else {
            return Err(Status::not_found("ingest job not found"));
        };
        let _ =
            runtime_state::delete_job_runtime_state(&self.state.kube, &record.namespace, id).await;
        delete_resources(
            &self.state.kube,
            &record.namespace,
            record.kafka_connector_name.as_deref(),
            record.flink_deployment_name.as_deref(),
        )
        .await
        .map_err(|e| internal("delete kubernetes resources", e))?;
        Ok(Response::new(DeleteIngestJobResponse {}))
    }
}
