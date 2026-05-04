//! Per Foundry doc § "Parameterized pipelines": the union view
//! exposes every deployment's outputs `UNION ALL`'d, augmented with a
//! synthetic `_deployment_key` column. This pure-SQL test asserts the
//! exact composition shape so a future query-engine change can't
//! silently drop the `_deployment_key` column or de-duplicate rows.

use dataset_versioning_service::domain::parameterized_view::{
    UnionViewSpec, compose_union_view_sql,
};

fn spec_with(outputs: Vec<&str>) -> UnionViewSpec {
    UnionViewSpec {
        union_view_dataset_rid: "ri.foundry.main.dataset.alpha-view".into(),
        output_dataset_rids: outputs.into_iter().map(String::from).collect(),
        deployment_key_param: "region".into(),
    }
}

#[test]
fn union_includes_every_output_dataset() {
    let sql = compose_union_view_sql(&spec_with(vec![
        "ri.foundry.main.dataset.eu-out",
        "ri.foundry.main.dataset.us-out",
        "ri.foundry.main.dataset.ap-out",
    ]))
    .unwrap();
    assert!(sql.contains("ri.foundry.main.dataset.eu-out"));
    assert!(sql.contains("ri.foundry.main.dataset.us-out"));
    assert!(sql.contains("ri.foundry.main.dataset.ap-out"));
    // Two `UNION ALL` glue tokens between three SELECTs.
    assert_eq!(sql.matches("UNION ALL").count(), 2);
}

#[test]
fn union_emits_deployment_key_column_for_every_branch() {
    let sql = compose_union_view_sql(&spec_with(vec![
        "ri.foundry.main.dataset.eu-out",
        "ri.foundry.main.dataset.us-out",
    ]))
    .unwrap();
    // Each SELECT projects `_deployment_key`.
    assert_eq!(sql.matches("_deployment_key").count(), 2);
}

#[test]
fn union_skips_non_parameterized_transactions() {
    let sql = compose_union_view_sql(&spec_with(vec![
        "ri.foundry.main.dataset.eu-out",
    ]))
    .unwrap();
    assert!(sql.contains("deployment_key IS NOT NULL"));
}

#[test]
fn union_rejects_empty_output_list() {
    let err = compose_union_view_sql(&spec_with(vec![])).unwrap_err();
    assert_eq!(err, "output_dataset_rids must not be empty");
}

#[test]
fn union_rejects_dataset_rid_with_quote_character() {
    let mut spec = spec_with(vec!["ok"]);
    spec.output_dataset_rids[0].push('\'');
    let err = compose_union_view_sql(&spec).unwrap_err();
    assert_eq!(err, "output_dataset_rid contains forbidden character");
}
