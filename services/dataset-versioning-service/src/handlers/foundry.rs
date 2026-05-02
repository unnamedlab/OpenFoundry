//! Endpoints Foundry-style del `dataset-versioning-service`.
//!
//! Este módulo implementa la firma pública pedida en T0.1 sobre el esquema
//! definido en `migrations/20260501000001_versioning_init.sql`:
//!
//! * Branches:
//!   - `POST   /v1/datasets/{rid}/branches`
//!   - `GET    /v1/datasets/{rid}/branches`
//!   - `GET    /v1/datasets/{rid}/branches/{branch}`
//!   - `DELETE /v1/datasets/{rid}/branches/{branch}`
//!   - `POST   /v1/datasets/{rid}/branches/{branch}:reparent`
//!
//! * Transactions:
//!   - `POST /v1/datasets/{rid}/branches/{branch}/transactions`
//!     (body `{ "type": "SNAPSHOT|APPEND|UPDATE|DELETE", "providence": {...} }`)
//!   - `POST /v1/datasets/{rid}/branches/{branch}/transactions/{txn}:commit`
//!   - `POST /v1/datasets/{rid}/branches/{branch}/transactions/{txn}:abort`
//!   - `GET  /v1/datasets/{rid}/branches/{branch}/transactions/{txn}`
//!   - `GET  /v1/datasets/{rid}/transactions?branch=&before=`
//!
//! * Views:
//!   - `GET /v1/datasets/{rid}/views/current`
//!   - `GET /v1/datasets/{rid}/views/at?ts=`
//!   - `GET /v1/datasets/{rid}/views/{view_id}/files`
//!
//! Las acciones `:reparent`, `:commit`, `:abort` se enrutan como segmentos
//! únicos (`{branch}`, `{txn}`) y el handler parsea el sufijo `:<accion>`,
//! ya que Axum 0.8 no soporta capturas con sufijo literal en un mismo
//! segmento.

use std::str::FromStr;

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sqlx::Row;
use uuid::Uuid;

use crate::AppState;

// ─────────────────────────────────────────────────────────────────────────────
// Tipos de petición / respuesta
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct CreateBranchBody {
    pub name: String,
    /// Padre por nombre. Mutuamente excluyente con `from_transaction`.
    /// Si ambos están ausentes, se crea una rama raíz.
    #[serde(default)]
    pub parent_branch: Option<String>,
    /// Padre derivado de una transacción concreta (Foundry
    /// `create_child_branch(from_transaction = …)`): la nueva rama
    /// hereda el `parent_branch_id` de esa txn y arranca con
    /// `head_transaction_id` apuntando a ella.
    #[serde(default)]
    pub from_transaction: Option<Uuid>,
    #[serde(default)]
    pub description: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ReparentBody {
    /// Nuevo padre. `None` ⇒ promoverse a rama raíz.
    #[serde(default)]
    pub new_parent_branch: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct StartTransactionBody {
    /// `SNAPSHOT | APPEND | UPDATE | DELETE`.
    #[serde(rename = "type")]
    pub tx_type: String,
    #[serde(default)]
    pub providence: Value,
    #[serde(default)]
    pub summary: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ListTxQuery {
    #[serde(default)]
    pub branch: Option<String>,
    /// ISO-8601. Devuelve transacciones iniciadas estrictamente antes.
    #[serde(default)]
    pub before: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ViewAtQuery {
    /// ISO-8601 timestamp. Si falta, equivale a `views/current`.
    #[serde(default)]
    pub ts: Option<String>,
    #[serde(default = "default_branch_master")]
    pub branch: String,
}

fn default_branch_master() -> String {
    "master".to_string()
}

#[derive(Debug, Serialize, sqlx::FromRow)]
pub struct BranchOut {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub name: String,
    pub parent_branch_id: Option<Uuid>,
    pub head_transaction_id: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Serialize, sqlx::FromRow)]
pub struct TransactionOut {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub branch_id: Uuid,
    pub branch_name: String,
    pub tx_type: String,
    pub status: String,
    pub summary: String,
    pub metadata: Value,
    pub providence: Value,
    pub started_by: Option<Uuid>,
    pub started_at: DateTime<Utc>,
    pub committed_at: Option<DateTime<Utc>>,
    pub aborted_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Serialize)]
pub struct ViewOut {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub branch_id: Uuid,
    pub head_transaction_id: Uuid,
    pub computed_at: DateTime<Utc>,
    pub file_count: i32,
    pub size_bytes: i64,
    pub files: Vec<ViewFileOut>,
}

#[derive(Debug, Serialize, sqlx::FromRow)]
pub struct ViewFileOut {
    pub logical_path: String,
    pub physical_path: String,
    pub size_bytes: i64,
    pub introduced_by: Option<Uuid>,
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

/// Resuelve un identificador público (RID textual o UUID) al `id UUID` interno.
async fn resolve_dataset_id(
    state: &AppState,
    rid: &str,
) -> Result<Uuid, (StatusCode, Json<Value>)> {
    if let Ok(uuid) = Uuid::parse_str(rid) {
        return Ok(uuid);
    }
    let row = sqlx::query("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?;
    match row {
        Some(row) => Ok(row.get::<Uuid, _>("id")),
        None => Err((
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "dataset not found", "rid": rid })),
        )),
    }
}

async fn load_branch(
    state: &AppState,
    dataset_id: Uuid,
    branch: &str,
) -> Result<BranchOut, (StatusCode, Json<Value>)> {
    sqlx::query_as::<_, BranchOut>(
        r#"SELECT id, dataset_id, name, parent_branch_id, head_transaction_id,
                  created_at, updated_at
             FROM dataset_branches
            WHERE dataset_id = $1 AND name = $2 AND deleted_at IS NULL"#,
    )
    .bind(dataset_id)
    .bind(branch)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .ok_or_else(|| {
        (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "branch not found", "branch": branch })),
        )
    })
}

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "dataset-versioning-service: internal error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
}

#[derive(Debug, PartialEq, Eq)]
enum TxType {
    Snapshot,
    Append,
    Update,
    Delete,
}

impl FromStr for TxType {
    type Err = ();
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_ascii_uppercase().as_str() {
            "SNAPSHOT" => Ok(Self::Snapshot),
            "APPEND" => Ok(Self::Append),
            "UPDATE" => Ok(Self::Update),
            "DELETE" => Ok(Self::Delete),
            _ => Err(()),
        }
    }
}

impl TxType {
    fn as_str(&self) -> &'static str {
        match self {
            Self::Snapshot => "SNAPSHOT",
            Self::Append => "APPEND",
            Self::Update => "UPDATE",
            Self::Delete => "DELETE",
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Branches
// ─────────────────────────────────────────────────────────────────────────────

pub async fn list_branches(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
) -> Result<Json<Vec<BranchOut>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let rows = sqlx::query_as::<_, BranchOut>(
        r#"SELECT id, dataset_id, name, parent_branch_id, head_transaction_id,
                  created_at, updated_at
             FROM dataset_branches
            WHERE dataset_id = $1 AND deleted_at IS NULL
            ORDER BY (parent_branch_id IS NULL) DESC, name ASC"#,
    )
    .bind(dataset_id)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;
    Ok(Json(rows))
}

pub async fn create_branch(
    State(state): State<AppState>,
    user: AuthUser,
    Path(rid): Path<String>,
    Json(body): Json<CreateBranchBody>,
) -> Result<(StatusCode, Json<BranchOut>), (StatusCode, Json<Value>)> {
    let name = body.name.trim();
    if name.is_empty() {
        return Err(bad_request("branch name is required"));
    }
    if body.parent_branch.is_some() && body.from_transaction.is_some() {
        return Err(bad_request(
            "`parent_branch` and `from_transaction` are mutually exclusive",
        ));
    }
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    // Resuelve también el RID textual canónico para persistirlo en la rama.
    let dataset_rid = sqlx::query("SELECT rid FROM datasets WHERE id = $1")
        .bind(dataset_id)
        .fetch_one(&state.db)
        .await
        .map_err(internal)?
        .get::<String, _>("rid");

    // Resolución del padre y del puntero HEAD inicial.
    //
    //  * Sin parent_branch ni from_transaction → rama raíz, HEAD=NULL.
    //  * Con parent_branch → padre = esa rama; HEAD copia el HEAD del padre.
    //  * Con from_transaction → padre = la rama de la txn; HEAD = la txn.
    let (parent_branch_id, initial_head): (Option<Uuid>, Option<Uuid>) =
        if let Some(txn_id) = body.from_transaction {
            let row = sqlx::query(
                r#"SELECT branch_id, dataset_id FROM dataset_transactions
                    WHERE id = $1"#,
            )
            .bind(txn_id)
            .fetch_optional(&state.db)
            .await
            .map_err(internal)?
            .ok_or_else(|| {
                (
                    StatusCode::NOT_FOUND,
                    Json(json!({ "error": "from_transaction not found", "txn": txn_id })),
                )
            })?;
            let txn_dataset_id: Uuid = row.get("dataset_id");
            if txn_dataset_id != dataset_id {
                return Err(bad_request(
                    "from_transaction belongs to a different dataset",
                ));
            }
            (Some(row.get::<Uuid, _>("branch_id")), Some(txn_id))
        } else if let Some(p) = body.parent_branch.as_deref().map(str::trim).filter(|p| !p.is_empty()) {
            let parent = load_branch(&state, dataset_id, p).await?;
            (Some(parent.id), parent.head_transaction_id)
        } else {
            (None, None)
        };

    let row = sqlx::query_as::<_, BranchOut>(
        r#"INSERT INTO dataset_branches (
               id, dataset_id, dataset_rid, name,
               parent_branch_id, head_transaction_id,
               version, base_version, description, is_default, created_by
           )
           VALUES ($1, $2, $3, $4, $5, $6,
                   COALESCE((SELECT version FROM dataset_branches WHERE id = $5), 1),
                   1, $7, FALSE, $8)
           RETURNING id, dataset_id, name, parent_branch_id,
                     head_transaction_id, created_at, updated_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(dataset_id)
    .bind(&dataset_rid)
    .bind(name)
    .bind(parent_branch_id)
    .bind(initial_head)
    .bind(body.description.unwrap_or_default())
    .bind(user.0.sub)
    .fetch_one(&state.db)
    .await
    .map_err(|e| match e {
        sqlx::Error::Database(db) if db.is_unique_violation() => (
            StatusCode::CONFLICT,
            Json(json!({
                "error": "a branch with that name already exists for this dataset",
                "name": name,
            })),
        ),
        other => internal(other),
    })?;

    tracing::info!(
        rid, branch = %row.name,
        parent = ?parent_branch_id,
        head = ?initial_head,
        "branch created"
    );
    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn get_branch(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<BranchOut>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    Ok(Json(load_branch(&state, dataset_id, &branch).await?))
}

pub async fn delete_branch(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<StatusCode, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;

    // Foundry guarantee: deleting a branch never orphans its descendants.
    // Children get re-parented to the grandparent (which may be NULL =>
    // they become roots). Soft-delete (`deleted_at`) keeps the row for
    // audit while freeing the (dataset_id, name) slot via the partial
    // unique index `uq_dataset_branches_dataset_id_name_active`.
    let mut tx = state.db.begin().await.map_err(internal)?;
    sqlx::query(
        r#"UPDATE dataset_branches
              SET parent_branch_id = $2
            WHERE parent_branch_id = $1 AND deleted_at IS NULL"#,
    )
    .bind(target.id)
    .bind(target.parent_branch_id)
    .execute(&mut *tx)
    .await
    .map_err(internal)?;

    sqlx::query(
        r#"UPDATE dataset_branches
              SET deleted_at = NOW(), updated_at = NOW()
            WHERE id = $1 AND deleted_at IS NULL"#,
    )
    .bind(target.id)
    .execute(&mut *tx)
    .await
    .map_err(internal)?;

    tx.commit().await.map_err(internal)?;
    tracing::info!(rid, branch = %target.name, "branch soft-deleted (children re-parented)");
    Ok(StatusCode::NO_CONTENT)
}

/// Despachador para `POST /branches/{branch}:reparent` (única acción soportada
/// hoy sobre un segmento de rama). Si el segmento no contiene `:reparent`
/// devuelve 405.
pub async fn branch_action(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch_action)): Path<(String, String)>,
    Json(body): Json<ReparentBody>,
) -> Result<Json<BranchOut>, (StatusCode, Json<Value>)> {
    let (branch, action) = match branch_action.split_once(':') {
        Some(parts) => parts,
        None => {
            return Err((
                StatusCode::METHOD_NOT_ALLOWED,
                Json(json!({
                    "error": "POST on /branches/{branch} requires a ':reparent' action suffix",
                })),
            ));
        }
    };
    if action != "reparent" {
        return Err(bad_request(
            "unsupported branch action; only ':reparent' is supported",
        ));
    }

    let _ = user;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, branch).await?;

    let new_parent_id = match body.new_parent_branch.as_deref().map(str::trim) {
        Some(p) if !p.is_empty() => Some(load_branch(&state, dataset_id, p).await?.id),
        _ => None,
    };

    if Some(target.id) == new_parent_id {
        return Err(bad_request("a branch cannot be its own parent"));
    }

    let row = sqlx::query_as::<_, BranchOut>(
        r#"UPDATE dataset_branches
              SET parent_branch_id = $2,
                  updated_at = NOW()
            WHERE id = $1
            RETURNING id, dataset_id, name, parent_branch_id,
                      head_transaction_id, created_at, updated_at"#,
    )
    .bind(target.id)
    .bind(new_parent_id)
    .fetch_one(&state.db)
    .await
    .map_err(internal)?;

    tracing::info!(rid, branch = %row.name, new_parent = ?new_parent_id, "branch reparented");
    Ok(Json(row))
}

// ─────────────────────────────────────────────────────────────────────────────
// Transactions
// ─────────────────────────────────────────────────────────────────────────────

pub async fn start_transaction(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
    Json(body): Json<StartTransactionBody>,
) -> Result<(StatusCode, Json<TransactionOut>), (StatusCode, Json<Value>)> {
    let tx_type =
        TxType::from_str(&body.tx_type).map_err(|_| bad_request("invalid transaction type"))?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let summary = body.summary.unwrap_or_default();

    // El UNIQUE parcial garantiza una OPEN por rama; aquí devolvemos 409
    // si ya existe.
    let row = sqlx::query_as::<_, TransactionOut>(
        r#"INSERT INTO dataset_transactions (
               id, dataset_id, branch_id, branch_name, tx_type, status,
               operation, summary, providence, started_by
           )
           VALUES ($1, $2, $3, $4, $5, 'OPEN', $6, $7, $8::jsonb, $9)
           RETURNING id, dataset_id, branch_id, branch_name, tx_type, status,
                     summary, metadata, providence, started_by,
                     started_at, committed_at, aborted_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(dataset_id)
    .bind(target.id)
    .bind(&target.name)
    .bind(tx_type.as_str())
    .bind(tx_type.as_str().to_ascii_lowercase())
    .bind(&summary)
    .bind(body.providence)
    .bind(user.0.sub)
    .fetch_one(&state.db)
    .await
    .map_err(|e| match e {
        sqlx::Error::Database(db) if db.constraint() == Some("uq_dataset_transactions_one_open_per_branch") => (
            StatusCode::CONFLICT,
            Json(json!({
                "error": "branch already has an OPEN transaction",
                "branch": target.name,
            })),
        ),
        other => internal(other),
    })?;

    Ok((StatusCode::CREATED, Json(row)))
}

pub async fn get_transaction(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch, txn)): Path<(String, String, String)>,
) -> Result<Json<TransactionOut>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let txn_id =
        Uuid::parse_str(&txn).map_err(|_| bad_request("transaction id is not a valid UUID"))?;

    sqlx::query_as::<_, TransactionOut>(
        r#"SELECT id, dataset_id, branch_id, branch_name, tx_type, status,
                  summary, metadata, providence, started_by,
                  started_at, committed_at, aborted_at
             FROM dataset_transactions
            WHERE dataset_id = $1 AND branch_name = $2 AND id = $3"#,
    )
    .bind(dataset_id)
    .bind(&branch)
    .bind(txn_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .map(Json)
    .ok_or_else(|| {
        (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "transaction not found" })),
        )
    })
}

/// Despachador para `POST /transactions/{txn}:commit` y `:abort`.
///
/// Las invariantes de negocio (`OPEN-only`, APPEND/UPDATE/DELETE/SNAPSHOT
/// rules) viven en `crate::domain::transactions`; este handler sólo enruta,
/// resuelve el dataset, llama al dominio y mapea el resultado a HTTP.
pub async fn transaction_action(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch, txn_action)): Path<(String, String, String)>,
) -> Result<Json<TransactionOut>, (StatusCode, Json<Value>)> {
    use crate::domain::transactions as txn_domain;

    let (txn_str, action) = match txn_action.split_once(':') {
        Some(parts) => parts,
        None => {
            return Err((
                StatusCode::METHOD_NOT_ALLOWED,
                Json(json!({
                    "error": "POST on /transactions/{txn} requires ':commit' or ':abort' action suffix",
                })),
            ));
        }
    };
    let txn_id =
        Uuid::parse_str(txn_str).map_err(|_| bad_request("transaction id is not a valid UUID"))?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;

    let result = match action {
        "commit" => txn_domain::commit_transaction(&state.db, txn_id).await,
        "abort" => txn_domain::abort_transaction(&state.db, txn_id).await,
        _ => return Err(bad_request("unsupported transaction action")),
    };

    if let Err(err) = result {
        return Err(map_commit_error(err));
    }

    // Re-fetch the canonical row (and verify branch/dataset scoping).
    sqlx::query_as::<_, TransactionOut>(
        r#"SELECT id, dataset_id, branch_id, branch_name, tx_type, status,
                  summary, metadata, providence, started_by,
                  started_at, committed_at, aborted_at
             FROM dataset_transactions
            WHERE id = $1 AND dataset_id = $2 AND branch_name = $3"#,
    )
    .bind(txn_id)
    .bind(dataset_id)
    .bind(&branch)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .map(Json)
    .ok_or_else(|| {
        (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "transaction not found after action", "txn": txn_id })),
        )
    })
}

fn map_commit_error(err: crate::domain::transactions::CommitError) -> (StatusCode, Json<Value>) {
    use crate::domain::transactions::CommitError;
    match err {
        CommitError::NotFound => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "transaction not found" })),
        ),
        CommitError::NotOpen { current } => (
            StatusCode::CONFLICT,
            Json(json!({
                "error": "transaction is not OPEN",
                "current_state": current.to_string(),
            })),
        ),
        CommitError::AppendModifiesExisting { count, paths } => (
            StatusCode::CONFLICT,
            Json(json!({
                "error": "APPEND cannot modify files already present in the current view",
                "count": count,
                "paths": paths,
            })),
        ),
        CommitError::DeleteWithWriteOps { count, paths } => (
            StatusCode::BAD_REQUEST,
            Json(json!({
                "error": "DELETE may only carry REMOVE ops",
                "count": count,
                "paths": paths,
            })),
        ),
        CommitError::SnapshotWithRemoveOps { count, paths } => (
            StatusCode::BAD_REQUEST,
            Json(json!({
                "error": "SNAPSHOT cannot stage REMOVE ops; the view is reset wholesale",
                "count": count,
                "paths": paths,
            })),
        ),
        CommitError::UnknownKind { kind } => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": "unknown transaction kind in DB", "kind": kind })),
        ),
        CommitError::Database(msg) => internal(msg),
    }
}

pub async fn list_transactions(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Query(params): Query<ListTxQuery>,
) -> Result<Json<Vec<TransactionOut>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let before = match params.before.as_deref() {
        Some(s) => Some(
            DateTime::parse_from_rfc3339(s)
                .map_err(|_| bad_request("'before' must be RFC3339"))?
                .with_timezone(&Utc),
        ),
        None => None,
    };

    let rows = sqlx::query_as::<_, TransactionOut>(
        r#"SELECT id, dataset_id, branch_id, branch_name, tx_type, status,
                  summary, metadata, providence, started_by,
                  started_at, committed_at, aborted_at
             FROM dataset_transactions
            WHERE dataset_id = $1
              AND ($2::text IS NULL OR branch_name = $2)
              AND ($3::timestamptz IS NULL OR started_at < $3)
            ORDER BY started_at DESC
            LIMIT 200"#,
    )
    .bind(dataset_id)
    .bind(params.branch.as_deref())
    .bind(before)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;
    Ok(Json(rows))
}

// ─────────────────────────────────────────────────────────────────────────────
// Views
// ─────────────────────────────────────────────────────────────────────────────

/// Materializa (o devuelve) la vista actual de una rama: archivos efectivos
/// resultantes de aplicar SNAPSHOT/APPEND/UPDATE/DELETE en orden temporal.
pub async fn get_current_view(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Query(params): Query<ViewAtQuery>,
) -> Result<Json<ViewOut>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    compute_view_at(&state, dataset_id, &params.branch, None)
        .await
        .map(Json)
}

pub async fn get_view_at(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Query(params): Query<ViewAtQuery>,
) -> Result<Json<ViewOut>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let at = match params.ts.as_deref() {
        Some(s) => Some(
            DateTime::parse_from_rfc3339(s)
                .map_err(|_| bad_request("'ts' must be RFC3339"))?
                .with_timezone(&Utc),
        ),
        None => None,
    };
    compute_view_at(&state, dataset_id, &params.branch, at)
        .await
        .map(Json)
}

pub async fn list_view_files(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, view_id)): Path<(String, String)>,
) -> Result<Json<Vec<ViewFileOut>>, (StatusCode, Json<Value>)> {
    let _ = resolve_dataset_id(&state, &rid).await?;
    let view_uuid =
        Uuid::parse_str(&view_id).map_err(|_| bad_request("view_id is not a valid UUID"))?;

    let rows = sqlx::query_as::<_, ViewFileOut>(
        r#"SELECT logical_path, physical_path, size_bytes, introduced_by
             FROM dataset_view_files
            WHERE view_id = $1
            ORDER BY logical_path ASC"#,
    )
    .bind(view_uuid)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;
    Ok(Json(rows))
}

/// Cálculo de vista perezoso: enumera transacciones COMMITTED de la rama
/// (acotadas por `at`) y aplica el algoritmo SNAPSHOT/APPEND/UPDATE/DELETE
/// descrito en `Datasets.md` (sección "Dataset views").
async fn compute_view_at(
    state: &AppState,
    dataset_id: Uuid,
    branch: &str,
    at: Option<DateTime<Utc>>,
) -> Result<ViewOut, (StatusCode, Json<Value>)> {
    let target = load_branch(state, dataset_id, branch).await?;

    let txns: Vec<(Uuid, String, DateTime<Utc>)> = sqlx::query(
        r#"SELECT id, tx_type, COALESCE(committed_at, started_at) AS ts
             FROM dataset_transactions
            WHERE branch_id = $1
              AND status = 'COMMITTED'
              AND ($2::timestamptz IS NULL OR COALESCE(committed_at, started_at) <= $2)
            ORDER BY COALESCE(committed_at, started_at) ASC, started_at ASC"#,
    )
    .bind(target.id)
    .bind(at)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?
    .into_iter()
    .map(|row| (row.get("id"), row.get("tx_type"), row.get("ts")))
    .collect();

    // Localizar último SNAPSHOT (≤ at) y arrancar desde allí.
    let start_idx = txns
        .iter()
        .rposition(|(_, t, _)| t == "SNAPSHOT")
        .unwrap_or(0);

    use std::collections::BTreeMap;
    let mut files: BTreeMap<String, ViewFileOut> = BTreeMap::new();

    for (txn_id, tx_type, _ts) in txns.iter().skip(start_idx) {
        let rows = sqlx::query(
            r#"SELECT logical_path, physical_path, size_bytes, op
                 FROM dataset_transaction_files
                WHERE transaction_id = $1"#,
        )
        .bind(txn_id)
        .fetch_all(&state.db)
        .await
        .map_err(internal)?;

        match tx_type.as_str() {
            "SNAPSHOT" => {
                files.clear();
                for r in rows {
                    let lp: String = r.get("logical_path");
                    files.insert(
                        lp.clone(),
                        ViewFileOut {
                            logical_path: lp,
                            physical_path: r.get("physical_path"),
                            size_bytes: r.get::<i64, _>("size_bytes"),
                            introduced_by: Some(*txn_id),
                        },
                    );
                }
            }
            "APPEND" => {
                for r in rows {
                    let lp: String = r.get("logical_path");
                    files.entry(lp.clone()).or_insert(ViewFileOut {
                        logical_path: lp,
                        physical_path: r.get("physical_path"),
                        size_bytes: r.get::<i64, _>("size_bytes"),
                        introduced_by: Some(*txn_id),
                    });
                }
            }
            "UPDATE" => {
                for r in rows {
                    let lp: String = r.get("logical_path");
                    files.insert(
                        lp.clone(),
                        ViewFileOut {
                            logical_path: lp,
                            physical_path: r.get("physical_path"),
                            size_bytes: r.get::<i64, _>("size_bytes"),
                            introduced_by: Some(*txn_id),
                        },
                    );
                }
            }
            "DELETE" => {
                for r in rows {
                    let lp: String = r.get("logical_path");
                    files.remove(&lp);
                }
            }
            _ => {}
        }
    }

    let head_txn_id = txns.last().map(|(id, _, _)| *id).unwrap_or(Uuid::nil());
    let file_count = files.len() as i32;
    let size_bytes: i64 = files.values().map(|f| f.size_bytes).sum();

    // Persistir la vista para futuras consultas O(1) (idempotente por
    // (dataset, branch, head_transaction)).
    let view_id = if head_txn_id != Uuid::nil() {
        let new_id = Uuid::now_v7();
        let row = sqlx::query(
            r#"INSERT INTO dataset_views (
                   id, dataset_id, branch_id, head_transaction_id,
                   computed_at, file_count, size_bytes
               )
               VALUES ($1, $2, $3, $4, NOW(), $5, $6)
               ON CONFLICT (dataset_id, branch_id, head_transaction_id) DO UPDATE
                  SET computed_at = NOW()
               RETURNING id"#,
        )
        .bind(new_id)
        .bind(dataset_id)
        .bind(target.id)
        .bind(head_txn_id)
        .bind(file_count)
        .bind(size_bytes)
        .fetch_one(&state.db)
        .await
        .map_err(internal)?;
        let persisted: Uuid = row.get("id");

        // Refrescar archivos de la vista.
        sqlx::query("DELETE FROM dataset_view_files WHERE view_id = $1")
            .bind(persisted)
            .execute(&state.db)
            .await
            .map_err(internal)?;
        for f in files.values() {
            sqlx::query(
                r#"INSERT INTO dataset_view_files
                       (view_id, logical_path, physical_path, size_bytes, introduced_by)
                   VALUES ($1, $2, $3, $4, $5)"#,
            )
            .bind(persisted)
            .bind(&f.logical_path)
            .bind(&f.physical_path)
            .bind(f.size_bytes)
            .bind(f.introduced_by)
            .execute(&state.db)
            .await
            .map_err(internal)?;
        }
        persisted
    } else {
        Uuid::nil()
    };

    Ok(ViewOut {
        id: view_id,
        dataset_id,
        branch_id: target.id,
        head_transaction_id: head_txn_id,
        computed_at: Utc::now(),
        file_count,
        size_bytes,
        files: files.into_values().collect(),
    })
}

// ─────────────────────────────────────────────────────────────────────────────
// Fallback chain (T2.3 backing data).
//
// Cada rama puede tener una cadena ordenada de "ramas vecinas" a las que el
// build se cae cuando un dataset de entrada no contiene la rama solicitada.
// El orden lo da `position` ascendente (0 gana primero).
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Serialize, sqlx::FromRow)]
pub struct FallbackEntry {
    pub position: i32,
    pub fallback_branch_name: String,
}

#[derive(Debug, Deserialize)]
pub struct PutFallbacksBody {
    /// Lista ordenada de nombres de rama. Reemplaza la cadena actual de forma
    /// at\u00f3mica (DELETE + INSERT en una transacci\u00f3n).
    pub chain: Vec<String>,
}

pub async fn list_fallbacks(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<Vec<FallbackEntry>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let rows = sqlx::query_as::<_, FallbackEntry>(
        r#"SELECT position, fallback_branch_name
             FROM dataset_branch_fallbacks
            WHERE branch_id = $1
            ORDER BY position ASC"#,
    )
    .bind(target.id)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;
    Ok(Json(rows))
}

pub async fn put_fallbacks(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
    Json(body): Json<PutFallbacksBody>,
) -> Result<Json<Vec<FallbackEntry>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    // Sanity: ningún nombre vacío y sin auto-referencias triviales.
    for name in &body.chain {
        let trimmed = name.trim();
        if trimmed.is_empty() {
            return Err(bad_request("fallback chain entries must be non-empty"));
        }
        if trimmed == target.name {
            return Err(bad_request(
                "fallback chain cannot reference the branch itself",
            ));
        }
    }

    let mut tx = state.db.begin().await.map_err(internal)?;
    sqlx::query("DELETE FROM dataset_branch_fallbacks WHERE branch_id = $1")
        .bind(target.id)
        .execute(&mut *tx)
        .await
        .map_err(internal)?;
    for (i, name) in body.chain.iter().enumerate() {
        sqlx::query(
            r#"INSERT INTO dataset_branch_fallbacks
                   (branch_id, position, fallback_branch_name)
               VALUES ($1, $2, $3)"#,
        )
        .bind(target.id)
        .bind(i as i32)
        .bind(name.trim())
        .execute(&mut *tx)
        .await
        .map_err(internal)?;
    }
    tx.commit().await.map_err(internal)?;

    let rows = sqlx::query_as::<_, FallbackEntry>(
        r#"SELECT position, fallback_branch_name
             FROM dataset_branch_fallbacks
            WHERE branch_id = $1
            ORDER BY position ASC"#,
    )
    .bind(target.id)
    .fetch_all(&state.db)
    .await
    .map_err(internal)?;
    Ok(Json(rows))
}
