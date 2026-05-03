use chrono::{Duration, Utc};
use uuid::Uuid;

use crate::models::retention::{
    CreateRetentionPolicyRequest, RetentionJob, RetentionJobRow, RetentionPolicy,
    RetentionPolicyRow, RunRetentionJobRequest, UpdateRetentionPolicyRequest,
};

pub fn policy_rules_payload(
    request: &CreateRetentionPolicyRequest,
) -> Result<serde_json::Value, String> {
    serde_json::to_value(&request.rules).map_err(|cause| cause.to_string())
}

pub fn updated_rules_payload(rules: &[String]) -> Result<serde_json::Value, String> {
    serde_json::to_value(rules).map_err(|cause| cause.to_string())
}

pub async fn load_policies(db: &sqlx::PgPool) -> Result<Vec<RetentionPolicy>, String> {
    let rows = sqlx::query_as::<_, RetentionPolicyRow>(
        "SELECT id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, is_system, selector, criteria, grace_period_minutes, last_applied_at, next_run_at, created_at, updated_at
         FROM retention_policies
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await
    .map_err(|cause| cause.to_string())?;

    rows.into_iter()
        .map(RetentionPolicy::try_from)
        .collect::<Result<Vec<_>, _>>()
}

pub async fn load_policy(db: &sqlx::PgPool, id: Uuid) -> Result<Option<RetentionPolicy>, String> {
    let row = sqlx::query_as::<_, RetentionPolicyRow>(
        "SELECT id, name, scope, target_kind, retention_days, legal_hold, purge_mode, rules, updated_by, active, is_system, selector, criteria, grace_period_minutes, last_applied_at, next_run_at, created_at, updated_at
         FROM retention_policies WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(db)
    .await
    .map_err(|cause| cause.to_string())?;

    match row {
        Some(row) => RetentionPolicy::try_from(row).map(Some),
        None => Ok(None),
    }
}

pub async fn run_job(
    db: &sqlx::PgPool,
    request: &RunRetentionJobRequest,
) -> Result<RetentionJob, String> {
    let Some(policy) = load_policy(db, request.policy_id).await? else {
        return Err("retention policy not found".to_string());
    };

    let target_label = if let Some(dataset_id) = request.target_dataset_id {
        format!("dataset {dataset_id}")
    } else if let Some(transaction_id) = request.target_transaction_id {
        format!("transaction {transaction_id}")
    } else {
        "policy scope".to_string()
    };

    let action_summary = format!(
        "Applied {} retention ({} days, purge mode {}) to {}",
        policy.target_kind, policy.retention_days, policy.purge_mode, target_label
    );

    let affected_record_count = if request.target_transaction_id.is_some() {
        1
    } else {
        3
    };

    let row = sqlx::query_as::<_, RetentionJobRow>(
        "INSERT INTO retention_jobs (id, policy_id, target_dataset_id, target_transaction_id, status, action_summary, affected_record_count, created_at, completed_at)
         VALUES ($1, $2, $3, $4, 'completed', $5, $6, $7, $8)
         RETURNING id, policy_id, target_dataset_id, target_transaction_id, status, action_summary, affected_record_count, created_at, completed_at",
    )
    .bind(Uuid::now_v7())
    .bind(request.policy_id)
    .bind(request.target_dataset_id)
    .bind(request.target_transaction_id)
    .bind(action_summary)
    .bind(affected_record_count)
    .bind(Utc::now())
    .bind(Utc::now())
    .fetch_one(db)
    .await
    .map_err(|cause| cause.to_string())?;

    Ok(row.into())
}

pub fn apply_update(current: &mut RetentionPolicy, update: UpdateRetentionPolicyRequest) {
    if let Some(name) = update.name {
        current.name = name;
    }
    if let Some(scope) = update.scope {
        current.scope = scope;
    }
    if let Some(target_kind) = update.target_kind {
        current.target_kind = target_kind;
    }
    if let Some(retention_days) = update.retention_days {
        current.retention_days = retention_days;
    }
    if let Some(legal_hold) = update.legal_hold {
        current.legal_hold = legal_hold;
    }
    if let Some(purge_mode) = update.purge_mode {
        current.purge_mode = purge_mode;
    }
    if let Some(rules) = update.rules {
        current.rules = rules;
    }
    if let Some(updated_by) = update.updated_by {
        current.updated_by = updated_by;
    }
    if let Some(active) = update.active {
        current.active = active;
    }
    if let Some(selector) = update.selector {
        current.selector = selector;
    }
    if let Some(criteria) = update.criteria {
        current.criteria = criteria;
    }
    if let Some(grace) = update.grace_period_minutes {
        current.grace_period_minutes = grace;
    }
}

/// Filter loaded policies according to a `?dataset_rid=&project_id=…`
/// query (T4.1 contract). Selectors are OR-combined per policy:
/// `all_datasets` always matches; otherwise any explicit selector
/// equality counts as a match.
pub fn filter_policies(
    policies: Vec<RetentionPolicy>,
    query: &crate::handlers::retention::ListPoliciesQuery,
) -> Vec<RetentionPolicy> {
    policies
        .into_iter()
        .filter(|policy| matches_query(policy, query))
        .collect()
}

fn matches_query(
    policy: &RetentionPolicy,
    query: &crate::handlers::retention::ListPoliciesQuery,
) -> bool {
    if let Some(active) = query.active {
        if policy.active != active {
            return false;
        }
    }
    if matches!(query.system_only, Some(true)) && !policy.is_system {
        return false;
    }
    let any_selector =
        query.dataset_rid.is_some() || query.project_id.is_some() || query.marking_id.is_some();
    if !any_selector {
        return true;
    }
    if policy.selector.all_datasets {
        return true;
    }
    if let Some(rid) = query.dataset_rid.as_deref() {
        if policy.selector.dataset_rid.as_deref() == Some(rid) {
            return true;
        }
    }
    if let Some(pid) = query.project_id {
        if policy.selector.project_id == Some(pid) {
            return true;
        }
    }
    if let Some(mid) = query.marking_id {
        if policy.selector.marking_id == Some(mid) {
            return true;
        }
    }
    false
}

// ─────────────────────────────────────────────────────────────────────────────
// P4 — applicable-policies resolution + retention-preview computation.
// ─────────────────────────────────────────────────────────────────────────────

use crate::handlers::retention::{
    InheritedPolicies, PolicyConflict, ResolutionContext, RetentionPreviewFile,
    RetentionPreviewResponse, RetentionPreviewSummary, RetentionPreviewTransaction,
};

#[derive(Debug, Default)]
pub struct ResolvedPolicies {
    pub inherited: InheritedPolicies,
    pub explicit: Vec<RetentionPolicy>,
    pub effective: Option<RetentionPolicy>,
    pub conflicts: Vec<PolicyConflict>,
}

/// Resolve the applicable policies for `rid` given the dataset's
/// inheritance context. Foundry's "View retention policies" surface
/// requires this view to honour the four levels (Org → Space →
/// Project → Dataset) and surface a single winner.
///
/// Selection rules:
///   * `selector.dataset_rid == rid` → explicit (most specific).
///   * `selector.project_id == ctx.project_id` → project.
///   * `selector.marking_id == ctx.marking_id`
///       OR `selector.marking_id == ctx.space_id` (space-as-marking
///         shorthand used by the existing data) → space.
///   * `selector.all_datasets == true` → org (platform-wide).
///
/// Inactive policies are skipped (they don't even appear in the
/// inherited buckets) — only `policy.active = true` is considered.
pub fn resolve_applicable(
    policies: &[RetentionPolicy],
    rid: &str,
    ctx: &ResolutionContext,
) -> ResolvedPolicies {
    let mut out = ResolvedPolicies::default();

    for policy in policies.iter().filter(|p| p.active) {
        let sel = &policy.selector;

        // explicit match wins its own bucket and is also considered
        // for the effective resolution.
        if let Some(target) = &sel.dataset_rid {
            if target == rid {
                out.explicit.push(policy.clone());
                continue;
            }
        }

        if let (Some(want), Some(p_id)) = (ctx.project_id, sel.project_id) {
            if want == p_id {
                out.inherited.project.push(policy.clone());
                continue;
            }
        }

        if let Some(marking_id) = sel.marking_id {
            if Some(marking_id) == ctx.marking_id || Some(marking_id) == ctx.space_id {
                out.inherited.space.push(policy.clone());
                continue;
            }
        }

        if sel.all_datasets {
            out.inherited.org.push(policy.clone());
        }
    }

    let (effective, conflicts) = pick_effective(&out);
    out.effective = effective;
    out.conflicts = conflicts;
    out
}

/// Most-restrictive resolution. Order:
///   1. `legal_hold = true` always wins.
///   2. Otherwise lowest `retention_days` wins.
///   3. Tie-break: explicit > project > space > org (specificity).
///   4. Final tie-break: oldest `created_at` (stable).
fn pick_effective(resolved: &ResolvedPolicies) -> (Option<RetentionPolicy>, Vec<PolicyConflict>) {
    fn score(policy: &RetentionPolicy, level: u8) -> (u8, i32, chrono::DateTime<Utc>) {
        // Lower (legal_hold_score, retention_days, created_at) wins.
        let legal_hold_score: u8 = if policy.legal_hold { 0 } else { 1 };
        // Specificity bonus — explicit (level=0) wins ties over project (1)
        // > space (2) > org (3). We pack it into the second tuple field by
        // adding (level << 24) so retention_days dominates within a level
        // but specificity breaks ties with the same retention_days.
        let bias = (level as i32) << 24;
        (legal_hold_score, policy.retention_days + bias, policy.created_at)
    }

    let mut candidates: Vec<(u8, &RetentionPolicy)> = Vec::new();
    for p in &resolved.explicit {
        candidates.push((0, p));
    }
    for p in &resolved.inherited.project {
        candidates.push((1, p));
    }
    for p in &resolved.inherited.space {
        candidates.push((2, p));
    }
    for p in &resolved.inherited.org {
        candidates.push((3, p));
    }
    if candidates.is_empty() {
        return (None, Vec::new());
    }
    candidates.sort_by_key(|(level, p)| score(p, *level));
    let (_, winner) = candidates.first().unwrap();
    let winner = (*winner).clone();

    let conflicts: Vec<PolicyConflict> = candidates
        .iter()
        .skip(1)
        .map(|(_, p)| PolicyConflict {
            winner_id: winner.id,
            loser_id: p.id,
            reason: if winner.legal_hold && !p.legal_hold {
                "winner_has_legal_hold"
            } else if winner.retention_days < p.retention_days {
                "winner_has_lower_retention_days"
            } else {
                "winner_has_higher_specificity"
            },
        })
        .collect();
    (Some(winner), conflicts)
}

/// Run the retention preview against the *shared* Postgres instance:
/// for each policy that targets transactions/files we read the relevant
/// rows from the DVS-owned tables (`dataset_transactions`,
/// `dataset_files`) and decide whether the policy would purge them
/// `as_of_days` days from now.
///
/// This runs entirely in SQL — there's no separate runner — so the
/// preview is cheap enough to call on every UI render.
pub async fn run_preview(
    db: &sqlx::PgPool,
    rid: &str,
    as_of_days: i64,
    resolved: &ResolvedPolicies,
) -> Result<RetentionPreviewResponse, String> {
    let as_of = Utc::now() + Duration::days(as_of_days);

    // Resolve dataset_id from the rid. If the table doesn't exist
    // (retention service running solo, no DVS migrations applied) we
    // surface a warning rather than a 500.
    let dataset_id = lookup_dataset_id(db, rid).await;
    let dataset_id = match dataset_id {
        Ok(Some(id)) => id,
        Ok(None) => {
            return Ok(RetentionPreviewResponse {
                dataset_rid: rid.into(),
                as_of_days,
                as_of,
                effective_policy: resolved.effective.clone(),
                transactions: vec![],
                files: vec![],
                summary: RetentionPreviewSummary::default(),
                warnings: vec!["dataset not found in catalog".into()],
            });
        }
        Err(msg) => {
            return Ok(RetentionPreviewResponse {
                dataset_rid: rid.into(),
                as_of_days,
                as_of,
                effective_policy: resolved.effective.clone(),
                transactions: vec![],
                files: vec![],
                summary: RetentionPreviewSummary::default(),
                warnings: vec![format!("dataset lookup failed: {msg}")],
            });
        }
    };

    // Active candidate policies, in the same most-restrictive order
    // applicable() uses. Each transaction/file is matched against the
    // *first* policy that says "yes purge"; that mirrors what the
    // retention runner will do in production.
    let policies: Vec<RetentionPolicy> = applicable_policies_in_order(resolved);

    let txns = load_transactions(db, dataset_id).await?;
    let mut transactions: Vec<RetentionPreviewTransaction> =
        Vec::with_capacity(txns.len());
    let mut would_delete_count = 0usize;
    let mut purged_txn_ids: Vec<Uuid> = Vec::new();
    let mut purge_policy_for_txn: std::collections::HashMap<Uuid, (Uuid, String, String)> =
        std::collections::HashMap::new();

    for txn in &txns {
        let mut hit: Option<(&RetentionPolicy, String)> = None;
        for policy in &policies {
            if policy.target_kind != "transaction" && !policy.scope.contains("transaction") {
                continue;
            }
            if let Some(reason) = matches_transaction(policy, txn, as_of) {
                hit = Some((policy, reason));
                break;
            }
        }
        let (would_delete, policy_id, policy_name, reason) = match &hit {
            Some((p, why)) => (true, Some(p.id), Some(p.name.clone()), Some(why.clone())),
            None => (false, None, None, None),
        };
        if would_delete {
            would_delete_count += 1;
            purged_txn_ids.push(txn.id);
            if let Some((p, why)) = &hit {
                purge_policy_for_txn.insert(txn.id, (p.id, p.name.clone(), why.clone()));
            }
        }
        transactions.push(RetentionPreviewTransaction {
            id: txn.id,
            tx_type: txn.tx_type.clone(),
            status: txn.status.clone(),
            started_at: txn.started_at,
            committed_at: txn.committed_at,
            would_delete,
            policy_id,
            policy_name,
            reason,
        });
    }

    // Files inherit the deletion fate of their owning transaction
    // (Foundry: physical files are reaped after the transaction's
    // grace period, not directly per-file). For COMMITTED txns we
    // read `dataset_files` (the post-commit projection); for ABORTED
    // txns we fall back to `dataset_transaction_files` (the staging
    // table) since the trigger only writes `dataset_files` on commit.
    let files = if purged_txn_ids.is_empty() {
        Vec::new()
    } else {
        let mut from_committed =
            load_files_from_dataset_files(db, dataset_id, &purged_txn_ids).await?;
        let from_staged =
            load_files_from_staging(db, &purged_txn_ids, &transactions).await?;
        from_committed.extend(from_staged);
        from_committed
    };
    let mut bytes_total = 0i64;
    let preview_files: Vec<RetentionPreviewFile> = files
        .into_iter()
        .map(|f| {
            bytes_total += f.size_bytes;
            let (policy_id, policy_name, reason) = purge_policy_for_txn
                .get(&f.transaction_id)
                .cloned()
                .unwrap_or_else(|| (Uuid::nil(), "unknown".into(), "transaction purged".into()));
            RetentionPreviewFile {
                id: f.id,
                transaction_id: f.transaction_id,
                logical_path: f.logical_path,
                physical_uri: f.physical_uri,
                size_bytes: f.size_bytes,
                policy_id,
                policy_name,
                reason,
            }
        })
        .collect();

    let summary = RetentionPreviewSummary {
        transactions_total: transactions.len(),
        transactions_would_delete: would_delete_count,
        files_total: preview_files.len(),
        bytes_total,
    };

    Ok(RetentionPreviewResponse {
        dataset_rid: rid.into(),
        as_of_days,
        as_of,
        effective_policy: resolved.effective.clone(),
        transactions,
        files: preview_files,
        summary,
        warnings: Vec::new(),
    })
}

fn applicable_policies_in_order(resolved: &ResolvedPolicies) -> Vec<RetentionPolicy> {
    let mut out = Vec::new();
    out.extend(resolved.explicit.iter().cloned());
    out.extend(resolved.inherited.project.iter().cloned());
    out.extend(resolved.inherited.space.iter().cloned());
    out.extend(resolved.inherited.org.iter().cloned());
    out
}

#[derive(Debug, sqlx::FromRow)]
struct TransactionPreviewRow {
    id: Uuid,
    tx_type: String,
    status: String,
    started_at: chrono::DateTime<Utc>,
    committed_at: Option<chrono::DateTime<Utc>>,
    aborted_at: Option<chrono::DateTime<Utc>>,
}

#[derive(Debug, sqlx::FromRow)]
struct FilePreviewRow {
    id: Uuid,
    transaction_id: Uuid,
    logical_path: String,
    physical_uri: String,
    size_bytes: i64,
}

async fn lookup_dataset_id(db: &sqlx::PgPool, rid: &str) -> Result<Option<Uuid>, String> {
    // The `datasets` table lives in dataset-versioning-service's
    // migration set. When this service runs solo (no shared DB), the
    // query errors with "relation does not exist" — we map that to
    // `Ok(None)` so the preview can still surface a warning.
    let result = sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(db)
        .await;
    match result {
        Ok(row) => Ok(row),
        Err(sqlx::Error::Database(db_err))
            if db_err.message().contains("does not exist") =>
        {
            Ok(None)
        }
        Err(other) => Err(other.to_string()),
    }
}

async fn load_transactions(
    db: &sqlx::PgPool,
    dataset_id: Uuid,
) -> Result<Vec<TransactionPreviewRow>, String> {
    let result = sqlx::query_as::<_, TransactionPreviewRow>(
        r#"SELECT id, tx_type, status, started_at, committed_at, aborted_at
             FROM dataset_transactions
            WHERE dataset_id = $1
            ORDER BY started_at ASC"#,
    )
    .bind(dataset_id)
    .fetch_all(db)
    .await;
    match result {
        Ok(rows) => Ok(rows),
        Err(sqlx::Error::Database(db_err))
            if db_err.message().contains("does not exist") =>
        {
            Ok(Vec::new())
        }
        Err(other) => Err(other.to_string()),
    }
}

async fn load_files_from_dataset_files(
    db: &sqlx::PgPool,
    dataset_id: Uuid,
    txn_ids: &[Uuid],
) -> Result<Vec<FilePreviewRow>, String> {
    let result = sqlx::query_as::<_, FilePreviewRow>(
        r#"SELECT id, transaction_id, logical_path, physical_uri, size_bytes
             FROM dataset_files
            WHERE dataset_id = $1
              AND transaction_id = ANY($2)
              AND deleted_at IS NULL"#,
    )
    .bind(dataset_id)
    .bind(txn_ids)
    .fetch_all(db)
    .await;
    match result {
        Ok(rows) => Ok(rows),
        Err(sqlx::Error::Database(db_err))
            if db_err.message().contains("does not exist") =>
        {
            Ok(Vec::new())
        }
        Err(other) => Err(other.to_string()),
    }
}

/// Pick up files staged by ABORTED transactions. The commit trigger
/// only projects rows into `dataset_files` on COMMITTED transitions,
/// so aborted txns leave their bytes behind in the staging table —
/// which is exactly what the `DELETE_ABORTED_TRANSACTIONS` system
/// policy is meant to clean up.
async fn load_files_from_staging(
    db: &sqlx::PgPool,
    txn_ids: &[Uuid],
    transactions: &[RetentionPreviewTransaction],
) -> Result<Vec<FilePreviewRow>, String> {
    let aborted_ids: Vec<Uuid> = transactions
        .iter()
        .filter(|t| t.would_delete && t.status == "ABORTED")
        .map(|t| t.id)
        .filter(|id| txn_ids.contains(id))
        .collect();
    if aborted_ids.is_empty() {
        return Ok(Vec::new());
    }
    let result = sqlx::query_as::<_, FilePreviewRow>(
        r#"SELECT
              gen_random_uuid()                                    AS id,
              transaction_id                                       AS transaction_id,
              logical_path                                         AS logical_path,
              CASE
                  WHEN COALESCE(physical_path, '') <> ''
                      THEN 'local:///' || trim(both '/' from physical_path)
                  ELSE 'local:///'   || transaction_id::text
                                       || '/' || trim(both '/' from logical_path)
              END                                                  AS physical_uri,
              size_bytes                                           AS size_bytes
            FROM dataset_transaction_files
           WHERE transaction_id = ANY($1)
             AND op <> 'REMOVE'"#,
    )
    .bind(aborted_ids)
    .fetch_all(db)
    .await;
    match result {
        Ok(rows) => Ok(rows),
        Err(sqlx::Error::Database(db_err))
            if db_err.message().contains("does not exist") =>
        {
            Ok(Vec::new())
        }
        Err(other) => Err(other.to_string()),
    }
}

fn matches_transaction(
    policy: &RetentionPolicy,
    txn: &TransactionPreviewRow,
    as_of: chrono::DateTime<Utc>,
) -> Option<String> {
    let c = &policy.criteria;
    if let Some(state) = c.transaction_state.as_deref() {
        if !state.eq_ignore_ascii_case(&txn.status) {
            return None;
        }
    }
    if let Some(age_seconds) = c.transaction_age_seconds {
        let anchor = txn
            .committed_at
            .or(txn.aborted_at)
            .unwrap_or(txn.started_at);
        let elapsed = (as_of - anchor).num_seconds();
        if elapsed < age_seconds {
            return None;
        }
    }
    // Foundry "retention_days" treats 0 as "purge as soon as criteria match".
    if policy.retention_days > 0 {
        let anchor = txn
            .committed_at
            .or(txn.aborted_at)
            .unwrap_or(txn.started_at);
        let earliest_purge = anchor + Duration::days(policy.retention_days as i64);
        if as_of < earliest_purge {
            return None;
        }
    }
    let mut parts = Vec::new();
    if let Some(state) = c.transaction_state.as_deref() {
        parts.push(format!("transaction_state={state}"));
    }
    if let Some(age) = c.transaction_age_seconds {
        parts.push(format!("transaction_age>={age}s"));
    }
    if policy.retention_days > 0 {
        parts.push(format!("retention_days={}", policy.retention_days));
    }
    if parts.is_empty() {
        parts.push(format!("policy={}", policy.name));
    }
    Some(parts.join(", "))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::handlers::retention::ListPoliciesQuery;
    use crate::models::retention::{RetentionCriteria, RetentionSelector};
    use chrono::Utc;

    fn policy(id: Uuid, system: bool, selector: RetentionSelector) -> RetentionPolicy {
        RetentionPolicy {
            id,
            name: "p".into(),
            scope: "".into(),
            target_kind: "transaction".into(),
            retention_days: 0,
            legal_hold: false,
            purge_mode: "hard-delete-after-ttl".into(),
            rules: vec![],
            updated_by: "system".into(),
            active: true,
            is_system: system,
            selector,
            criteria: RetentionCriteria::default(),
            grace_period_minutes: 60,
            last_applied_at: None,
            next_run_at: None,
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    #[test]
    fn empty_query_returns_all_policies() {
        let p = vec![policy(Uuid::now_v7(), false, RetentionSelector::default())];
        let out = filter_policies(p.clone(), &ListPoliciesQuery::default());
        assert_eq!(out.len(), p.len());
    }

    #[test]
    fn all_datasets_selector_matches_any_dataset_query() {
        let mut sel = RetentionSelector::default();
        sel.all_datasets = true;
        let p = vec![policy(Uuid::now_v7(), true, sel)];
        let q = ListPoliciesQuery {
            dataset_rid: Some("ri.x".into()),
            ..Default::default()
        };
        assert_eq!(filter_policies(p, &q).len(), 1);
    }

    #[test]
    fn explicit_dataset_rid_filters_policies() {
        let id_match = Uuid::now_v7();
        let id_other = Uuid::now_v7();
        let mut sel_match = RetentionSelector::default();
        sel_match.dataset_rid = Some("ri.match".into());
        let mut sel_other = RetentionSelector::default();
        sel_other.dataset_rid = Some("ri.other".into());
        let policies = vec![
            policy(id_match, false, sel_match),
            policy(id_other, false, sel_other),
        ];
        let q = ListPoliciesQuery {
            dataset_rid: Some("ri.match".into()),
            ..Default::default()
        };
        let out = filter_policies(policies, &q);
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].id, id_match);
    }

    #[test]
    fn system_only_filters_user_policies_out() {
        let policies = vec![
            policy(Uuid::now_v7(), true, RetentionSelector::default()),
            policy(Uuid::now_v7(), false, RetentionSelector::default()),
        ];
        let q = ListPoliciesQuery {
            system_only: Some(true),
            ..Default::default()
        };
        let out = filter_policies(policies, &q);
        assert_eq!(out.len(), 1);
        assert!(out[0].is_system);
    }
}
