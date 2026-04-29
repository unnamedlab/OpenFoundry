//! gRPC `IngestJobService` implementation backed by the `sync_jobs` Postgres table.

use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use prost_types::Timestamp;
use tokio_stream::Stream;
use tonic::{Request, Response, Status};
use uuid::Uuid;

use crate::AppState;
use crate::models::sync_job::SyncJob;
use crate::open_foundry::data_integration::{
    CancelIngestJobRequest, CancelIngestJobResponse, GetIngestJobRequest, IngestJob,
    IngestJobStatus, ListIngestJobsRequest, ListIngestJobsResponse, QueueIngestJobRequest,
    WatchIngestJobRequest,
    ingest_job_service_server::IngestJobService,
};

/// Tonic service implementation for [`IngestJobService`].
#[derive(Clone)]
pub struct IngestJobServiceImpl {
    state: Arc<AppState>,
}

impl IngestJobServiceImpl {
    pub fn new(state: Arc<AppState>) -> Self {
        Self { state }
    }
}

// ── helpers ──────────────────────────────────────────────────────────────────

fn dt_to_proto(dt: &chrono::DateTime<Utc>) -> Timestamp {
    Timestamp {
        seconds: dt.timestamp(),
        nanos: dt.timestamp_subsec_nanos() as i32,
    }
}

fn opt_dt_to_proto(dt: Option<&chrono::DateTime<Utc>>) -> Option<Timestamp> {
    dt.map(dt_to_proto)
}

fn status_str_to_proto(s: &str) -> i32 {
    match s {
        "pending" => IngestJobStatus::Pending as i32,
        "running" => IngestJobStatus::Running as i32,
        "retrying" => IngestJobStatus::Retrying as i32,
        "completed" => IngestJobStatus::Completed as i32,
        "failed" => IngestJobStatus::Failed as i32,
        "cancelled" => IngestJobStatus::Cancelled as i32,
        _ => IngestJobStatus::Unspecified as i32,
    }
}

fn uuid_msg(u: Uuid) -> crate::open_foundry::common::Uuid {
    crate::open_foundry::common::Uuid {
        value: u.to_string(),
    }
}

fn sync_job_to_proto(job: &SyncJob) -> IngestJob {
    IngestJob {
        id: Some(uuid_msg(job.id)),
        connection_id: Some(uuid_msg(job.connection_id)),
        target_dataset_id: job.target_dataset_id.map(uuid_msg),
        table_name: job.table_name.clone(),
        status: status_str_to_proto(&job.status),
        rows_synced: job.rows_synced,
        error: job.error.clone().unwrap_or_default(),
        attempts: job.attempts,
        max_attempts: job.max_attempts,
        scheduled_at: Some(dt_to_proto(&job.scheduled_at)),
        next_retry_at: opt_dt_to_proto(job.next_retry_at.as_ref()),
        started_at: opt_dt_to_proto(job.started_at.as_ref()),
        completed_at: opt_dt_to_proto(job.completed_at.as_ref()),
        created_at: Some(dt_to_proto(&job.created_at)),
    }
}

fn is_terminal(status: &str) -> bool {
    matches!(status, "completed" | "failed" | "cancelled")
}

// ── RPC implementations ───────────────────────────────────────────────────────

#[tonic::async_trait]
impl IngestJobService for IngestJobServiceImpl {
    async fn queue_ingest_job(
        &self,
        request: Request<QueueIngestJobRequest>,
    ) -> Result<Response<IngestJob>, Status> {
        let req = request.into_inner();
        let connection_id: Uuid = req
            .connection_id
            .as_ref()
            .ok_or_else(|| Status::invalid_argument("connection_id is required"))?
            .value
            .parse()
            .map_err(|_| Status::invalid_argument("invalid connection_id UUID"))?;

        let target_dataset_id: Option<Uuid> = req
            .target_dataset_id
            .as_ref()
            .map(|u| u.value.parse::<Uuid>())
            .transpose()
            .map_err(|_| Status::invalid_argument("invalid target_dataset_id UUID"))?;

        let scheduled_at = req
            .schedule_at
            .as_ref()
            .and_then(|ts| chrono::DateTime::from_timestamp(ts.seconds, ts.nanos as u32))
            .unwrap_or_else(Utc::now);

        let max_attempts = if req.max_attempts == 0 {
            3
        } else {
            req.max_attempts.clamp(1, 10)
        };

        // Verify connection exists.
        let exists: bool =
            sqlx::query_scalar("SELECT EXISTS(SELECT 1 FROM connections WHERE id = $1)")
                .bind(connection_id)
                .fetch_one(&self.state.db)
                .await
                .map_err(|e| Status::internal(e.to_string()))?;

        if !exists {
            return Err(Status::not_found("connection not found"));
        }

        let job = sqlx::query_as::<_, SyncJob>(
            r#"INSERT INTO sync_jobs
                   (id, connection_id, target_dataset_id, table_name, status,
                    scheduled_at, max_attempts, sync_metadata)
               VALUES ($1, $2, $3, $4, 'pending', $5, $6, '{}'::jsonb)
               RETURNING *"#,
        )
        .bind(Uuid::now_v7())
        .bind(connection_id)
        .bind(target_dataset_id)
        .bind(&req.table_name)
        .bind(scheduled_at)
        .bind(max_attempts)
        .fetch_one(&self.state.db)
        .await
        .map_err(|e| Status::internal(e.to_string()))?;

        // Kick the scheduler so the job is picked up quickly.
        let state = Arc::clone(&self.state);
        tokio::spawn(async move {
            if let Err(e) = crate::domain::scheduler::tick(&state).await {
                tracing::warn!("post-queue scheduler tick failed: {e}");
            }
        });

        tracing::info!(
            job_id = %job.id,
            connection_id = %connection_id,
            "ingest job queued via gRPC"
        );

        Ok(Response::new(sync_job_to_proto(&job)))
    }

    async fn get_ingest_job(
        &self,
        request: Request<GetIngestJobRequest>,
    ) -> Result<Response<IngestJob>, Status> {
        let id: Uuid = request
            .into_inner()
            .id
            .ok_or_else(|| Status::invalid_argument("id is required"))?
            .value
            .parse()
            .map_err(|_| Status::invalid_argument("invalid job UUID"))?;

        let job = sqlx::query_as::<_, SyncJob>("SELECT * FROM sync_jobs WHERE id = $1")
            .bind(id)
            .fetch_optional(&self.state.db)
            .await
            .map_err(|e| Status::internal(e.to_string()))?
            .ok_or_else(|| Status::not_found("ingest job not found"))?;

        Ok(Response::new(sync_job_to_proto(&job)))
    }

    async fn list_ingest_jobs(
        &self,
        request: Request<ListIngestJobsRequest>,
    ) -> Result<Response<ListIngestJobsResponse>, Status> {
        let req = request.into_inner();
        let connection_id: Uuid = req
            .connection_id
            .ok_or_else(|| Status::invalid_argument("connection_id is required"))?
            .value
            .parse()
            .map_err(|_| Status::invalid_argument("invalid connection_id UUID"))?;

        let page = req
            .pagination
            .as_ref()
            .map(|p| p.page.max(1))
            .unwrap_or(1) as i64;
        let per_page = req
            .pagination
            .as_ref()
            .map(|p| p.per_page.clamp(1, 100))
            .unwrap_or(50) as i64;
        let offset = (page - 1) * per_page;

        let jobs = sqlx::query_as::<_, SyncJob>(
            "SELECT * FROM sync_jobs WHERE connection_id = $1 \
             ORDER BY created_at DESC LIMIT $2 OFFSET $3",
        )
        .bind(connection_id)
        .bind(per_page)
        .bind(offset)
        .fetch_all(&self.state.db)
        .await
        .map_err(|e| Status::internal(e.to_string()))?;

        let total: i64 =
            sqlx::query_scalar("SELECT COUNT(*) FROM sync_jobs WHERE connection_id = $1")
                .bind(connection_id)
                .fetch_one(&self.state.db)
                .await
                .map_err(|e| Status::internal(e.to_string()))?;

        let total_pages = ((total + per_page - 1) / per_page) as i32;

        let pagination = crate::open_foundry::common::PageResponse {
            page: page as i32,
            per_page: per_page as i32,
            total,
            total_pages,
        };

        Ok(Response::new(ListIngestJobsResponse {
            jobs: jobs.iter().map(sync_job_to_proto).collect(),
            pagination: Some(pagination),
        }))
    }

    async fn cancel_ingest_job(
        &self,
        request: Request<CancelIngestJobRequest>,
    ) -> Result<Response<CancelIngestJobResponse>, Status> {
        let id: Uuid = request
            .into_inner()
            .id
            .ok_or_else(|| Status::invalid_argument("id is required"))?
            .value
            .parse()
            .map_err(|_| Status::invalid_argument("invalid job UUID"))?;

        let rows_affected = sqlx::query(
            "UPDATE sync_jobs SET status = 'cancelled', completed_at = NOW() \
             WHERE id = $1 AND status IN ('pending', 'retrying')",
        )
        .bind(id)
        .execute(&self.state.db)
        .await
        .map_err(|e| Status::internal(e.to_string()))?
        .rows_affected();

        if rows_affected == 0 {
            return Err(Status::not_found(
                "ingest job not found or already in a terminal state",
            ));
        }

        tracing::info!(job_id = %id, "ingest job cancelled via gRPC");

        Ok(Response::new(CancelIngestJobResponse {}))
    }

    type WatchIngestJobStream =
        Pin<Box<dyn Stream<Item = Result<IngestJob, Status>> + Send + 'static>>;

    async fn watch_ingest_job(
        &self,
        request: Request<WatchIngestJobRequest>,
    ) -> Result<Response<Self::WatchIngestJobStream>, Status> {
        let req = request.into_inner();
        let id: Uuid = req
            .id
            .ok_or_else(|| Status::invalid_argument("id is required"))?
            .value
            .parse()
            .map_err(|_| Status::invalid_argument("invalid job UUID"))?;

        let poll_ms = if req.poll_interval_ms <= 0 {
            500u64
        } else {
            req.poll_interval_ms.clamp(100, 10_000) as u64
        };

        let db = self.state.db.clone();

        let stream = async_stream::try_stream! {
            loop {
                let job = sqlx::query_as::<_, SyncJob>("SELECT * FROM sync_jobs WHERE id = $1")
                    .bind(id)
                    .fetch_optional(&db)
                    .await
                    .map_err(|e| Status::internal(e.to_string()))?;

                match job {
                    None => {
                        return Err(Status::not_found("ingest job not found"))?;
                    }
                    Some(j) => {
                        let terminal = is_terminal(&j.status);
                        yield sync_job_to_proto(&j);
                        if terminal {
                            break;
                        }
                    }
                }

                tokio::time::sleep(Duration::from_millis(poll_ms)).await;
            }
        };

        Ok(Response::new(Box::pin(stream)))
    }
}
