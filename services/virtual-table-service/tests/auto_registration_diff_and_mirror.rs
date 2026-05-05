//! Pure-function coverage for the P4 auto-registration scanner —
//! diff classification, folder hierarchy mirroring, orphan handling,
//! Databricks tag filtering. Mirrors the test list in the prompt:
//!
//!   * tests/auto_registration_creates_managed_project.rs       — covered in
//!     domain unit tests (project rid is deterministic by source/name).
//!   * tests/auto_registration_mirrors_nested_hierarchy.rs       — here
//!   * tests/auto_registration_orphan_marked_not_deleted.rs      — here
//!   * tests/databricks_tag_filter_excludes_untagged.rs          — here
//!   * tests/auto_registration_run_records_diff_counts.rs        — here
//!
//! DB-coupled tests (project provisioning, `auto_register_runs` row
//! persistence) ship with the testcontainer harness in P4.next.

use virtual_table_service::domain::auto_registration::{
    DiffResult, ExistingTable, FolderMirrorKind, RemoteTable, compute_diff, filter_databricks_tags,
    folder_path,
};
use virtual_table_service::domain::capability_matrix::{SourceProvider, TableType};

fn remote(db: &str, schema: &str, table: &str, sig: &str, tags: &[&str]) -> RemoteTable {
    RemoteTable {
        database: db.into(),
        schema: schema.into(),
        table: table.into(),
        table_type: TableType::Table,
        schema_signature: sig.into(),
        tags: tags.iter().map(|t| t.to_string()).collect(),
    }
}

fn existing(rid: &str, db: &str, schema: &str, table: &str, sig: &str) -> ExistingTable {
    ExistingTable {
        rid: rid.into(),
        database: db.into(),
        schema: schema.into(),
        table: table.into(),
        schema_signature: sig.into(),
    }
}

#[test]
fn nested_hierarchy_mirrors_database_schema_table() {
    let table = remote("warehouse", "public", "orders", "v1", &[]);
    assert_eq!(
        folder_path(FolderMirrorKind::Nested, &table),
        "warehouse/public/orders"
    );
}

#[test]
fn flat_hierarchy_collapses_with_double_underscore() {
    let table = remote("warehouse", "public", "orders", "v1", &[]);
    assert_eq!(
        folder_path(FolderMirrorKind::Flat, &table),
        "warehouse__public__orders"
    );
}

#[test]
fn orphan_is_in_orphaned_bucket_and_not_deleted_by_diff() {
    // Doc § "Auto-registration": orphans must NEVER be auto-deleted.
    // The diff returns them in a separate bucket so the apply path
    // can mark `properties.orphaned = true` instead of issuing a
    // DELETE.
    let remote = vec![remote("c", "s", "kept", "v1", &[])];
    let existing = vec![
        existing("ri.vt.kept", "c", "s", "kept", "v1"),
        existing("ri.vt.gone", "c", "s", "deleted_at_source", "v1"),
    ];
    let DiffResult {
        added,
        updated,
        orphaned,
    } = compute_diff(remote, existing);
    assert!(added.is_empty(), "no new tables");
    assert!(updated.is_empty(), "no schema changes");
    assert_eq!(orphaned.len(), 1);
    assert_eq!(orphaned[0].rid, "ri.vt.gone");
    assert_eq!(orphaned[0].table, "deleted_at_source");
}

#[test]
fn run_diff_counts_match_added_updated_orphaned() {
    let remote = vec![
        remote("c", "s", "new", "v0", &[]),
        remote("c", "s", "kept", "v1", &[]),
        remote("c", "s", "schema_changed", "v2", &[]),
    ];
    let existing = vec![
        existing("ri.vt.kept", "c", "s", "kept", "v1"),
        existing("ri.vt.changed", "c", "s", "schema_changed", "v1"),
        existing("ri.vt.gone", "c", "s", "deleted", "v1"),
    ];
    let diff = compute_diff(remote, existing);
    assert_eq!(diff.added.len(), 1);
    assert_eq!(diff.updated.len(), 1);
    assert_eq!(diff.orphaned.len(), 1);
}

#[test]
fn databricks_tag_filter_excludes_untagged_tables() {
    let tables = vec![
        remote("c", "s", "gold_orders", "v1", &["gold"]),
        remote("c", "s", "raw_events", "v1", &["bronze"]),
        remote("c", "s", "untagged", "v1", &[]),
    ];
    let filtered = filter_databricks_tags(
        SourceProvider::Databricks,
        &["gold".to_string()],
        tables,
    );
    assert_eq!(filtered.len(), 1);
    assert_eq!(filtered[0].table, "gold_orders");
}

#[test]
fn tag_filter_keeps_tables_that_match_any_filter() {
    let tables = vec![
        remote("c", "s", "a", "v1", &["pii"]),
        remote("c", "s", "b", "v1", &["pii", "gold"]),
        remote("c", "s", "c", "v1", &["bronze"]),
    ];
    let filtered = filter_databricks_tags(
        SourceProvider::Databricks,
        &["pii".to_string(), "gold".to_string()],
        tables,
    );
    assert_eq!(filtered.len(), 2);
    assert!(filtered.iter().any(|t| t.table == "a"));
    assert!(filtered.iter().any(|t| t.table == "b"));
}

#[test]
fn tag_filter_is_no_op_for_non_databricks_providers() {
    // The doc only blesses tag filtering for Databricks. Other
    // providers ignore the filter entirely so a misconfigured tenant
    // does not silently strip tables.
    for provider in [
        SourceProvider::BigQuery,
        SourceProvider::Snowflake,
        SourceProvider::AmazonS3,
        SourceProvider::Gcs,
    ] {
        let tables = vec![remote("c", "s", "t", "v1", &[])];
        let result = filter_databricks_tags(provider, &["gold".to_string()], tables.clone());
        assert_eq!(result, tables, "{:?} must not apply the tag filter", provider);
    }
}
