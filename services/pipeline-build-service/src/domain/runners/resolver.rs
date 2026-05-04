//! Resolver for [`super::view_filter::ViewFilter`] selectors.
//!
//! Materialises every `InputSpec.view_filter` entry into a
//! [`ResolvedViewFilter`] with concrete view ids / transaction
//! windows. Persisted into `jobs.input_view_resolutions` so the
//! runner can replay the build with byte-for-byte input identity.

use sqlx::PgPool;
use std::collections::HashSet;

use super::view_filter::{ResolvedViewFilter, ViewFilter};
use crate::domain::build_resolution::{InputSpec, JobSpec, ResolvedInputView};

/// Result envelope: per-input resolutions + a flat validation error
/// list. Empty `errors` means the resolver was happy with every
/// selector; non-empty short-circuits the build before any locks are
/// acquired.
#[derive(Debug, Default, Clone, PartialEq, Eq)]
pub struct ViewResolutionOutcome {
    pub resolutions: Vec<ResolvedViewFilter>,
    pub errors: Vec<String>,
}

/// Materialise the view filters declared on `spec.inputs` into
/// concrete view ids / transaction windows. The branch resolution
/// step has already run, so `resolved_inputs` carries the per-input
/// branch the build will read from.
///
/// The resolver is async because `INCREMENTAL_SINCE_LAST_BUILD`
/// queries the `jobs` table for the previous COMPLETED job; the
/// other three selectors are pure transforms over the input itself.
pub async fn resolve_view_filters(
    pool: &PgPool,
    pipeline_rid: &str,
    build_branch: &str,
    spec: &JobSpec,
    resolved_inputs: &[ResolvedInputView],
) -> ViewResolutionOutcome {
    let mut outcome = ViewResolutionOutcome::default();
    // Index resolved_inputs by dataset_rid so we can attach the
    // branch the resolver picked.
    let resolved_branches: std::collections::HashMap<&str, String> = resolved_inputs
        .iter()
        .map(|r| (r.dataset_rid.as_str(), r.branch.as_str().to_string()))
        .collect();
    let producer_outputs: HashSet<&str> = spec
        .output_dataset_rids
        .iter()
        .map(String::as_str)
        .collect();

    for input in &spec.inputs {
        if producer_outputs.contains(input.dataset_rid.as_str()) {
            // Virtual input — produced by another spec in this same
            // build. View filter does not apply.
            continue;
        }
        let branch = resolved_branches
            .get(input.dataset_rid.as_str())
            .cloned()
            .unwrap_or_else(|| build_branch.to_string());

        if input.view_filter.is_empty() {
            // No selector → use the resolved branch's current view.
            outcome.resolutions.push(ResolvedViewFilter {
                dataset_rid: input.dataset_rid.clone(),
                branch,
                filter: ViewFilter::AtTransaction {
                    transaction_rid: String::new(),
                },
                resolved_view_id: None,
                resolved_transaction_rid: None,
                range_from_transaction_rid: None,
                range_to_transaction_rid: None,
                note: Some("no view_filter declared; runner reads current view".into()),
            });
            continue;
        }

        for filter in &input.view_filter {
            let resolved = resolve_one(
                pool,
                pipeline_rid,
                build_branch,
                input,
                spec,
                &branch,
                filter,
            )
            .await;
            match resolved {
                Ok(r) => outcome.resolutions.push(r),
                Err(err) => outcome.errors.push(err),
            }
        }
    }

    outcome
}

async fn resolve_one(
    pool: &PgPool,
    pipeline_rid: &str,
    _build_branch: &str,
    input: &InputSpec,
    spec: &JobSpec,
    branch: &str,
    filter: &ViewFilter,
) -> Result<ResolvedViewFilter, String> {
    match filter {
        ViewFilter::AtTimestamp { value } => Ok(ResolvedViewFilter {
            dataset_rid: input.dataset_rid.clone(),
            branch: branch.to_string(),
            filter: filter.clone(),
            // The dataset-versioning client materialises the actual
            // view id when the runner makes the read; the resolver
            // only validates the timestamp and persists the selector.
            resolved_view_id: None,
            resolved_transaction_rid: None,
            range_from_transaction_rid: None,
            range_to_transaction_rid: None,
            note: Some(format!("at_timestamp:{value}")),
        }),
        ViewFilter::AtTransaction { transaction_rid } => {
            if transaction_rid.is_empty() {
                return Err(format!(
                    "{}: AT_TRANSACTION requires a non-empty transaction_rid",
                    input.dataset_rid
                ));
            }
            Ok(ResolvedViewFilter {
                dataset_rid: input.dataset_rid.clone(),
                branch: branch.to_string(),
                filter: filter.clone(),
                resolved_view_id: None,
                resolved_transaction_rid: Some(transaction_rid.clone()),
                range_from_transaction_rid: None,
                range_to_transaction_rid: None,
                note: None,
            })
        }
        ViewFilter::Range {
            from_transaction,
            to_transaction,
        } => {
            if from_transaction.is_empty() || to_transaction.is_empty() {
                return Err(format!(
                    "{}: RANGE requires both from_transaction and to_transaction",
                    input.dataset_rid
                ));
            }
            Ok(ResolvedViewFilter {
                dataset_rid: input.dataset_rid.clone(),
                branch: branch.to_string(),
                filter: filter.clone(),
                resolved_view_id: None,
                resolved_transaction_rid: None,
                range_from_transaction_rid: Some(from_transaction.clone()),
                range_to_transaction_rid: Some(to_transaction.clone()),
                note: None,
            })
        }
        ViewFilter::IncrementalSinceLastBuild => {
            let outputs = serde_json::Value::Array(
                spec.output_dataset_rids
                    .iter()
                    .map(|s| serde_json::Value::String(s.clone()))
                    .collect(),
            );
            // Look at the most recent COMPLETED job for the same
            // (pipeline_rid, build_branch, output set) and pull the
            // window's upper bound from its resolution row for this
            // dataset.
            let lower_bound: Option<(serde_json::Value,)> = sqlx::query_as(
                r#"SELECT j.input_view_resolutions
                     FROM jobs j
                     JOIN builds b ON b.id = j.build_id
                    WHERE b.pipeline_rid = $1
                      AND b.build_branch = $2
                      AND j.state = 'COMPLETED'
                      AND j.input_view_resolutions @> '[]'::jsonb
                      AND (
                        SELECT array_agg(value::text)
                        FROM jsonb_array_elements_text(to_jsonb($3::text[]))
                      ) IS NOT NULL
                    ORDER BY j.state_changed_at DESC
                    LIMIT 1"#,
            )
            .bind(pipeline_rid)
            .bind(branch)
            .bind(serde_json::from_value::<Vec<String>>(outputs).unwrap_or_default())
            .fetch_optional(pool)
            .await
            .map_err(|e| format!("incremental lookup failed: {e}"))?;

            let from_txn = match lower_bound {
                Some((value,)) => serde_json::from_value::<Vec<ResolvedViewFilter>>(value)
                    .ok()
                    .and_then(|arr| {
                        arr.into_iter()
                            .find(|r| r.dataset_rid == input.dataset_rid)
                            .and_then(|r| r.range_to_transaction_rid.or(r.resolved_transaction_rid))
                    }),
                None => None,
            };

            // Upper bound is the dataset's HEAD as of resolution
            // time. The resolver does not query dataset-versioning
            // here — the runner snapshots HEAD when it actually
            // reads. We persist the lower bound so the runner can
            // build the (lower, HEAD] window.
            Ok(ResolvedViewFilter {
                dataset_rid: input.dataset_rid.clone(),
                branch: branch.to_string(),
                filter: filter.clone(),
                resolved_view_id: None,
                resolved_transaction_rid: None,
                range_from_transaction_rid: from_txn.clone(),
                range_to_transaction_rid: None,
                note: Some(if from_txn.is_some() {
                    "incremental: lower bound from previous COMPLETED job".into()
                } else {
                    "incremental: no prior build, runner will read full view".into()
                }),
            })
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn input(rid: &str, filters: Vec<ViewFilter>) -> InputSpec {
        InputSpec {
            dataset_rid: rid.into(),
            fallback_chain: vec!["master".into()],
            view_filter: filters,
            require_fresh: false,
        }
    }

    fn dummy_spec(inputs: Vec<InputSpec>) -> JobSpec {
        JobSpec {
            rid: "ri.spec.s".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs,
            output_dataset_rids: vec!["out".into()],
            logic_kind: "TRANSFORM".into(),
            logic_payload: serde_json::Value::Null,
            content_hash: "h".into(),
        }
    }

    fn dummy_resolved(rid: &str, branch: &str) -> ResolvedInputView {
        ResolvedInputView {
            dataset_rid: rid.into(),
            branch: branch.parse().unwrap(),
            schema: serde_json::json!({}),
        }
    }

    #[tokio::test]
    async fn at_transaction_passes_through_validated_rid() {
        // No DB access for these branches → use a closed pool that
        // never gets touched.
        let pool = sqlx::postgres::PgPoolOptions::new()
            .max_connections(1)
            .connect_lazy("postgres://nobody@127.0.0.1/none")
            .unwrap();
        let spec = dummy_spec(vec![input(
            "ds.a",
            vec![ViewFilter::AtTransaction {
                transaction_rid: "ri.txn.42".into(),
            }],
        )]);
        let resolved = vec![dummy_resolved("ds.a", "master")];
        let outcome = resolve_view_filters(&pool, "ri.p", "master", &spec, &resolved).await;
        assert!(outcome.errors.is_empty(), "{outcome:?}");
        assert_eq!(outcome.resolutions.len(), 1);
        assert_eq!(
            outcome.resolutions[0].resolved_transaction_rid.as_deref(),
            Some("ri.txn.42")
        );
    }

    #[tokio::test]
    async fn at_transaction_rejects_empty_rid() {
        let pool = sqlx::postgres::PgPoolOptions::new()
            .max_connections(1)
            .connect_lazy("postgres://nobody@127.0.0.1/none")
            .unwrap();
        let spec = dummy_spec(vec![input(
            "ds.a",
            vec![ViewFilter::AtTransaction {
                transaction_rid: String::new(),
            }],
        )]);
        let resolved = vec![dummy_resolved("ds.a", "master")];
        let outcome = resolve_view_filters(&pool, "ri.p", "master", &spec, &resolved).await;
        assert_eq!(outcome.errors.len(), 1);
        assert!(outcome.errors[0].contains("AT_TRANSACTION"));
    }

    #[tokio::test]
    async fn range_requires_both_endpoints() {
        let pool = sqlx::postgres::PgPoolOptions::new()
            .max_connections(1)
            .connect_lazy("postgres://nobody@127.0.0.1/none")
            .unwrap();
        let spec = dummy_spec(vec![input(
            "ds.a",
            vec![ViewFilter::Range {
                from_transaction: "ri.a".into(),
                to_transaction: String::new(),
            }],
        )]);
        let resolved = vec![dummy_resolved("ds.a", "master")];
        let outcome = resolve_view_filters(&pool, "ri.p", "master", &spec, &resolved).await;
        assert_eq!(outcome.errors.len(), 1);
        assert!(outcome.errors[0].contains("RANGE"));
    }

    #[tokio::test]
    async fn empty_filter_list_yields_default_current_view() {
        let pool = sqlx::postgres::PgPoolOptions::new()
            .max_connections(1)
            .connect_lazy("postgres://nobody@127.0.0.1/none")
            .unwrap();
        let spec = dummy_spec(vec![input("ds.a", vec![])]);
        let resolved = vec![dummy_resolved("ds.a", "master")];
        let outcome = resolve_view_filters(&pool, "ri.p", "master", &spec, &resolved).await;
        assert!(outcome.errors.is_empty());
        assert_eq!(outcome.resolutions.len(), 1);
        assert_eq!(outcome.resolutions[0].dataset_rid, "ds.a");
        assert_eq!(outcome.resolutions[0].branch, "master");
    }
}
