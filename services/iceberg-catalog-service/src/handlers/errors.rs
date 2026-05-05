//! Error type that all REST Catalog handlers return. The Iceberg spec
//! requires a specific JSON error envelope: `{"error": {"message", "type", "code"}}`
//! (see `rest-catalog-open-api.yaml` § ErrorModel). We mirror the shape so
//! external clients (PyIceberg, Trino, …) can parse it directly.

use axum::Json;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use serde::Serialize;

use crate::domain::foundry_transaction::TxnError;
use crate::domain::schema_strict::SchemaDiff;
use crate::domain::{namespace::NamespaceError, snapshot::SnapshotError, table::TableError};

#[derive(Debug, thiserror::Error)]
pub enum ApiError {
    #[error("bad request: {0}")]
    BadRequest(String),
    #[error("unauthenticated")]
    Unauthenticated,
    #[error("forbidden: {0}")]
    Forbidden(String),
    #[error("not found: {0}")]
    NotFound(String),
    #[error("conflict: {0}")]
    Conflict(String),
    /// 422 — schema strict-mode rejection. Carries the diff envelope
    /// so PyIceberg / pipeline-authoring can surface a generated
    /// `ALTER TABLE` migration to the user.
    #[error("schema incompatible (requires explicit ALTER TABLE)")]
    SchemaIncompatible {
        current_schema: serde_json::Value,
        attempted_schema: serde_json::Value,
        diff: SchemaDiff,
    },
    /// 409 with conflict-source metadata. Pipeline-build-service
    /// translates this into a job retry per the doc § "Job queuing
    /// and optimistic concurrency".
    #[error("retryable conflict on `{table_rid}`: {reason}")]
    Retryable {
        table_rid: String,
        reason: String,
        conflicting_with: String,
    },
    #[error("internal error: {0}")]
    Internal(String),
}

#[derive(Debug, Serialize)]
struct ErrorEnvelope<'a> {
    error: ErrorBody<'a>,
}

#[derive(Debug, Serialize)]
struct ErrorBody<'a> {
    message: &'a str,
    #[serde(rename = "type")]
    kind: &'a str,
    code: u16,
}

impl IntoResponse for ApiError {
    fn into_response(self) -> Response {
        match &self {
            ApiError::SchemaIncompatible {
                current_schema,
                attempted_schema,
                diff,
            } => {
                let message = self.to_string();
                let body = serde_json::json!({
                    "error": {
                        "message": message,
                        "type": "SCHEMA_INCOMPATIBLE_REQUIRES_ALTER",
                        "code": StatusCode::UNPROCESSABLE_ENTITY.as_u16(),
                        "current_schema": current_schema,
                        "attempted_schema": attempted_schema,
                        "diff": diff,
                    }
                });
                (StatusCode::UNPROCESSABLE_ENTITY, Json(body)).into_response()
            }
            ApiError::Retryable {
                table_rid,
                reason,
                conflicting_with,
            } => {
                let body = serde_json::json!({
                    "error": {
                        "message": format!("retryable conflict on {table_rid}: {reason}"),
                        "type": "CONFLICTING_CONCURRENT_UPDATE",
                        "code": StatusCode::CONFLICT.as_u16(),
                        "table_rid": table_rid,
                        "conflicting_with": conflicting_with,
                    }
                });
                (StatusCode::CONFLICT, Json(body)).into_response()
            }
            _ => {
                let (status, kind) = match &self {
                    ApiError::BadRequest(_) => (StatusCode::BAD_REQUEST, "BadRequestException"),
                    ApiError::Unauthenticated => (StatusCode::UNAUTHORIZED, "AuthenticationException"),
                    ApiError::Forbidden(_) => (StatusCode::FORBIDDEN, "ForbiddenException"),
                    ApiError::NotFound(_) => (StatusCode::NOT_FOUND, "NoSuchTableException"),
                    ApiError::Conflict(_) => (StatusCode::CONFLICT, "AlreadyExistsException"),
                    ApiError::Internal(_) => (
                        StatusCode::INTERNAL_SERVER_ERROR,
                        "InternalServerException",
                    ),
                    ApiError::SchemaIncompatible { .. } | ApiError::Retryable { .. } => {
                        // handled above
                        unreachable!()
                    }
                };
                let message = self.to_string();
                let body = ErrorEnvelope {
                    error: ErrorBody {
                        message: &message,
                        kind,
                        code: status.as_u16(),
                    },
                };
                (status, Json(body)).into_response()
            }
        }
    }
}

impl From<NamespaceError> for ApiError {
    fn from(e: NamespaceError) -> Self {
        match e {
            NamespaceError::AlreadyExists(_) => ApiError::Conflict(e.to_string()),
            NamespaceError::NotExists(_) => ApiError::NotFound(e.to_string()),
            NamespaceError::NotEmpty(_) => ApiError::Conflict(e.to_string()),
            NamespaceError::Database(err) => ApiError::Internal(err.to_string()),
        }
    }
}

impl From<TableError> for ApiError {
    fn from(e: TableError) -> Self {
        match e {
            TableError::AlreadyExists(_) => ApiError::Conflict(e.to_string()),
            TableError::NotFound(_) => ApiError::NotFound(e.to_string()),
            TableError::InvalidFormatVersion(_) | TableError::SchemaMissing => {
                ApiError::BadRequest(e.to_string())
            }
            TableError::RequirementsFailed(_) => ApiError::Conflict(e.to_string()),
            TableError::Namespace(ns) => ApiError::from(ns),
            TableError::Database(err) => ApiError::Internal(err.to_string()),
        }
    }
}

impl From<SnapshotError> for ApiError {
    fn from(e: SnapshotError) -> Self {
        match e {
            SnapshotError::InvalidOperation(_) => ApiError::BadRequest(e.to_string()),
            SnapshotError::Database(err) => ApiError::Internal(err.to_string()),
        }
    }
}

impl From<sqlx::Error> for ApiError {
    fn from(err: sqlx::Error) -> Self {
        ApiError::Internal(err.to_string())
    }
}

impl From<TxnError> for ApiError {
    fn from(err: TxnError) -> Self {
        match err {
            TxnError::Closed => ApiError::BadRequest("transaction already closed".to_string()),
            TxnError::UnknownTable(rid) => ApiError::NotFound(format!("unknown table `{rid}`")),
            TxnError::SchemaIncompatible { table_rid, diff } => ApiError::BadRequest(format!(
                "schema strict-mode for `{table_rid}`: {diff}"
            )),
            TxnError::Retryable {
                table_rid,
                reason,
                conflicting_with,
            } => ApiError::Retryable {
                table_rid,
                reason,
                conflicting_with: conflicting_with.as_str().to_string(),
            },
            TxnError::Catalog(msg) => ApiError::Internal(msg),
            TxnError::Other(msg) => ApiError::Internal(msg),
        }
    }
}
