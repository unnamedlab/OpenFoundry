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

use std::{collections::HashSet, str::FromStr};

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::{HeaderMap, StatusCode},
    response::Response,
};
use chrono::{DateTime, Utc};
use core_models::TransactionType;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::handlers::conformance::{
    BatchItemResult, ErrorEnvelope, Page, PageQuery, batch_response, json_with_etag,
};
use crate::storage::{
    RuntimeStore,
    runtime::{
        NewFoundryBranch, RuntimeBranch as BranchOut, RuntimeFallbackEntry as FallbackEntry,
        RuntimeTransaction as TransactionOut, RuntimeViewFile as ViewFileOut,
    },
    transactional::TransactionalDatasetWriter,
};

// ─────────────────────────────────────────────────────────────────────────────
// Tipos de petición / respuesta
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct CreateBranchBody {
    pub name: String,
    /// Padre por nombre. Compat legacy. Mutuamente excluyente con
    /// `from_transaction`. `source` (P1) toma precedencia.
    #[serde(default)]
    pub parent_branch: Option<String>,
    /// Padre derivado de una transacción concreta (Foundry
    /// `create_child_branch(from_transaction = …)`). Compat legacy.
    #[serde(default)]
    pub from_transaction: Option<Uuid>,
    #[serde(default)]
    pub description: Option<String>,
    /// P1 — discriminated branch source. When present, takes
    /// precedence over `parent_branch`/`from_transaction`.
    #[serde(default)]
    pub source: Option<BranchSource>,
    /// Optional initial fallback chain. When omitted the handler
    /// picks a sensible default per source kind:
    ///   * `from_branch`            → `[parent_name]`
    ///   * `from_transaction_rid`   → `[<branch of that transaction>]`
    ///   * `as_root`                → `[]`
    #[serde(default)]
    pub fallback_chain: Option<Vec<String>>,
    /// Free-form metadata (persona, ticket, …).
    #[serde(default)]
    pub labels: Option<serde_json::Map<String, Value>>,
}

/// Discriminated branch-source. Exactly one of the three keys must be
/// truthy; the handler 400s otherwise.
#[derive(Debug, Default, Deserialize)]
pub struct BranchSource {
    /// Create a child off another branch. HEAD copies the parent's
    /// committed HEAD even when the parent has an OPEN transaction.
    #[serde(default)]
    pub from_branch: Option<String>,
    /// Create a child off a specific COMMITTED transaction. Accepts
    /// either a UUID or a `ri.foundry.main.transaction.<uuid>` RID.
    #[serde(default)]
    pub from_transaction_rid: Option<String>,
    /// Mint a root branch. Only valid when the dataset has no other
    /// active branches yet.
    #[serde(default)]
    pub as_root: Option<bool>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum BranchSourceKind {
    Root,
    ChildFromBranch,
    ChildFromTransaction,
}

impl BranchSourceKind {
    fn metric_label(self) -> &'static str {
        match self {
            Self::Root => "root",
            Self::ChildFromBranch => "child_from_branch",
            Self::ChildFromTransaction => "child_from_transaction",
        }
    }

    fn audit_label(self) -> &'static str {
        self.metric_label()
    }
}

/// Parse `ri.foundry.main.transaction.<uuid>` or a bare UUID.
fn parse_transaction_rid(input: &str) -> Option<Uuid> {
    const PREFIX: &str = "ri.foundry.main.transaction.";
    let trimmed = input.trim();
    if let Some(rest) = trimmed.strip_prefix(PREFIX) {
        Uuid::parse_str(rest).ok()
    } else {
        Uuid::parse_str(trimmed).ok()
    }
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
    /// Optional committed transaction id. When present it takes
    /// precedence over `ts` and returns the view at that commit.
    #[serde(default)]
    pub transaction_id: Option<Uuid>,
    #[serde(default = "default_branch_master")]
    pub branch: String,
}

#[derive(Debug, Deserialize)]
pub struct RollbackBody {
    pub transaction_id: Uuid,
    #[serde(default)]
    pub summary: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct CompareQuery {
    #[serde(default)]
    pub base_branch: Option<String>,
    #[serde(default)]
    pub target_branch: Option<String>,
    #[serde(default)]
    pub base_transaction: Option<Uuid>,
    #[serde(default)]
    pub target_transaction: Option<Uuid>,
}

fn default_branch_master() -> String {
    "master".to_string()
}

#[derive(Debug, Serialize)]
pub struct ViewOut {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub branch_id: Uuid,
    pub head_transaction_id: Uuid,
    pub requested_branch: String,
    pub resolved_branch: String,
    pub fallback_chain: Vec<String>,
    pub computed_at: DateTime<Utc>,
    pub file_count: i32,
    pub size_bytes: i64,
    pub files: Vec<ViewFileOut>,
}

#[derive(Debug, Serialize)]
pub struct FileDiff {
    pub added: Vec<ViewFileOut>,
    pub removed: Vec<ViewFileOut>,
    pub modified: Vec<FileChange>,
}

#[derive(Debug, Serialize)]
pub struct FileChange {
    pub logical_path: String,
    pub before: ViewFileOut,
    pub after: ViewFileOut,
}

#[derive(Debug, Serialize)]
pub struct CompareOut {
    pub base: ViewOut,
    pub target: ViewOut,
    pub files: FileDiff,
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

fn runtime(state: &AppState) -> RuntimeStore {
    RuntimeStore::new(state.db.clone())
}

/// Resuelve un identificador público (RID textual o UUID) al `id UUID` interno.
async fn resolve_dataset_id(
    state: &AppState,
    rid: &str,
) -> Result<Uuid, (StatusCode, Json<Value>)> {
    if let Ok(uuid) = Uuid::parse_str(rid) {
        return Ok(uuid);
    }
    let row = sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?;
    match row {
        Some(id) => Ok(id),
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
    runtime(state)
        .load_active_branch(dataset_id, branch)
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

    fn as_model(&self) -> TransactionType {
        match self {
            Self::Snapshot => TransactionType::Snapshot,
            Self::Append => TransactionType::Append,
            Self::Update => TransactionType::Update,
            Self::Delete => TransactionType::Delete,
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
    Query(page): Query<PageQuery>,
) -> Result<Json<Page<BranchOut>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let rows = runtime(&state)
        .list_active_branches(dataset_id)
        .await
        .map_err(internal)?;
    Ok(Json(slice_into_page(rows, &page)))
}

/// In-memory pagination over a vector — fine for the dataset-surface
/// row counts we expect today (≤ a few hundred). Centralised so each
/// list handler stays readable.
fn slice_into_page<T>(rows: Vec<T>, page: &PageQuery) -> Page<T> {
    let total = rows.len() as i64;
    let offset = page.offset();
    let limit = page.effective_limit();
    let start = offset.clamp(0, total) as usize;
    let end = (offset + limit).clamp(0, total) as usize;
    let has_more = (offset + limit) < total;
    let slice = rows.into_iter().skip(start).take(end - start).collect();
    Page::from_slice(slice, offset, limit, has_more)
}

pub async fn create_branch(
    State(state): State<AppState>,
    user: AuthUser,
    Path(rid): Path<String>,
    Json(body): Json<CreateBranchBody>,
) -> Result<(StatusCode, Json<BranchOut>), (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "branch.create")?;
    let name = body.name.trim().to_string();
    if name.is_empty() {
        return Err(bad_request("branch name is required"));
    }
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let dataset_rid = runtime(&state)
        .load_dataset_rid(dataset_id)
        .await
        .map_err(internal)?;

    // ── Source resolution ────────────────────────────────────────────
    //
    // Order of precedence:
    //   1. `source.{from_branch|from_transaction_rid|as_root}` (P1).
    //   2. Legacy `from_transaction` (UUID).
    //   3. Legacy `parent_branch` (string).
    //   4. Neither set → root branch (only when the dataset has no
    //      active branches; matches the Foundry "create root" call).
    let (kind, parent_branch_id, initial_head, created_from_txn, default_fallback) =
        resolve_branch_source(&state, dataset_id, &body).await?;

    let fallback_chain: Vec<String> = match body.fallback_chain.clone() {
        Some(chain) => chain
            .into_iter()
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect(),
        None => default_fallback,
    };

    let labels_value = match body.labels.clone() {
        Some(map) => Value::Object(map),
        None => Value::Object(serde_json::Map::new()),
    };

    let row = runtime(&state)
        .create_foundry_branch(NewFoundryBranch {
            id: Uuid::now_v7(),
            dataset_id,
            dataset_rid: &dataset_rid,
            name: &name,
            parent_branch_id,
            head_transaction_id: initial_head,
            created_from_transaction_id: created_from_txn,
            description: body.description.as_deref().unwrap_or_default(),
            created_by: user.0.sub,
            fallback_chain: fallback_chain.clone(),
            labels: labels_value.clone(),
        })
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

    crate::metrics::DATASET_BRANCHES_CREATED_TOTAL
        .with_label_values(&[kind.metric_label()])
        .inc();
    let _ = crate::metrics::refresh_branch_gauges(&state.db).await;

    // P4 — markings inheritance + outbox event. The snapshot copy
    // happens once, AT child creation time, so the parent owner can
    // never retroactively raise the child's clearance floor (Foundry
    // doc § "Branch security" / "Best practices and technical
    // details").
    let snapshot_markings = match parent_branch_id {
        Some(parent_id) => {
            sqlx::query_scalar::<_, Uuid>(
                r#"INSERT INTO branch_markings_snapshot (branch_id, marking_id, source, set_by)
                   SELECT $1, marking_id, 'PARENT', $2
                     FROM branch_markings_snapshot
                    WHERE branch_id = $3
                   ON CONFLICT (branch_id, marking_id) DO NOTHING
                   RETURNING marking_id"#,
            )
            .bind(row.id)
            .bind(user.0.sub)
            .bind(parent_id)
            .fetch_all(&state.db)
            .await
            .unwrap_or_default()
        }
        None => Vec::new(),
    };

    {
        use crate::domain::branch_events::{BranchEnvelope, EVT_CREATED, emit};
        let mut tx = state.db.begin().await.map_err(internal)?;
        let envelope = BranchEnvelope::new(EVT_CREATED, &row.rid, &dataset_rid, &user.0.sub.to_string())
            .with_parent_rid(parent_branch_id.map(|id| format!("ri.foundry.main.branch.{id}")))
            .with_head(initial_head.map(|id| format!("ri.foundry.main.transaction.{id}")))
            .with_fallback(fallback_chain.clone())
            .with_markings(snapshot_markings.clone())
            .with_extras(json!({
                "source_kind": kind.audit_label(),
                "created_from_transaction_rid": created_from_txn
                    .map(|id| format!("ri.foundry.main.transaction.{id}")),
            }));
        emit(&mut tx, &envelope)
            .await
            .map_err(|e| internal(e.to_string()))?;
        tx.commit().await.map_err(internal)?;
    }

    tracing::info!(
        rid, branch = %row.name,
        parent = ?parent_branch_id,
        head = ?initial_head,
        kind = kind.metric_label(),
        inherited_markings = snapshot_markings.len(),
        "branch created"
    );
    crate::security::emit_audit(
        &user.0.sub,
        "branch.created",
        &rid,
        json!({
            "branch_rid": row.rid,
            "branch": row.name,
            "source_kind": kind.audit_label(),
            "parent_rid": parent_branch_id.map(|id| format!("ri.foundry.main.branch.{id}")),
            "created_from_transaction_rid": created_from_txn
                .map(|id| format!("ri.foundry.main.transaction.{id}")),
            "labels": labels_value,
            "fallback_chain": fallback_chain,
        }),
    );
    Ok((StatusCode::CREATED, Json(row)))
}

#[allow(clippy::type_complexity)]
async fn resolve_branch_source(
    state: &AppState,
    dataset_id: Uuid,
    body: &CreateBranchBody,
) -> Result<
    (
        BranchSourceKind,
        Option<Uuid>, // parent_branch_id
        Option<Uuid>, // initial_head_transaction_id
        Option<Uuid>, // created_from_transaction_id
        Vec<String>, // default fallback chain
    ),
    (StatusCode, Json<Value>),
> {
    if body.parent_branch.is_some() && body.from_transaction.is_some() {
        return Err(bad_request(
            "`parent_branch` and `from_transaction` are mutually exclusive",
        ));
    }

    if let Some(source) = &body.source {
        let provided = [
            source
                .from_branch
                .as_deref()
                .map(str::trim)
                .filter(|s| !s.is_empty())
                .is_some(),
            source
                .from_transaction_rid
                .as_deref()
                .map(str::trim)
                .filter(|s| !s.is_empty())
                .is_some(),
            source.as_root.unwrap_or(false),
        ];
        let count = provided.iter().filter(|p| **p).count();
        if count != 1 {
            return Err(bad_request(
                "exactly one of `source.from_branch`, `source.from_transaction_rid`, `source.as_root` must be set",
            ));
        }

        if let Some(parent) = source
            .from_branch
            .as_deref()
            .map(str::trim)
            .filter(|s| !s.is_empty())
        {
            let parent_branch = load_branch(state, dataset_id, parent).await?;
            return Ok((
                BranchSourceKind::ChildFromBranch,
                Some(parent_branch.id),
                parent_branch.head_transaction_id,
                None,
                vec![parent_branch.name.clone()],
            ));
        }

        if let Some(rid_or_uuid) = source
            .from_transaction_rid
            .as_deref()
            .map(str::trim)
            .filter(|s| !s.is_empty())
        {
            let txn_id = parse_transaction_rid(rid_or_uuid).ok_or_else(|| {
                bad_request("from_transaction_rid is not a valid transaction RID")
            })?;
            return resolve_from_transaction(state, dataset_id, txn_id).await;
        }

        // as_root: only when no other branches exist on this dataset.
        if dataset_has_active_branches(state, dataset_id).await? {
            return Err((
                StatusCode::CONFLICT,
                Json(json!({
                    "error": "as_root requires the dataset to have no active branches yet",
                })),
            ));
        }
        return Ok((BranchSourceKind::Root, None, None, None, Vec::new()));
    }

    // Legacy fall-throughs.
    if let Some(txn_id) = body.from_transaction {
        return resolve_from_transaction(state, dataset_id, txn_id).await;
    }
    if let Some(parent) = body
        .parent_branch
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
    {
        let parent_branch = load_branch(state, dataset_id, parent).await?;
        return Ok((
            BranchSourceKind::ChildFromBranch,
            Some(parent_branch.id),
            parent_branch.head_transaction_id,
            None,
            vec![parent_branch.name.clone()],
        ));
    }

    // No source provided → implicit root branch (only if first one).
    if dataset_has_active_branches(state, dataset_id).await? {
        return Err(bad_request(
            "creating a root branch requires the dataset to have no active branches; \
             pass `source.from_branch` or `parent_branch` to create a child",
        ));
    }
    Ok((BranchSourceKind::Root, None, None, None, Vec::new()))
}

async fn dataset_has_active_branches(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<bool, (StatusCode, Json<Value>)> {
    sqlx::query_scalar::<_, bool>(
        r#"SELECT EXISTS(SELECT 1 FROM dataset_branches
                          WHERE dataset_id = $1 AND deleted_at IS NULL)"#,
    )
    .bind(dataset_id)
    .fetch_one(&state.db)
    .await
    .map_err(internal)
}

#[allow(clippy::type_complexity)]
async fn resolve_from_transaction(
    state: &AppState,
    dataset_id: Uuid,
    txn_id: Uuid,
) -> Result<
    (
        BranchSourceKind,
        Option<Uuid>,
        Option<Uuid>,
        Option<Uuid>,
        Vec<String>,
    ),
    (StatusCode, Json<Value>),
> {
    let scope = runtime(state)
        .load_transaction_scope(txn_id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "from_transaction not found", "txn": txn_id })),
            )
        })?;
    if scope.dataset_id != dataset_id {
        return Err(bad_request(
            "from_transaction belongs to a different dataset",
        ));
    }
    let status = runtime(state)
        .load_transaction_status(txn_id)
        .await
        .map_err(internal)?
        .unwrap_or_default();
    if status != "COMMITTED" {
        return Err((
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(json!({
                "error": "from_transaction must be COMMITTED",
                "transaction_id": txn_id,
                "status": status,
            })),
        ));
    }
    let parent_name = sqlx::query_scalar::<_, String>(
        "SELECT name FROM dataset_branches WHERE id = $1 AND deleted_at IS NULL",
    )
    .bind(scope.branch_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?
    .unwrap_or_default();
    let default_fallback = if parent_name.is_empty() {
        Vec::new()
    } else {
        vec![parent_name]
    };

    Ok((
        BranchSourceKind::ChildFromTransaction,
        Some(scope.branch_id),
        Some(txn_id),
        Some(txn_id),
        default_fallback,
    ))
}

pub async fn get_branch(
    State(state): State<AppState>,
    _user: AuthUser,
    headers: HeaderMap,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Response, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let row = load_branch(&state, dataset_id, &branch).await?;
    let value = serde_json::to_value(&row).map_err(|e| internal(e.to_string()))?;
    Ok(json_with_etag(&headers, value))
}

/// `GET /v1/datasets/{rid}/branches/{branch}/preview-delete`
///
/// P3 — read-only preview of what `DELETE /branches/{branch}` *would*
/// do. Powers the UI confirmation dialog so the user sees:
///   * which children are about to be re-parented,
///   * which transactions are preserved (always: deletion only moves
///     the branch pointer; transactions are kept for audit), and
///   * the head transaction RID at delete time.
pub async fn preview_delete_branch(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<Value>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;

    let children = runtime(&state)
        .list_direct_children(target.id)
        .await
        .map_err(internal)?;

    let new_parent_name = match target.parent_branch_id {
        Some(parent_id) => sqlx::query_scalar::<_, String>(
            "SELECT name FROM dataset_branches WHERE id = $1 AND deleted_at IS NULL",
        )
        .bind(parent_id)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?,
        None => None,
    };
    let new_parent_rid = target
        .parent_branch_id
        .map(|id| format!("ri.foundry.main.branch.{id}"));

    let children_to_reparent: Vec<Value> = children
        .iter()
        .map(|(child_id, child_name)| {
            json!({
                "branch": child_name,
                "branch_rid": format!("ri.foundry.main.branch.{child_id}"),
                "new_parent": new_parent_name,
                "new_parent_rid": new_parent_rid,
            })
        })
        .collect();

    let head_transaction = target.head_transaction_id.map(|id| {
        json!({
            "id": id,
            "rid": format!("ri.foundry.main.transaction.{id}"),
        })
    });

    Ok(Json(json!({
        "branch": target.name,
        "branch_rid": target.rid,
        "current_parent": new_parent_name,
        "current_parent_rid": new_parent_rid,
        "children_to_reparent": children_to_reparent,
        "transactions_preserved": true,
        "head_transaction": head_transaction,
    })))
}

pub async fn delete_branch(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<Value>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "branch.delete")?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;

    // Foundry guarantee: deleting a branch never orphans its descendants.
    // Children get re-parented to the grandparent (which may be NULL =>
    // they become roots). Soft-delete (`deleted_at`) keeps the row for
    // audit while freeing the (dataset_id, name) slot via the partial
    // unique index `uq_dataset_branches_dataset_id_name_active`.
    let children = runtime(&state)
        .list_direct_children(target.id)
        .await
        .map_err(internal)?;

    let new_parent_rid = target
        .parent_branch_id
        .map(|id| format!("ri.foundry.main.branch.{id}"));
    let new_parent_name = match target.parent_branch_id {
        Some(parent_id) => sqlx::query_scalar::<_, String>(
            "SELECT name FROM dataset_branches WHERE id = $1 AND deleted_at IS NULL",
        )
        .bind(parent_id)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?,
        None => None,
    };

    runtime(&state)
        .soft_delete_branch(target.id, target.parent_branch_id)
        .await
        .map_err(internal)?;
    let _ = crate::metrics::refresh_branch_gauges(&state.db).await;

    let reparented: Vec<Value> = children
        .iter()
        .map(|(child_id, child_name)| {
            json!({
                "child_branch": child_name,
                "child_branch_rid": format!("ri.foundry.main.branch.{child_id}"),
                "new_parent": new_parent_name,
                "new_parent_rid": new_parent_rid,
            })
        })
        .collect();

    {
        use crate::domain::branch_events::{BranchEnvelope, EVT_DELETED, emit};
        let dataset_rid = runtime(&state)
            .load_dataset_rid(dataset_id)
            .await
            .map_err(internal)?;
        let mut tx = state.db.begin().await.map_err(internal)?;
        let envelope = BranchEnvelope::new(EVT_DELETED, &target.rid, &dataset_rid, &user.0.sub.to_string())
            .with_parent_rid(target.parent_branch_id.map(|id| format!("ri.foundry.main.branch.{id}")))
            .with_extras(json!({ "reparented_children": reparented }));
        emit(&mut tx, &envelope)
            .await
            .map_err(|e| internal(e.to_string()))?;
        tx.commit().await.map_err(internal)?;
    }

    tracing::info!(
        rid,
        branch = %target.name,
        children = children.len(),
        "branch soft-deleted (children re-parented)"
    );
    crate::security::emit_audit(
        &user.0.sub,
        "branch.deleted",
        &rid,
        json!({
            "branch_rid": target.rid,
            "branch": target.name,
            "branch_id": target.id,
            "reparented_children": reparented,
        }),
    );
    Ok(Json(json!({
        "branch": target.name,
        "branch_rid": target.rid,
        "reparented": reparented,
    })))
}

/// `GET /v1/datasets/{rid}/branches/{branch}/ancestry` — returns the
/// ancestry chain from the requested branch up to its root, in
/// child→root order.
pub async fn branch_ancestry(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<Vec<Value>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let chain = runtime(&state)
        .list_branch_ancestry(target.id)
        .await
        .map_err(internal)?;
    let payload: Vec<Value> = chain
        .into_iter()
        .map(|b| {
            json!({
                "rid": b.rid,
                "name": b.name,
                "is_root": b.parent_branch_id.is_none(),
            })
        })
        .collect();
    Ok(Json(payload))
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
    crate::security::require_dataset_write(&user.0, &rid, "branch.reparent")?;
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

    let from_parent_id = target.parent_branch_id;
    let row = runtime(&state)
        .reparent_branch(target.id, new_parent_id)
        .await
        .map_err(internal)?;

    {
        use crate::domain::branch_events::{BranchEnvelope, EVT_REPARENTED, emit};
        let dataset_rid = runtime(&state)
            .load_dataset_rid(dataset_id)
            .await
            .map_err(internal)?;
        let mut tx = state.db.begin().await.map_err(internal)?;
        let envelope = BranchEnvelope::new(EVT_REPARENTED, &row.rid, &dataset_rid, &user.0.sub.to_string())
            .with_parent_rid(new_parent_id.map(|id| format!("ri.foundry.main.branch.{id}")))
            .with_head(row.head_transaction_id.map(|id| format!("ri.foundry.main.transaction.{id}")))
            .with_extras(json!({
                "from_parent_rid": from_parent_id.map(|id| format!("ri.foundry.main.branch.{id}")),
                "to_parent_rid": new_parent_id.map(|id| format!("ri.foundry.main.branch.{id}")),
            }));
        emit(&mut tx, &envelope)
            .await
            .map_err(|e| internal(e.to_string()))?;
        tx.commit().await.map_err(internal)?;
    }

    tracing::info!(rid, branch = %row.name, new_parent = ?new_parent_id, "branch reparented");
    crate::security::emit_audit(
        "system",
        "branch.reparent",
        &rid,
        json!({
            "branch": row.name,
            "new_parent_id": new_parent_id,
        }),
    );
    Ok(Json(row))
}

pub async fn rollback_branch(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
    Json(body): Json<RollbackBody>,
) -> Result<Json<Value>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "branch.rollback")?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let rollback_txn = runtime(&state)
        .fetch_transaction_by_branch_id(dataset_id, target.id, body.transaction_id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "rollback transaction not found" })),
            )
        })?;
    if rollback_txn.status != "COMMITTED" {
        return Err(bad_request("rollback target must be COMMITTED"));
    }
    let at = rollback_txn.committed_at.or(Some(rollback_txn.started_at));
    let historical = compute_view_at(&state, dataset_id, &branch, at).await?;

    let writer = TransactionalDatasetWriter::new(state.db.clone());
    let opened = writer
        .open_transaction(
            dataset_id,
            target.id,
            &target.name,
            TransactionType::Snapshot,
            body.summary.as_deref().unwrap_or("rollback"),
            &json!({
                "rollback_from_transaction": body.transaction_id,
                "rollback_from_view": historical.id,
            }),
            Some(user.0.sub),
        )
        .await
        .map_err(map_commit_error)?;

    for file in &historical.files {
        writer
            .stage_file(
                &opened,
                crate::storage::transactional::StagedFile {
                    logical_path: file.logical_path.clone(),
                    physical_path: file.physical_path.clone(),
                    size_bytes: file.size_bytes,
                    op: crate::storage::transactional::StageOp::Add,
                },
            )
            .await
            .map_err(map_commit_error)?;
    }
    writer.commit(&opened).await.map_err(map_commit_error)?;
    let row = runtime(&state)
        .fetch_transaction_by_branch_id(dataset_id, target.id, opened.id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "rollback transaction not found after commit" })),
            )
        })?;
    let current = compute_view_at(&state, dataset_id, &branch, None).await?;

    crate::security::emit_audit(
        &user.0.sub,
        "branch.rollback",
        &rid,
        json!({
            "branch": branch,
            "rollback_from_transaction": body.transaction_id,
            "transaction_id": row.id,
        }),
    );

    Ok(Json(json!({
        "transaction": row,
        "view": current,
    })))
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
    crate::security::require_dataset_write(&user.0, &rid, "transaction.open")?;
    let tx_type =
        TxType::from_str(&body.tx_type).map_err(|_| bad_request("invalid transaction type"))?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let summary = body.summary.unwrap_or_default();

    let writer = TransactionalDatasetWriter::new(state.db.clone());
    let opened_result = writer
        .open_transaction(
            dataset_id,
            target.id,
            &target.name,
            tx_type.as_model(),
            &summary,
            &body.providence,
            Some(user.0.sub),
        )
        .await;
    let opened = match opened_result {
        Ok(o) => o,
        Err(crate::domain::transactions::CommitError::ConcurrentOpenTransaction { branch }) => {
            // P1 — surface the open transaction RID so the UI can
            // route the user straight to it instead of asking them to
            // refetch the branch state.
            let open_txn_id = runtime(&state)
                .open_transaction_for_branch(target.id)
                .await
                .map_err(internal)?;
            let open_txn_rid =
                open_txn_id.map(|id| format!("ri.foundry.main.transaction.{id}"));
            return Err((
                StatusCode::CONFLICT,
                Json(json!({
                    "error": "BRANCH_HAS_OPEN_TRANSACTION",
                    "message": "branch already has an OPEN transaction",
                    "branch": branch,
                    "open_transaction_id": open_txn_id,
                    "open_transaction_rid": open_txn_rid,
                })),
            ));
        }
        Err(other) => return Err(internal(other)),
    };

    let row = runtime(&state)
        .fetch_transaction_by_branch_id(dataset_id, target.id, opened.id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "transaction not found after open", "txn": opened.id })),
            )
        })?;

    // T8.1 — bump the per-(dataset, branch) OPEN gauge. The matching
    // dec lives in `transaction_action` for the commit/abort paths.
    crate::metrics::open_gauge(&rid, &target.name).inc();
    crate::metrics::DATASET_TX_TOTAL
        .with_label_values(&["open"])
        .inc();
    let _ = crate::metrics::refresh_branch_gauges(&state.db).await;

    crate::security::emit_audit(
        &user.0.sub,
        "transaction.open",
        &rid,
        json!({
            "branch": target.name,
            "transaction_id": row.id,
            "tx_type": tx_type.as_str(),
        }),
    );

    Ok((StatusCode::CREATED, Json(row)))
}

/// `POST /v1/datasets/{rid}/transactions:batchGet`
///
/// P6 — Foundry "Application reference" 207 Multi-Status batch read.
/// Body shape:
///
/// ```json
/// { "ids": ["<txn_uuid_1>", "<txn_uuid_2>", "..."] }
/// ```
///
/// Returns one [`BatchItemResult`] per input id; per-item status
/// mirrors the per-resource GET (200 / 404, plus 400 for malformed
/// uuids) so callers can fan-out without N round-trips.
#[derive(Deserialize)]
pub struct BatchGetTransactionsBody {
    pub ids: Vec<String>,
}

pub async fn batch_get_transactions(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Json(body): Json<BatchGetTransactionsBody>,
) -> Result<Response, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let mut items: Vec<BatchItemResult<TransactionOut>> = Vec::with_capacity(body.ids.len());
    for raw_id in body.ids {
        match Uuid::parse_str(&raw_id) {
            Ok(txn_id) => {
                let row = runtime(&state)
                    .fetch_transaction(dataset_id, txn_id)
                    .await
                    .map_err(internal)?;
                match row {
                    Some(tx) => items.push(BatchItemResult {
                        status: 200,
                        id: raw_id,
                        data: Some(tx),
                        error: None,
                    }),
                    None => items.push(BatchItemResult {
                        status: 404,
                        id: raw_id,
                        data: None,
                        error: Some(ErrorEnvelope::new(
                            "TRANSACTION_NOT_FOUND",
                            "transaction not found",
                        )),
                    }),
                }
            }
            Err(_) => items.push(BatchItemResult {
                status: 400,
                id: raw_id,
                data: None,
                error: Some(ErrorEnvelope::new(
                    "TRANSACTION_BAD_ID",
                    "transaction id is not a valid UUID",
                )),
            }),
        }
    }
    Ok(batch_response(items))
}

pub async fn get_transaction(
    State(state): State<AppState>,
    _user: AuthUser,
    headers: HeaderMap,
    Path((rid, branch, txn)): Path<(String, String, String)>,
) -> Result<Response, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let txn_id =
        Uuid::parse_str(&txn).map_err(|_| bad_request("transaction id is not a valid UUID"))?;

    let row = runtime(&state)
        .fetch_transaction_by_branch_name(dataset_id, &branch, txn_id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "transaction not found" })),
            )
        })?;
    let value = serde_json::to_value(&row).map_err(|e| internal(e.to_string()))?;
    Ok(json_with_etag(&headers, value))
}

/// Despachador para `POST /transactions/{txn}:commit` y `:abort`.
///
/// Las invariantes de negocio (`OPEN-only`, APPEND/UPDATE/DELETE/SNAPSHOT
/// rules) viven en `crate::domain::transactions`; este handler sólo enruta,
/// resuelve el dataset, llama al dominio y mapea el resultado a HTTP.
pub async fn transaction_action(
    State(state): State<AppState>,
    user: AuthUser,
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
    // Gate as soon as we know which action was requested so the metric
    // label matches the audit `action` field.
    let op_label: &'static str = match action {
        "commit" => "transaction.commit",
        "abort" => "transaction.abort",
        _ => return Err(bad_request("unsupported transaction action")),
    };
    crate::security::require_dataset_write(&user.0, &rid, op_label)?;
    let txn_id =
        Uuid::parse_str(txn_str).map_err(|_| bad_request("transaction id is not a valid UUID"))?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let before = runtime(&state)
        .fetch_transaction_by_branch_name(dataset_id, &branch, txn_id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "transaction not found", "txn": txn_id })),
            )
        })?;
    let was_open = before.status == "OPEN";

    let result = match action {
        "commit" => txn_domain::commit_transaction(&state.db, txn_id).await,
        "abort" => txn_domain::abort_transaction(&state.db, txn_id).await,
        _ => return Err(bad_request("unsupported transaction action")),
    };

    if let Err(err) = result {
        return Err(map_commit_error(err));
    }

    // Re-fetch the canonical row (and verify branch/dataset scoping).
    let row = runtime(&state)
        .fetch_transaction_by_branch_name(dataset_id, &branch, txn_id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "transaction not found after action", "txn": txn_id })),
            )
        })?;

    // T8.1 — terminal-state transitions: dec the OPEN gauge and bump
    // the appropriate counter. Done after the re-fetch so we can label
    // the committed counter by the actual `tx_type` recorded in DB.
    if was_open {
        crate::metrics::open_gauge(&rid, &branch).dec();
    }
    match action {
        "commit" => {
            crate::metrics::DATASET_TRANSACTIONS_COMMITTED_TOTAL
                .with_label_values(&[row.tx_type.as_str()])
                .inc();
            crate::metrics::DATASET_TX_TOTAL
                .with_label_values(&["commit"])
                .inc();
        }
        "abort" => {
            crate::metrics::DATASET_TRANSACTIONS_ABORTED_TOTAL.inc();
            crate::metrics::DATASET_TX_TOTAL
                .with_label_values(&["abort"])
                .inc();
        }
        _ => unreachable!("action filtered above"),
    }
    let _ = crate::metrics::refresh_branch_gauges(&state.db).await;

    crate::security::emit_audit(
        &user.0.sub,
        op_label,
        &rid,
        json!({
            "branch": branch,
            "transaction_id": txn_id,
            "tx_type": row.tx_type.as_str(),
        }),
    );

    Ok(Json(row))
}

fn map_commit_error(err: crate::domain::transactions::CommitError) -> (StatusCode, Json<Value>) {
    use crate::domain::transactions::CommitError;
    match err {
        CommitError::NotFound => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "transaction not found" })),
        ),
        CommitError::ConcurrentOpenTransaction { branch } => (
            StatusCode::CONFLICT,
            Json(json!({
                "error": "branch already has an OPEN transaction",
                "branch": branch,
            })),
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
    Query(page): Query<PageQuery>,
) -> Result<Json<Page<TransactionOut>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let before = match params.before.as_deref() {
        Some(s) => Some(
            DateTime::parse_from_rfc3339(s)
                .map_err(|_| bad_request("'before' must be RFC3339"))?
                .with_timezone(&Utc),
        ),
        None => None,
    };

    let rows = runtime(&state)
        .list_transactions(dataset_id, params.branch.as_deref(), before)
        .await
        .map_err(internal)?;
    Ok(Json(slice_into_page(rows, &page)))
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
    let at = if let Some(txn_id) = params.transaction_id {
        Some(committed_transaction_time(&state, dataset_id, &params.branch, txn_id).await?)
    } else {
        match params.ts.as_deref() {
            Some(s) => Some(
                DateTime::parse_from_rfc3339(s)
                    .map_err(|_| bad_request("'ts' must be RFC3339"))?
                    .with_timezone(&Utc),
            ),
            None => None,
        }
    };
    compute_view_at(&state, dataset_id, &params.branch, at)
        .await
        .map(Json)
}

pub async fn compare_views(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Query(params): Query<CompareQuery>,
) -> Result<Json<CompareOut>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let base_branch = params.base_branch.unwrap_or_else(default_branch_master);
    let target_branch = params.target_branch.unwrap_or_else(|| base_branch.clone());
    let base_at = match params.base_transaction {
        Some(txn_id) => {
            Some(committed_transaction_time(&state, dataset_id, &base_branch, txn_id).await?)
        }
        None => None,
    };
    let target_at = match params.target_transaction {
        Some(txn_id) => {
            Some(committed_transaction_time(&state, dataset_id, &target_branch, txn_id).await?)
        }
        None => None,
    };

    let base = compute_view_at(&state, dataset_id, &base_branch, base_at).await?;
    let target = compute_view_at(&state, dataset_id, &target_branch, target_at).await?;
    let files = diff_views(&base.files, &target.files);

    Ok(Json(CompareOut {
        base,
        target,
        files,
    }))
}

pub async fn list_view_files(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, view_id)): Path<(String, String)>,
) -> Result<Json<Vec<ViewFileOut>>, (StatusCode, Json<Value>)> {
    let _ = resolve_dataset_id(&state, &rid).await?;
    let view_uuid =
        Uuid::parse_str(&view_id).map_err(|_| bad_request("view_id is not a valid UUID"))?;

    let rows = runtime(&state)
        .list_view_files(view_uuid)
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
    // T8.1 — observe the wall-clock cost of materialising a branch view.
    // The timer drops at the end of the function and records into
    // `dataset_view_compute_duration_seconds`.
    let _view_timer = crate::metrics::DATASET_VIEW_COMPUTE_DURATION_SECONDS.start_timer();
    let runtime = runtime(state);
    let requested = load_branch(state, dataset_id, branch).await?;
    let (target, fallback_chain, txns) =
        resolve_branch_view(state, dataset_id, requested, at).await?;

    // Localizar último SNAPSHOT (≤ at) y arrancar desde allí.
    let start_idx = txns
        .iter()
        .rposition(|record| record.tx_type == "SNAPSHOT")
        .unwrap_or(0);

    use std::collections::BTreeMap;
    let mut files: BTreeMap<String, ViewFileOut> = BTreeMap::new();

    for txn in txns.iter().skip(start_idx) {
        let rows = runtime
            .list_transaction_files(txn.id)
            .await
            .map_err(internal)?;

        match txn.tx_type.as_str() {
            "SNAPSHOT" => {
                files.clear();
                for r in rows {
                    let lp = r.logical_path;
                    files.insert(
                        lp.clone(),
                        ViewFileOut {
                            logical_path: lp,
                            physical_path: r.physical_path,
                            size_bytes: r.size_bytes,
                            introduced_by: Some(txn.id),
                        },
                    );
                }
            }
            "APPEND" => {
                for r in rows {
                    let lp = r.logical_path;
                    files.entry(lp.clone()).or_insert(ViewFileOut {
                        logical_path: lp,
                        physical_path: r.physical_path,
                        size_bytes: r.size_bytes,
                        introduced_by: Some(txn.id),
                    });
                }
            }
            "UPDATE" => {
                for r in rows {
                    if r.op == "REMOVE" {
                        files.remove(&r.logical_path);
                    } else {
                        let lp = r.logical_path;
                        files.insert(
                            lp.clone(),
                            ViewFileOut {
                                logical_path: lp,
                                physical_path: r.physical_path,
                                size_bytes: r.size_bytes,
                                introduced_by: Some(txn.id),
                            },
                        );
                    }
                }
            }
            "DELETE" => {
                for r in rows {
                    files.remove(&r.logical_path);
                }
            }
            _ => {}
        }
    }

    let head_txn_id = txns.last().map(|txn| txn.id).unwrap_or(Uuid::nil());
    let file_count = files.len() as i32;
    let size_bytes: i64 = files.values().map(|f| f.size_bytes).sum();

    // Persistir la vista para futuras consultas O(1) (idempotente por
    // (dataset, branch, head_transaction)).
    let view_id = if head_txn_id != Uuid::nil() {
        let persisted = runtime
            .upsert_view(dataset_id, target.id, head_txn_id, file_count, size_bytes)
            .await
            .map_err(internal)?;
        let materialized_files: Vec<ViewFileOut> = files.values().cloned().collect();
        runtime
            .replace_view_files(persisted, &materialized_files)
            .await
            .map_err(internal)?;
        persisted
    } else {
        Uuid::nil()
    };

    Ok(ViewOut {
        id: view_id,
        dataset_id,
        branch_id: target.id,
        head_transaction_id: head_txn_id,
        requested_branch: branch.to_string(),
        resolved_branch: target.name,
        fallback_chain,
        computed_at: Utc::now(),
        file_count,
        size_bytes,
        files: files.into_values().collect(),
    })
}

async fn committed_transaction_time(
    state: &AppState,
    dataset_id: Uuid,
    branch: &str,
    txn_id: Uuid,
) -> Result<DateTime<Utc>, (StatusCode, Json<Value>)> {
    let row = runtime(state)
        .fetch_transaction_by_branch_name(dataset_id, branch, txn_id)
        .await
        .map_err(internal)?
        .ok_or_else(|| {
            (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "transaction not found", "txn": txn_id })),
            )
        })?;
    if row.status != "COMMITTED" {
        return Err(bad_request("view-at-time transaction must be COMMITTED"));
    }
    Ok(row.committed_at.unwrap_or(row.started_at))
}

async fn resolve_branch_view(
    state: &AppState,
    dataset_id: Uuid,
    requested: BranchOut,
    at: Option<DateTime<Utc>>,
) -> Result<
    (
        BranchOut,
        Vec<String>,
        Vec<crate::storage::runtime::ViewTransactionRecord>,
    ),
    (StatusCode, Json<Value>),
> {
    let runtime = runtime(state);
    let mut current = requested;
    let mut chain = Vec::new();
    let mut seen = HashSet::new();

    loop {
        if !seen.insert(current.name.clone()) {
            return Err(bad_request("fallback chain contains a cycle"));
        }
        let txns = runtime
            .list_committed_branch_transactions(current.id, at)
            .await
            .map_err(internal)?;
        if !txns.is_empty() {
            return Ok((current, chain, txns));
        }

        let fallbacks = runtime.list_fallbacks(current.id).await.map_err(internal)?;
        let Some(next) = fallbacks.first() else {
            return Ok((current, chain, txns));
        };
        chain.push(next.fallback_branch_name.clone());
        current = load_branch(state, dataset_id, &next.fallback_branch_name).await?;
    }
}

fn diff_views(base: &[ViewFileOut], target: &[ViewFileOut]) -> FileDiff {
    use std::collections::BTreeMap;

    let base_by_path: BTreeMap<String, ViewFileOut> = base
        .iter()
        .map(|file| (file.logical_path.clone(), file.clone()))
        .collect();
    let target_by_path: BTreeMap<String, ViewFileOut> = target
        .iter()
        .map(|file| (file.logical_path.clone(), file.clone()))
        .collect();

    let mut added = Vec::new();
    let mut removed = Vec::new();
    let mut modified = Vec::new();

    for (path, target_file) in &target_by_path {
        match base_by_path.get(path) {
            Some(base_file)
                if base_file.physical_path != target_file.physical_path
                    || base_file.size_bytes != target_file.size_bytes =>
            {
                modified.push(FileChange {
                    logical_path: path.clone(),
                    before: base_file.clone(),
                    after: target_file.clone(),
                });
            }
            Some(_) => {}
            None => added.push(target_file.clone()),
        }
    }

    for (path, base_file) in &base_by_path {
        if !target_by_path.contains_key(path) {
            removed.push(base_file.clone());
        }
    }

    FileDiff {
        added,
        removed,
        modified,
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Fallback chain (T2.3 backing data).
//
// Cada rama puede tener una cadena ordenada de "ramas vecinas" a las que el
// build se cae cuando un dataset de entrada no contiene la rama solicitada.
// El orden lo da `position` ascendente (0 gana primero).
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct PutFallbacksBody {
    /// Lista ordenada de nombres de rama. Reemplaza la cadena actual de forma
    /// at\u00f3mica (DELETE + INSERT en una transacci\u00f3n).
    #[serde(default, alias = "fallbacks")]
    pub chain: Vec<String>,
}

pub async fn list_fallbacks(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
) -> Result<Json<Vec<FallbackEntry>>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let rows = runtime(&state)
        .list_fallbacks(target.id)
        .await
        .map_err(internal)?;
    Ok(Json(rows))
}

pub async fn put_fallbacks(
    State(state): State<AppState>,
    user: AuthUser,
    Path((rid, branch)): Path<(String, String)>,
    Json(body): Json<PutFallbacksBody>,
) -> Result<Json<Vec<FallbackEntry>>, (StatusCode, Json<Value>)> {
    crate::security::require_dataset_write(&user.0, &rid, "branch.fallbacks.update")?;
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let target = load_branch(&state, dataset_id, &branch).await?;
    let mut normalized = Vec::with_capacity(body.chain.len());
    let mut direct_seen = HashSet::new();
    // Sanity: ningún nombre vacío y sin auto-referencias triviales.
    for name in &body.chain {
        let trimmed = name.trim().to_string();
        if trimmed.is_empty() {
            return Err(bad_request("fallback chain entries must be non-empty"));
        }
        if trimmed == target.name {
            return Err(bad_request(
                "fallback chain cannot reference the branch itself",
            ));
        }
        if !direct_seen.insert(trimmed.clone()) {
            return Err(bad_request(
                "fallback chain cannot contain duplicate branches",
            ));
        }
        normalized.push(trimmed);
    }
    if fallback_cycle_would_form(&state, dataset_id, &target.name, &normalized).await? {
        return Err(bad_request("fallback chain contains a cycle"));
    }

    runtime(&state)
        .replace_fallbacks(target.id, &normalized)
        .await
        .map_err(internal)?;

    let rows = runtime(&state)
        .list_fallbacks(target.id)
        .await
        .map_err(internal)?;
    crate::security::emit_audit(
        &user.0.sub,
        "branch.fallbacks.update",
        &rid,
        json!({
            "branch": target.name,
            "chain": normalized,
        }),
    );
    Ok(Json(rows))
}

async fn fallback_cycle_would_form(
    state: &AppState,
    dataset_id: Uuid,
    target_branch: &str,
    new_chain: &[String],
) -> Result<bool, (StatusCode, Json<Value>)> {
    let runtime = runtime(state);
    let mut stack: Vec<String> = new_chain.to_vec();
    let mut seen = HashSet::new();

    while let Some(branch_name) = stack.pop() {
        if branch_name == target_branch {
            return Ok(true);
        }
        if !seen.insert(branch_name.clone()) {
            continue;
        }
        let Some(branch) = runtime
            .load_active_branch(dataset_id, &branch_name)
            .await
            .map_err(internal)?
        else {
            continue;
        };
        for fallback in runtime.list_fallbacks(branch.id).await.map_err(internal)? {
            stack.push(fallback.fallback_branch_name);
        }
    }

    Ok(false)
}
