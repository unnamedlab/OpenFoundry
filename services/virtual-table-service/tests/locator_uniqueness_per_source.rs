//! Asserts the locator canonicalisation contract that the unique
//! `(source_rid, locator)` index relies on. Equal locators with
//! different key orderings or whitespace MUST canonicalise to the
//! same JSON Value.

use serde_json::json;
use virtual_table_service::models::virtual_table::Locator;

#[test]
fn tabular_locator_is_canonical_across_key_order_and_whitespace() {
    let a = Locator::Tabular {
        database: "  warehouse ".into(),
        schema: "public".into(),
        table: "orders".into(),
    };
    let b: Locator = serde_json::from_value(json!({
        "kind": "tabular",
        "table": "orders",
        "schema": "public",
        "database": "warehouse",
    }))
    .expect("decode");
    assert_eq!(a.canonicalize(), b.canonicalize());
}

#[test]
fn file_locator_normalizes_format_to_lowercase() {
    let upper = Locator::File {
        bucket: "openfoundry".into(),
        prefix: "year=2026/".into(),
        format: "PARQUET".into(),
    };
    let lower = Locator::File {
        bucket: "openfoundry".into(),
        prefix: "year=2026/".into(),
        format: "parquet".into(),
    };
    assert_eq!(upper.canonicalize(), lower.canonicalize());
}

#[test]
fn iceberg_locator_round_trips() {
    let l = Locator::Iceberg {
        catalog: "polaris".into(),
        namespace: "sales".into(),
        table: "events".into(),
    };
    let canonical = l.canonicalize();
    assert_eq!(canonical["kind"], "iceberg");
    assert_eq!(canonical["catalog"], "polaris");
    assert_eq!(canonical["namespace"], "sales");
    assert_eq!(canonical["table"], "events");
}

#[test]
fn distinct_locators_canonicalize_to_distinct_values() {
    let a = Locator::Tabular {
        database: "wh".into(),
        schema: "s1".into(),
        table: "t".into(),
    };
    let b = Locator::Tabular {
        database: "wh".into(),
        schema: "s2".into(),
        table: "t".into(),
    };
    assert_ne!(a.canonicalize(), b.canonicalize());
}
