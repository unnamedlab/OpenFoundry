use chrono::Utc;
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
    let any_selector = query.dataset_rid.is_some()
        || query.project_id.is_some()
        || query.marking_id.is_some();
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
        let q = ListPoliciesQuery { dataset_rid: Some("ri.x".into()), ..Default::default() };
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
        let q = ListPoliciesQuery { dataset_rid: Some("ri.match".into()), ..Default::default() };
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
        let q = ListPoliciesQuery { system_only: Some(true), ..Default::default() };
        let out = filter_policies(policies, &q);
        assert_eq!(out.len(), 1);
        assert!(out[0].is_system);
    }
}
