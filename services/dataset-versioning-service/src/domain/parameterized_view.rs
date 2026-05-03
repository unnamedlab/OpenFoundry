//! P4 — Parameterized union views.
//!
//! When a dataset is registered as a parameterized union view, its
//! preview is `UNION ALL` over the per-deployment transactions of
//! every output dataset, augmented with a synthetic
//! `_deployment_key` column carrying the run's deployment value.
//!
//! This module is pure-SQL composition: tests can assert the exact
//! query shape without booting Postgres.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct UnionViewSpec {
    pub union_view_dataset_rid: String,
    pub output_dataset_rids: Vec<String>,
    pub deployment_key_param: String,
}

/// Compose the SQL preview query for a parameterized union view.
///
/// Each output dataset contributes a single `SELECT *` over its
/// underlying physical view (named `dataset_view(<dataset_rid>)`),
/// augmented with the literal `_deployment_key` column the build
/// pass writes onto every transaction. `UNION ALL` preserves
/// duplicates across deployments — the doc warns that consumers see
/// the raw rows, not a deduplicated projection.
///
/// `dataset_rid` is interpolated verbatim into the SQL. The function
/// rejects any RID containing a single quote so callers (the preview
/// handler) cannot accidentally compose an injection-prone query
/// from operator-supplied input.
pub fn compose_union_view_sql(spec: &UnionViewSpec) -> Result<String, &'static str> {
    if spec.output_dataset_rids.is_empty() {
        return Err("output_dataset_rids must not be empty");
    }
    for rid in &spec.output_dataset_rids {
        if rid.contains('\'') || rid.contains(';') || rid.contains('"') {
            return Err("output_dataset_rid contains forbidden character");
        }
    }
    let parts: Vec<String> = spec
        .output_dataset_rids
        .iter()
        .map(|rid| {
            format!(
                "SELECT *, deployment_key AS _deployment_key \
                 FROM dataset_transactions \
                 WHERE dataset_rid = '{rid}' \
                   AND deployment_key IS NOT NULL"
            )
        })
        .collect();
    Ok(parts.join(" UNION ALL "))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn spec() -> UnionViewSpec {
        UnionViewSpec {
            union_view_dataset_rid: "ri.foundry.main.dataset.alpha-view".into(),
            output_dataset_rids: vec![
                "ri.foundry.main.dataset.alpha-out".into(),
                "ri.foundry.main.dataset.beta-out".into(),
            ],
            deployment_key_param: "region".into(),
        }
    }

    #[test]
    fn union_view_sql_includes_deployment_key_column() {
        let sql = compose_union_view_sql(&spec()).unwrap();
        assert!(sql.contains("_deployment_key"));
        assert!(sql.contains("UNION ALL"));
        assert!(sql.contains("ri.foundry.main.dataset.alpha-out"));
        assert!(sql.contains("ri.foundry.main.dataset.beta-out"));
    }

    #[test]
    fn union_view_sql_filters_out_non_parameterized_transactions() {
        let sql = compose_union_view_sql(&spec()).unwrap();
        assert!(sql.contains("deployment_key IS NOT NULL"));
    }

    #[test]
    fn union_view_sql_rejects_quote_in_rid() {
        let mut bad = spec();
        bad.output_dataset_rids[0].push('\'');
        let err = compose_union_view_sql(&bad).unwrap_err();
        assert_eq!(err, "output_dataset_rid contains forbidden character");
    }

    #[test]
    fn union_view_sql_rejects_empty_outputs() {
        let mut bad = spec();
        bad.output_dataset_rids.clear();
        assert!(compose_union_view_sql(&bad).is_err());
    }

    #[test]
    fn union_view_sql_emits_one_select_per_output() {
        let sql = compose_union_view_sql(&spec()).unwrap();
        let select_count = sql.matches("SELECT *").count();
        assert_eq!(select_count, 2);
    }
}
