//! Tonic gRPC implementation of `MediaSetService`.
//!
//! Each RPC delegates to the same `*_op` functions used by the REST
//! handlers in `crate::handlers`, so behaviour stays in lockstep without
//! a second copy of the SQL.
//!
//! `AccessPatternService` is intentionally *not* registered here: the
//! H1 task list scopes the runtime work to MediaSet + items +
//! transactions; access-pattern execution lands in a follow-up.

use std::str::FromStr;

use chrono::{DateTime, Utc};
use prost_types::Timestamp;
use tonic::{Request, Response, Status};

use crate::AppState;
use crate::domain::error::MediaError;
use crate::handlers::items::{
    delete_item_op, get_item_op, list_items_op, presigned_download_op, presigned_upload_op,
};
use crate::handlers::media_sets::{
    create_media_set_op, delete_media_set_op, get_media_set_op, list_media_sets_op,
};
use crate::handlers::transactions::{close_transaction_op, open_transaction_op};
use audit_trail::events::AuditContext;

/// Context for audit emissions originating from the gRPC surface. The
/// real per-call principal is wired in once the gRPC layer learns to
/// extract claims from metadata; for now every gRPC mutation logs as
/// the synthetic `grpc` actor with a fresh request id so the outbox
/// row is still unique per call.
fn grpc_ctx() -> AuditContext {
    AuditContext::for_actor("grpc")
        .with_request_id(uuid::Uuid::now_v7().to_string())
        .with_source_service("media-sets-service")
}
use crate::models::{
    CreateMediaSetRequest as CreateReq, MediaItem, MediaSet, MediaSetSchema, MediaSetTransaction,
    PresignedUploadRequest, TransactionPolicy, TransactionState,
};
use crate::proto::common::{PageRequest, PageResponse};
use crate::proto::media_set::{
    self as pb,
    media_set_service_server::{MediaSetService, MediaSetServiceServer},
};

#[derive(Clone)]
pub struct MediaSetGrpcService {
    state: AppState,
}

impl MediaSetGrpcService {
    pub fn new(state: AppState) -> Self {
        Self { state }
    }

    pub fn into_server(self) -> MediaSetServiceServer<Self> {
        MediaSetServiceServer::new(self)
    }
}

#[tonic::async_trait]
impl MediaSetService for MediaSetGrpcService {
    async fn create_media_set(
        &self,
        request: Request<pb::CreateMediaSetRequest>,
    ) -> Result<Response<pb::MediaSet>, Status> {
        let r = request.into_inner();
        let req = CreateReq {
            name: r.name,
            project_rid: r.project_rid,
            schema: schema_from_proto(r.schema)?,
            allowed_mime_types: r.allowed_mime_types,
            transaction_policy: policy_from_proto(r.transaction_policy)?,
            retention_seconds: r.retention_seconds,
            virtual_: r.r#virtual,
            source_rid: r.source_rid,
            markings: r.markings,
        };
        let row = create_media_set_op(&self.state, req, "grpc", &grpc_ctx())
            .await
            .map_err(to_status)?;
        Ok(Response::new(media_set_to_proto(&row)))
    }

    async fn get_media_set(
        &self,
        request: Request<pb::GetMediaSetRequest>,
    ) -> Result<Response<pb::MediaSet>, Status> {
        let row = get_media_set_op(&self.state, &request.into_inner().rid)
            .await
            .map_err(to_status)?;
        Ok(Response::new(media_set_to_proto(&row)))
    }

    async fn list_media_sets(
        &self,
        request: Request<pb::ListMediaSetsRequest>,
    ) -> Result<Response<pb::ListMediaSetsResponse>, Status> {
        let r = request.into_inner();
        let (page, per_page) = page_args(r.pagination.as_ref());
        let offset = ((page - 1).max(0) as i64) * (per_page as i64);
        let rows = list_media_sets_op(
            &self.state,
            Some(r.project_rid.as_str()).filter(|s| !s.is_empty()),
            per_page as i64,
            offset,
        )
        .await
        .map_err(to_status)?;
        Ok(Response::new(pb::ListMediaSetsResponse {
            media_sets: rows.iter().map(media_set_to_proto).collect(),
            pagination: Some(PageResponse {
                page,
                per_page,
                total: rows.len() as i64,
                total_pages: 1,
            }),
        }))
    }

    async fn delete_media_set(
        &self,
        request: Request<pb::DeleteMediaSetRequest>,
    ) -> Result<Response<pb::DeleteMediaSetResponse>, Status> {
        let rid = request.into_inner().rid;
        let row = get_media_set_op(&self.state, &rid).await.map_err(to_status)?;
        delete_media_set_op(&self.state, &rid, &row, &grpc_ctx())
            .await
            .map_err(to_status)?;
        Ok(Response::new(pb::DeleteMediaSetResponse {}))
    }

    async fn open_transaction(
        &self,
        request: Request<pb::OpenTransactionRequest>,
    ) -> Result<Response<pb::Transaction>, Status> {
        let r = request.into_inner();
        let branch = if r.branch.is_empty() { "main".into() } else { r.branch };
        let row = open_transaction_op(&self.state, &r.media_set_rid, &branch, "grpc", &grpc_ctx())
            .await
            .map_err(to_status)?;
        Ok(Response::new(transaction_to_proto(&row)))
    }

    async fn commit_transaction(
        &self,
        request: Request<pb::CommitTransactionRequest>,
    ) -> Result<Response<pb::Transaction>, Status> {
        let row = close_transaction_op(
            &self.state,
            &request.into_inner().transaction_rid,
            TransactionState::Committed,
            &grpc_ctx(),
        )
        .await
        .map_err(to_status)?;
        Ok(Response::new(transaction_to_proto(&row)))
    }

    async fn abort_transaction(
        &self,
        request: Request<pb::AbortTransactionRequest>,
    ) -> Result<Response<pb::Transaction>, Status> {
        let row = close_transaction_op(
            &self.state,
            &request.into_inner().transaction_rid,
            TransactionState::Aborted,
            &grpc_ctx(),
        )
        .await
        .map_err(to_status)?;
        Ok(Response::new(transaction_to_proto(&row)))
    }

    async fn list_media_items(
        &self,
        request: Request<pb::ListMediaItemsRequest>,
    ) -> Result<Response<pb::ListMediaItemsResponse>, Status> {
        let r = request.into_inner();
        let branch = if r.branch.is_empty() { "main".into() } else { r.branch };
        let (page, per_page) = page_args(r.pagination.as_ref());
        let rows = list_items_op(
            &self.state,
            &r.media_set_rid,
            &branch,
            Some(r.path_prefix.as_str()).filter(|s| !s.is_empty()),
            per_page as i64,
            None,
        )
        .await
        .map_err(to_status)?;
        Ok(Response::new(pb::ListMediaItemsResponse {
            items: rows.iter().map(media_item_to_proto).collect(),
            pagination: Some(PageResponse {
                page,
                per_page,
                total: rows.len() as i64,
                total_pages: 1,
            }),
        }))
    }

    async fn get_media_item(
        &self,
        request: Request<pb::GetMediaItemRequest>,
    ) -> Result<Response<pb::MediaItem>, Status> {
        let row = get_item_op(&self.state, &request.into_inner().rid)
            .await
            .map_err(to_status)?;
        Ok(Response::new(media_item_to_proto(&row)))
    }

    async fn delete_media_item(
        &self,
        request: Request<pb::DeleteMediaItemRequest>,
    ) -> Result<Response<pb::DeleteMediaItemResponse>, Status> {
        delete_item_op(&self.state, &request.into_inner().rid, &grpc_ctx())
            .await
            .map_err(to_status)?;
        Ok(Response::new(pb::DeleteMediaItemResponse {}))
    }

    async fn generate_presigned_upload_url(
        &self,
        request: Request<pb::GeneratePresignedUploadUrlRequest>,
    ) -> Result<Response<pb::PresignedUrlResponse>, Status> {
        let r = request.into_inner();
        let req = PresignedUploadRequest {
            path: r.path,
            mime_type: r.mime_type,
            branch: Some(if r.branch.is_empty() { "main".into() } else { r.branch }),
            transaction_rid: Some(r.transaction_rid).filter(|s| !s.is_empty()),
            sha256: None,
            size_bytes: None,
            expires_in_seconds: Some(if r.expires_in_seconds <= 0 {
                self.state.presign_ttl_seconds
            } else {
                r.expires_in_seconds as u64
            }),
        };
        let (_item, url) = presigned_upload_op(&self.state, &r.media_set_rid, req, &grpc_ctx())
            .await
            .map_err(to_status)?;
        Ok(Response::new(presigned_to_proto(&url)))
    }

    async fn generate_presigned_download_url(
        &self,
        request: Request<pb::GeneratePresignedDownloadUrlRequest>,
    ) -> Result<Response<pb::PresignedUrlResponse>, Status> {
        let r = request.into_inner();
        let ttl = if r.expires_in_seconds <= 0 {
            None
        } else {
            Some(r.expires_in_seconds as u64)
        };
        let (_item, url) = presigned_download_op(&self.state, &r.media_item_rid, ttl, &grpc_ctx())
            .await
            .map_err(to_status)?;
        Ok(Response::new(presigned_to_proto(&url)))
    }

    async fn register_virtual_media_item(
        &self,
        _request: Request<pb::RegisterVirtualMediaItemRequest>,
    ) -> Result<Response<pb::MediaItem>, Status> {
        // Virtual media set ingestion lives in a follow-up: the H1 cut
        // surfaces the contract so the catalog can target it, but the
        // actual external-source registration depends on connector
        // wiring that is not in scope here.
        Err(Status::unimplemented(
            "virtual media item registration ships in a follow-up",
        ))
    }
}

// ---------------------------------------------------------------------------
// Conversions: domain row ↔ proto message
// ---------------------------------------------------------------------------

fn page_args(req: Option<&PageRequest>) -> (i32, i32) {
    let page = req.map(|p| p.page).unwrap_or(1).max(1);
    let per_page = req.map(|p| p.per_page).filter(|n| *n > 0).unwrap_or(50);
    (page, per_page)
}

fn schema_from_proto(value: i32) -> Result<MediaSetSchema, Status> {
    match pb::MediaSetSchema::try_from(value).unwrap_or(pb::MediaSetSchema::Unspecified) {
        pb::MediaSetSchema::Image => Ok(MediaSetSchema::Image),
        pb::MediaSetSchema::Audio => Ok(MediaSetSchema::Audio),
        pb::MediaSetSchema::Video => Ok(MediaSetSchema::Video),
        pb::MediaSetSchema::Document => Ok(MediaSetSchema::Document),
        pb::MediaSetSchema::Spreadsheet => Ok(MediaSetSchema::Spreadsheet),
        pb::MediaSetSchema::Email => Ok(MediaSetSchema::Email),
        pb::MediaSetSchema::Unspecified => Err(Status::invalid_argument("schema is required")),
    }
}

fn policy_from_proto(value: i32) -> Result<TransactionPolicy, Status> {
    match pb::TransactionPolicy::try_from(value).unwrap_or(pb::TransactionPolicy::Unspecified) {
        pb::TransactionPolicy::Transactional => Ok(TransactionPolicy::Transactional),
        // The Foundry default is transactionless; treat UNSPECIFIED as
        // such so callers don't have to set it explicitly.
        pb::TransactionPolicy::Transactionless | pb::TransactionPolicy::Unspecified => {
            Ok(TransactionPolicy::Transactionless)
        }
    }
}

fn media_set_to_proto(row: &MediaSet) -> pb::MediaSet {
    pb::MediaSet {
        rid: row.rid.clone(),
        name: row.name.clone(),
        project_rid: row.project_rid.clone(),
        schema: schema_str_to_proto(&row.schema) as i32,
        allowed_mime_types: row.allowed_mime_types.clone(),
        transaction_policy: policy_str_to_proto(&row.transaction_policy) as i32,
        retention_seconds: row.retention_seconds,
        r#virtual: row.virtual_,
        source_rid: row.source_rid.clone(),
        markings: row.markings.clone(),
        created_at: Some(timestamp(row.created_at)),
        created_by: row.created_by.clone(),
    }
}

fn media_item_to_proto(row: &MediaItem) -> pb::MediaItem {
    pb::MediaItem {
        rid: row.rid.clone(),
        media_set_rid: row.media_set_rid.clone(),
        branch: row.branch.clone(),
        transaction_rid: row.transaction_rid.clone(),
        path: row.path.clone(),
        mime_type: row.mime_type.clone(),
        size_bytes: row.size_bytes,
        sha256: row.sha256.clone(),
        metadata: row.metadata.to_string(),
        created_at: Some(timestamp(row.created_at)),
        deduplicated_from: row.deduplicated_from.clone(),
    }
}

fn transaction_to_proto(row: &MediaSetTransaction) -> pb::Transaction {
    let state = TransactionState::from_str(&row.state).unwrap_or(TransactionState::Open);
    pb::Transaction {
        rid: row.rid.clone(),
        media_set_rid: row.media_set_rid.clone(),
        branch: row.branch.clone(),
        state: txn_state_to_proto(state) as i32,
        opened_at: Some(timestamp(row.opened_at)),
        closed_at: row.closed_at.map(timestamp),
        opened_by: row.opened_by.clone(),
    }
}

fn presigned_to_proto(url: &crate::domain::PresignedUrl) -> pb::PresignedUrlResponse {
    pb::PresignedUrlResponse {
        url: url.url.clone(),
        headers: url.headers.iter().cloned().collect(),
        expires_at: Some(timestamp(url.expires_at)),
    }
}

fn schema_str_to_proto(s: &str) -> pb::MediaSetSchema {
    match s {
        "IMAGE" => pb::MediaSetSchema::Image,
        "AUDIO" => pb::MediaSetSchema::Audio,
        "VIDEO" => pb::MediaSetSchema::Video,
        "DOCUMENT" => pb::MediaSetSchema::Document,
        "SPREADSHEET" => pb::MediaSetSchema::Spreadsheet,
        "EMAIL" => pb::MediaSetSchema::Email,
        _ => pb::MediaSetSchema::Unspecified,
    }
}

fn policy_str_to_proto(s: &str) -> pb::TransactionPolicy {
    match s {
        "TRANSACTIONAL" => pb::TransactionPolicy::Transactional,
        "TRANSACTIONLESS" => pb::TransactionPolicy::Transactionless,
        _ => pb::TransactionPolicy::Unspecified,
    }
}

fn txn_state_to_proto(s: TransactionState) -> pb::TransactionState {
    match s {
        TransactionState::Open => pb::TransactionState::Open,
        TransactionState::Committed => pb::TransactionState::Committed,
        TransactionState::Aborted => pb::TransactionState::Aborted,
    }
}

fn timestamp(ts: DateTime<Utc>) -> Timestamp {
    Timestamp {
        seconds: ts.timestamp(),
        nanos: ts.timestamp_subsec_nanos() as i32,
    }
}

fn to_status(err: MediaError) -> Status {
    match err {
        MediaError::MediaSetNotFound(rid)
        | MediaError::MediaItemNotFound(rid)
        | MediaError::TransactionNotFound(rid) => Status::not_found(rid),
        MediaError::Transactionless(rid) => Status::failed_precondition(format!(
            "media set `{rid}` is transactionless; transactions are not allowed"
        )),
        MediaError::TransactionTerminal(rid, st) => Status::failed_precondition(format!(
            "transaction `{rid}` is in terminal state `{st}`"
        )),
        MediaError::BadRequest(msg) => Status::invalid_argument(msg),
        MediaError::Forbidden(msg) => Status::permission_denied(msg),
        MediaError::Authz(msg) => {
            tracing::error!(error = %msg, "grpc authz error");
            Status::internal("authz error")
        }
        MediaError::Storage(msg) | MediaError::UpstreamUnavailable(msg) => {
            Status::unavailable(msg)
        }
        MediaError::Database(err) => {
            tracing::error!(error = %err, "grpc database error");
            Status::internal("database error")
        }
        MediaError::Outbox(msg) => {
            tracing::error!(error = %msg, "grpc audit outbox error");
            Status::internal("audit outbox error")
        }
    }
}

/// Convenience wrapper used by `main.rs` to mount the gRPC server.
pub fn build_server(state: AppState) -> MediaSetServiceServer<MediaSetGrpcService> {
    MediaSetGrpcService::new(state).into_server()
}
