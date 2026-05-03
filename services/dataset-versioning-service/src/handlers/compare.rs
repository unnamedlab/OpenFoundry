//! Branch comparison — Foundry "Branch comparison" surface.
//!
//! Computes the diff between two branches `A` and `B` of the same
//! dataset:
//!
//!   * `lca_branch_rid` — the lowest common ancestor on the
//!     ancestry tree (NULL when one of the branches is a root and
//!     the other lives on a separate trunk).
//!   * `a_only_transactions` / `b_only_transactions` — committed
//!     transactions strictly *after* the LCA on each side, in commit
//!     order.
//!   * `conflicting_files` — logical paths modified on both sides
//!     since the LCA. Conflict semantics mirror the Foundry doc
//!     "Rebasing and conflict resolution": the same key written on
//!     both branches is a conflict the operator must resolve before a
//!     merge / rebase can happen.
//!
//! Route: `GET /v1/datasets/{rid}/branches/compare?base=A&compare=B`.

use std::collections::{BTreeMap, HashMap, HashSet};

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::storage::{RuntimeStore, runtime::ViewTransactionRecord};

#[derive(Debug, Deserialize)]
pub struct CompareQuery {
    pub base: String,
    pub compare: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct TransactionSummary {
    pub transaction_rid: String,
    pub transaction_id: Uuid,
    pub branch: String,
    pub tx_type: String,
    pub status: String,
    pub committed_at: Option<DateTime<Utc>>,
    pub files_changed: usize,
}

#[derive(Debug, Serialize)]
pub struct ConflictingFile {
    pub logical_path: String,
    pub a_transaction_rid: String,
    pub b_transaction_rid: String,
    pub content_hash_a: Option<String>,
    pub content_hash_b: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct BranchCompareResponse {
    pub base_branch: String,
    pub compare_branch: String,
    pub lca_branch_rid: Option<String>,
    pub a_only_transactions: Vec<TransactionSummary>,
    pub b_only_transactions: Vec<TransactionSummary>,
    pub conflicting_files: Vec<ConflictingFile>,
}

pub async fn compare_branches(
    State(state): State<AppState>,
    _user: AuthUser,
    Path(rid): Path<String>,
    Query(query): Query<CompareQuery>,
) -> Result<Json<BranchCompareResponse>, (StatusCode, Json<Value>)> {
    if query.base == query.compare {
        return Err(bad_request("base and compare must differ"));
    }
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let runtime = RuntimeStore::new(state.db.clone());

    let base_branch = runtime
        .load_active_branch(dataset_id, &query.base)
        .await
        .map_err(internal)?
        .ok_or_else(|| not_found("base branch not found"))?;
    let compare_branch = runtime
        .load_active_branch(dataset_id, &query.compare)
        .await
        .map_err(internal)?
        .ok_or_else(|| not_found("compare branch not found"))?;

    let base_chain = runtime
        .list_branch_ancestry(base_branch.id)
        .await
        .map_err(internal)?;
    let compare_chain = runtime
        .list_branch_ancestry(compare_branch.id)
        .await
        .map_err(internal)?;

    // LCA: walk the base chain, return the first id present in the
    // compare chain. `list_branch_ancestry` returns child→root, so
    // walking it linearly visits the closest common ancestor first.
    let compare_ids: HashSet<Uuid> = compare_chain.iter().map(|b| b.id).collect();
    let lca = base_chain
        .iter()
        .find(|b| compare_ids.contains(&b.id))
        .cloned();
    let lca_branch_rid = lca.as_ref().map(|b| b.rid.clone());

    // The "after LCA" cutoff is the LCA branch's HEAD commit time.
    // When we have an LCA, we still want every transaction committed
    // *on the diverged side* — not on the LCA itself. Foundry's
    // doc-aligned semantics: a branch's "extra" commits are the ones
    // that happened on `branch.id`, not on any ancestor.
    //
    // For a branch B with ancestor LCA, the "B-only" set is the
    // transactions whose `branch_id == B.id` (i.e. created on B
    // itself). Transactions on the LCA branch belong to the shared
    // history.
    let a_only_raw = runtime
        .list_committed_branch_transactions(base_branch.id, None)
        .await
        .map_err(internal)?;
    let b_only_raw = runtime
        .list_committed_branch_transactions(compare_branch.id, None)
        .await
        .map_err(internal)?;

    // Hydrate file lists per transaction (committed-only). Cap the
    // hydration at a reasonable budget to keep the endpoint snappy
    // even for long-lived branches.
    const MAX_TX: usize = 200;
    let a_only = hydrate(&runtime, &base_branch.name, a_only_raw, MAX_TX).await?;
    let b_only = hydrate(&runtime, &compare_branch.name, b_only_raw, MAX_TX).await?;

    let conflicting_files = detect_conflicts(&a_only, &b_only);

    Ok(Json(BranchCompareResponse {
        base_branch: base_branch.name,
        compare_branch: compare_branch.name,
        lca_branch_rid,
        a_only_transactions: a_only.into_iter().map(|h| h.summary).collect(),
        b_only_transactions: b_only.into_iter().map(|h| h.summary).collect(),
        conflicting_files,
    }))
}

#[derive(Debug, Clone)]
struct Hydrated {
    summary: TransactionSummary,
    /// `logical_path → content_hash` for every file the transaction
    /// touched (ADD/REPLACE/REMOVE). Used by the conflict detector.
    files: BTreeMap<String, Option<String>>,
}

async fn hydrate(
    runtime: &RuntimeStore,
    branch_name: &str,
    rows: Vec<ViewTransactionRecord>,
    max: usize,
) -> Result<Vec<Hydrated>, (StatusCode, Json<Value>)> {
    let mut out = Vec::with_capacity(rows.len().min(max));
    for row in rows.into_iter().take(max) {
        let files = runtime
            .list_transaction_files(row.id)
            .await
            .map_err(internal)?;
        let mut file_map: BTreeMap<String, Option<String>> = BTreeMap::new();
        for f in files {
            // Use the physical path as a cheap surrogate for content
            // hash. The view-schema content_hash lives on a separate
            // table; we'll surface it as `null` when absent. The
            // detector flags the *fact* of co-modification, the UI
            // can fetch a richer diff on demand.
            file_map.insert(f.logical_path, Some(f.physical_path));
        }
        out.push(Hydrated {
            summary: TransactionSummary {
                transaction_rid: format!("ri.foundry.main.transaction.{}", row.id),
                transaction_id: row.id,
                branch: branch_name.to_string(),
                tx_type: row.tx_type.clone(),
                status: "COMMITTED".to_string(),
                committed_at: Some(row.ts),
                files_changed: file_map.len(),
            },
            files: file_map,
        });
    }
    Ok(out)
}

fn detect_conflicts(a: &[Hydrated], b: &[Hydrated]) -> Vec<ConflictingFile> {
    // Build per-side maps `logical_path → (transaction_rid, hash)`,
    // keeping the *latest* writer (last commit wins for surfacing
    // the conflict).
    let mut a_map: HashMap<String, (String, Option<String>)> = HashMap::new();
    for tx in a {
        for (path, hash) in &tx.files {
            a_map.insert(
                path.clone(),
                (tx.summary.transaction_rid.clone(), hash.clone()),
            );
        }
    }
    let mut conflicts = Vec::new();
    for tx in b {
        for (path, hash_b) in &tx.files {
            if let Some((rid_a, hash_a)) = a_map.get(path) {
                conflicts.push(ConflictingFile {
                    logical_path: path.clone(),
                    a_transaction_rid: rid_a.clone(),
                    b_transaction_rid: tx.summary.transaction_rid.clone(),
                    content_hash_a: hash_a.clone(),
                    content_hash_b: hash_b.clone(),
                });
            }
        }
    }
    // Stable order for the UI.
    conflicts.sort_by(|x, y| x.logical_path.cmp(&y.logical_path));
    conflicts
}

// ── helpers shared with `handlers/retention.rs` ───────────────────

async fn resolve_dataset_id(state: &AppState, rid: &str) -> Result<Uuid, (StatusCode, Json<Value>)> {
    if let Ok(id) = Uuid::parse_str(rid) {
        return Ok(id);
    }
    sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?
        .ok_or_else(|| not_found("dataset not found"))
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
}

fn not_found(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::NOT_FOUND, Json(json!({ "error": msg })))
}

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "branch compare error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn hyd(branch: &str, txn: u128, files: &[&str]) -> Hydrated {
        let id = Uuid::from_u128(txn);
        let mut map = BTreeMap::new();
        for path in files {
            map.insert((*path).to_string(), Some(format!("hash-{path}")));
        }
        Hydrated {
            summary: TransactionSummary {
                transaction_rid: format!("ri.foundry.main.transaction.{id}"),
                transaction_id: id,
                branch: branch.to_string(),
                tx_type: "APPEND".into(),
                status: "COMMITTED".into(),
                committed_at: Some(Utc.with_ymd_and_hms(2026, 5, 1, 0, 0, 0).unwrap()),
                files_changed: map.len(),
            },
            files: map,
        }
    }

    #[test]
    fn detects_conflict_on_overlapping_path() {
        let a = vec![hyd("feature", 1, &["data/users.parquet"])];
        let b = vec![hyd("master", 2, &["data/users.parquet"])];
        let conflicts = detect_conflicts(&a, &b);
        assert_eq!(conflicts.len(), 1);
        assert_eq!(conflicts[0].logical_path, "data/users.parquet");
    }

    #[test]
    fn no_conflict_when_paths_disjoint() {
        let a = vec![hyd("feature", 1, &["data/users.parquet"])];
        let b = vec![hyd("master", 2, &["data/products.parquet"])];
        assert!(detect_conflicts(&a, &b).is_empty());
    }

    #[test]
    fn conflicting_files_sorted_by_path() {
        let a = vec![hyd("feature", 1, &["b/x", "a/x"])];
        let b = vec![hyd("master", 2, &["a/x", "b/x"])];
        let conflicts = detect_conflicts(&a, &b);
        assert_eq!(conflicts[0].logical_path, "a/x");
        assert_eq!(conflicts[1].logical_path, "b/x");
    }
}
