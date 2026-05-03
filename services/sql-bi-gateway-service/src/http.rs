//! HTTP side router (port = `healthz_port`).
//!
//! The Flight SQL gRPC server on `port` is the gateway's primary surface;
//! this small axum router exposes:
//!
//! * `GET  /healthz` — liveness probe used by Kubernetes.
//! * `POST /api/v1/queries/saved` — create a saved query.
//! * `GET  /api/v1/queries/saved` — list saved queries (paginated).
//! * `DELETE /api/v1/queries/saved/:id` — delete a saved query.
//!
//! Saved queries are stored in the per-bounded-context CNPG cluster; the
//! schema lives in `migrations/20260419100003_initial_queries.sql` and is
//! provisioned out of band by the umbrella Helm chart (see
//! `services/sql-bi-gateway-service/k8s/README.md`).

use std::sync::Arc;

use axum::{
    Json, Router,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{delete, get, post},
};
use sqlx::PgPool;
use uuid::Uuid;

use crate::models::{
    CreateSavedQueryParams, CreateSavedQueryRequest, ListQueriesQuery, SavedQuery,
};

/// Axum state shared by every saved-queries handler.
#[derive(Clone)]
pub struct AppState {
    pub db: Arc<PgPool>,
}

/// Build the side router. `db` is `None` in environments where the
/// saved-queries Postgres cluster is not provisioned (smoke clusters,
/// integration tests of the Flight SQL surface in isolation), in which
/// case only `/healthz` is exposed.
pub fn build_router(db: Option<Arc<PgPool>>) -> Router {
    let base = Router::new()
        .route("/healthz", get(healthz))
        .route("/health", get(healthz));

    match db {
        None => base,
        Some(db) => {
            let state = AppState { db };
            let stateful: Router = Router::new()
                .route("/api/v1/queries/saved", post(create_saved_query))
                .route("/api/v1/queries/saved", get(list_saved_queries))
                .route("/api/v1/queries/saved/:id", delete(delete_saved_query))
                .with_state(state);
            base.merge(stateful)
        }
    }
}

async fn healthz() -> (StatusCode, &'static str) {
    (StatusCode::OK, "ok")
}

/// Conservative dataset-RID safety check used by the
/// `?seed_dataset_rid=` autofill path. Foundry RIDs are
/// `ri.foundry.main.dataset.<uuid>`; we accept any string composed
/// solely of alphanumerics + `._-` so the embed in SQL is safe even
/// if the caller passes a non-canonical RID. Anything else falls
/// through (the SQL stays empty).
fn is_safe_rid(rid: &str) -> bool {
    !rid.is_empty()
        && rid
            .chars()
            .all(|c| c.is_ascii_alphanumeric() || matches!(c, '.' | '_' | '-'))
}

async fn create_saved_query(
    State(state): State<AppState>,
    Query(params): Query<CreateSavedQueryParams>,
    Json(body): Json<CreateSavedQueryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    // owner_id must be derived from the authenticated user; until the
    // axum auth layer is wired into this side router we accept the
    // anonymous (nil) UUID and rely on the database's `NOT NULL`
    // constraint as a backstop. The Flight SQL gRPC surface is the
    // primary auth-gated entrypoint; the saved-queries CRUD here is a
    // BI-dashboard convenience.
    let owner_id = Uuid::nil();
    // P5 — Foundry "Open in SQL workbench" entry point. When the body
    // `sql` is empty AND `?seed_dataset_rid=` is present, pre-fill the
    // SQL with `SELECT * FROM <dataset>` so the user lands on a
    // runnable query. The dataset RID is sanitised to alphanumeric +
    // dot/dash/underscore so it can be embedded in SQL safely.
    let body_sql = body.sql.as_deref().unwrap_or("").trim().to_string();
    let sql = if body_sql.is_empty() {
        match params.seed_dataset_rid.as_deref() {
            Some(rid) if is_safe_rid(rid) => {
                format!("SELECT * FROM \"{rid}\" LIMIT 100")
            }
            _ => body_sql,
        }
    } else {
        body_sql
    };
    let result = sqlx::query_as::<_, SavedQuery>(
        r#"INSERT INTO saved_queries (id, name, description, sql, owner_id)
           VALUES ($1, $2, $3, $4, $5)
           RETURNING *"#,
    )
    .bind(id)
    .bind(&body.name)
    .bind(body.description.as_deref().unwrap_or(""))
    .bind(&sql)
    .bind(owner_id)
    .fetch_one(state.db.as_ref())
    .await;

    match result {
        Ok(q) => (StatusCode::CREATED, Json(q)).into_response(),
        Err(e) => {
            tracing::error!("create saved query failed: {e}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "create failed" })),
            )
                .into_response()
        }
    }
}

async fn list_saved_queries(
    State(state): State<AppState>,
    Query(params): Query<ListQueriesQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let search_pattern = params.search.map(|s| format!("%{s}%"));

    let queries = sqlx::query_as::<_, SavedQuery>(
        r#"SELECT * FROM saved_queries
           WHERE ($1::TEXT IS NULL OR name ILIKE $1 OR description ILIKE $1)
           ORDER BY updated_at DESC
           LIMIT $2 OFFSET $3"#,
    )
    .bind(&search_pattern)
    .bind(per_page)
    .bind(offset)
    .fetch_all(state.db.as_ref())
    .await;

    match queries {
        Ok(qs) => Json(serde_json::json!({
            "data": qs,
            "page": page,
            "per_page": per_page,
        }))
        .into_response(),
        Err(e) => {
            tracing::error!("list saved queries failed: {e}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn delete_saved_query(
    State(state): State<AppState>,
    Path(query_id): Path<Uuid>,
) -> impl IntoResponse {
    let result = sqlx::query("DELETE FROM saved_queries WHERE id = $1")
        .bind(query_id)
        .execute(state.db.as_ref())
        .await;

    match result {
        Ok(r) if r.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => {
            tracing::error!("delete saved query failed: {e}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}
