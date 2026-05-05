//! Verifies the Databricks connector's table_type detection matches
//! the Foundry compatibility matrix. Specifically:
//!   * External Delta and Managed Iceberg are read+write.
//!   * Managed Delta is read-only.
//!   * Views and Materialized Views are read-only.

use virtual_table_service::connectors::databricks::{
    ThreePartName, capabilities_for_table, detect_table_type, validate_config,
};
use virtual_table_service::domain::capability_matrix::TableType;

#[test]
fn external_delta_is_read_write() {
    let tt = detect_table_type("Provider: DELTA\nLocation: external\n");
    assert_eq!(tt, TableType::ExternalDelta);
    let caps = capabilities_for_table(tt);
    assert!(caps.read);
    assert!(caps.write, "External Delta must be writable per Foundry doc");
}

#[test]
fn managed_delta_is_read_only() {
    let tt = detect_table_type("Provider: delta\nLocation: dbfs:/tables/orders\n");
    assert_eq!(tt, TableType::ManagedDelta);
    let caps = capabilities_for_table(tt);
    assert!(caps.read);
    assert!(!caps.write, "Managed Delta must be read-only per Foundry doc");
}

#[test]
fn managed_iceberg_is_read_write() {
    let tt = detect_table_type("Provider: iceberg\n");
    assert_eq!(tt, TableType::ManagedIceberg);
    let caps = capabilities_for_table(tt);
    assert!(caps.read);
    assert!(caps.write);
}

#[test]
fn view_is_read_only_with_pyspark_pushdown() {
    let tt = detect_table_type("Type: VIEW\nView Text: SELECT 1\n");
    assert_eq!(tt, TableType::View);
    let caps = capabilities_for_table(tt);
    assert!(caps.read);
    assert!(!caps.write);
    assert!(caps.compute_pushdown.is_some());
}

#[test]
fn config_validation_rejects_http_workspace() {
    let err = validate_config(&serde_json::json!({
        "workspace_url": "http://insecure",
        "warehouse_http_path": "/sql/1.0/warehouses/abc",
        "auth_mode": "pat"
    }))
    .expect_err("must reject http://");
    assert!(err.contains("https"));
}

#[test]
fn config_validation_accepts_oauth_m2m() {
    validate_config(&serde_json::json!({
        "workspace_url": "https://acme.cloud.databricks.com",
        "warehouse_http_path": "/sql/1.0/warehouses/abc",
        "auth_mode": "oauth_m2m"
    }))
    .expect("oauth_m2m should be accepted");
}

#[test]
fn three_part_name_round_trips_to_locator() {
    let parsed = ThreePartName::parse("main.public.events").expect("parse");
    let locator = parsed.to_locator_value();
    assert_eq!(locator["kind"], "tabular");
    assert_eq!(locator["database"], "main");
    assert_eq!(locator["schema"], "public");
    assert_eq!(locator["table"], "events");
}
