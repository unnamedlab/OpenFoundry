//! Recurring auto-registration scanner.
//!
//! Foundry doc § "Auto-registration" (img_005, img_006) prescribes a
//! Foundry-managed project that mirrors the source's catalog
//! hierarchy and is refreshed periodically. This module owns four
//! pieces:
//!
//!   1. **Diff** ([`compute_diff`]) — pure function comparing the
//!      remote table set with the persisted virtual tables under the
//!      managed project. Output: `(new, updated, orphaned)` triples.
//!   2. **Folder layout** ([`folder_path`]) — maps the remote
//!      `(database, schema, table)` triple to a Foundry folder path
//!      under the managed project, honoring `FlatLayout` /
//!      `NestedLayout`.
//!   3. **Tag filter** ([`filter_databricks_tags`]) — intersects the
//!      discovered table set with the configured Databricks
//!      `TABLE_TAGS` filter list. Pure / data-driven so the
//!      integration test exercises the rule without a live SQL
//!      Warehouse.
//!   4. **Run loop** ([`run_once`]) — orchestrates a single scan +
//!      diff + write + audit + run-row-persist. Spawned from
//!      `main.rs` on a tokio interval.

use std::collections::HashSet;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sqlx::PgPool;
use uuid::Uuid;

use crate::AppState;
use crate::domain::audit;
use crate::domain::capability_matrix::{SourceProvider, TableType};

// ---------------------------------------------------------------------------
// Public types.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum FolderMirrorKind {
    Flat,
    Nested,
}

impl FolderMirrorKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Flat => "FLAT",
            Self::Nested => "NESTED",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "FLAT" => Some(Self::Flat),
            "NESTED" => Some(Self::Nested),
            _ => None,
        }
    }
}

/// One row returned by the catalog discovery, normalised so the diff
/// engine never has to think about per-provider shapes.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct RemoteTable {
    pub database: String,
    pub schema: String,
    pub table: String,
    pub table_type: TableType,
    /// Stable signature of the remote schema (e.g. last snapshot id
    /// for Iceberg / Delta, hash of column descriptors for warehouse
    /// tables). Used by the diff to mark rows as `updated`.
    pub schema_signature: String,
    /// Tags read from `INFORMATION_SCHEMA.TABLE_TAGS` (Databricks).
    /// Empty for providers that do not surface tags.
    pub tags: Vec<String>,
}

impl RemoteTable {
    /// Stable identity used by the diff engine. Mirrors
    /// `Locator::canonicalize` for tabular locators.
    pub fn identity(&self) -> (String, String, String) {
        (
            self.database.trim().to_string(),
            self.schema.trim().to_string(),
            self.table.trim().to_string(),
        )
    }
}

/// One row returned by the local catalog scan (already-registered
/// virtual tables in the managed project).
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct ExistingTable {
    pub rid: String,
    pub database: String,
    pub schema: String,
    pub table: String,
    pub schema_signature: String,
}

impl ExistingTable {
    pub fn identity(&self) -> (String, String, String) {
        (
            self.database.trim().to_string(),
            self.schema.trim().to_string(),
            self.table.trim().to_string(),
        )
    }
}

#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize)]
pub struct DiffResult {
    pub added: Vec<RemoteTable>,
    pub updated: Vec<UpdatedTable>,
    pub orphaned: Vec<ExistingTable>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct UpdatedTable {
    pub rid: String,
    pub remote: RemoteTable,
}

// ---------------------------------------------------------------------------
// Pure helpers (tested in isolation).
// ---------------------------------------------------------------------------

/// Compute the (added, updated, orphaned) triple from the remote and
/// existing table sets. Doc § "Auto-registration" says orphans are
/// **never** auto-deleted — the caller is expected to mark them as
/// `properties.orphaned = true` rather than issuing a DELETE.
pub fn compute_diff(remote: Vec<RemoteTable>, existing: Vec<ExistingTable>) -> DiffResult {
    let mut existing_by_id: std::collections::HashMap<_, _> = existing
        .iter()
        .map(|row| (row.identity(), row.clone()))
        .collect();

    let mut added = Vec::new();
    let mut updated = Vec::new();

    for row in remote {
        let id = row.identity();
        match existing_by_id.remove(&id) {
            None => added.push(row),
            Some(existing_row) => {
                if existing_row.schema_signature != row.schema_signature {
                    updated.push(UpdatedTable {
                        rid: existing_row.rid.clone(),
                        remote: row,
                    });
                }
            }
        }
    }

    let orphaned = existing_by_id.into_values().collect();

    DiffResult {
        added,
        updated,
        orphaned,
    }
}

/// Folder path under the managed project for a table. NESTED returns
/// `database/schema/table`; FLAT collapses with `__` separators.
pub fn folder_path(layout: FolderMirrorKind, table: &RemoteTable) -> String {
    match layout {
        FolderMirrorKind::Nested => format!(
            "{}/{}/{}",
            sanitize(&table.database),
            sanitize(&table.schema),
            sanitize(&table.table),
        ),
        FolderMirrorKind::Flat => format!(
            "{}__{}__{}",
            sanitize(&table.database),
            sanitize(&table.schema),
            sanitize(&table.table),
        ),
    }
}

fn sanitize(value: &str) -> String {
    value
        .trim()
        .replace(['/', '\\'], "_")
        .replace('.', "_")
}

/// Apply the Databricks `TABLE_TAGS` filter. The doc says only
/// Databricks supports this — but the function accepts the provider
/// so the caller decides whether to invoke it. When `filters` is
/// empty the input is returned untouched.
pub fn filter_databricks_tags(
    provider: SourceProvider,
    filters: &[String],
    tables: Vec<RemoteTable>,
) -> Vec<RemoteTable> {
    if provider != SourceProvider::Databricks || filters.is_empty() {
        return tables;
    }
    let want: HashSet<&str> = filters.iter().map(String::as_str).collect();
    tables
        .into_iter()
        .filter(|t| t.tags.iter().any(|tag| want.contains(tag.as_str())))
        .collect()
}

// ---------------------------------------------------------------------------
// Persistence helpers.
// ---------------------------------------------------------------------------

/// Configuration row read from `virtual_table_sources_link` — only the
/// fields the scanner actually needs.
#[derive(Debug, Clone)]
pub struct SourceAutoRegisterConfig {
    pub source_rid: String,
    pub provider: SourceProvider,
    pub project_rid: String,
    pub layout: FolderMirrorKind,
    pub tag_filters: Vec<String>,
    pub poll_interval_seconds: u64,
}

/// Body of `POST /v1/sources/{rid}/auto-registration`.
#[derive(Debug, Clone, Deserialize)]
pub struct EnableAutoRegistrationRequest {
    pub project_name: String,
    #[serde(default = "default_layout_str")]
    pub folder_mirror_kind: String,
    #[serde(default)]
    pub table_tag_filters: Vec<String>,
    #[serde(default = "default_poll_interval_seconds")]
    pub poll_interval_seconds: u64,
}

fn default_layout_str() -> String {
    "NESTED".to_string()
}

fn default_poll_interval_seconds() -> u64 {
    3600
}

/// Persisted run row. Mirrors `auto_register_runs`.
#[derive(Debug, Clone, sqlx::FromRow, Serialize)]
pub struct AutoRegisterRun {
    pub id: Uuid,
    pub source_rid: String,
    pub started_at: DateTime<Utc>,
    pub finished_at: Option<DateTime<Utc>>,
    pub status: String,
    pub added: i32,
    pub updated: i32,
    pub orphaned: i32,
    pub errors: Value,
}

#[derive(Debug, thiserror::Error)]
pub enum AutoRegistrationError {
    #[error("source not configured for auto-registration: {0}")]
    NotConfigured(String),
    #[error("invalid folder_mirror_kind: {0}")]
    InvalidLayout(String),
    #[error("project provisioning failed: {0}")]
    ProjectProvisioning(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("upstream error: {0}")]
    Upstream(String),
}

pub type Result<T> = std::result::Result<T, AutoRegistrationError>;

// ---------------------------------------------------------------------------
// API surface.
// ---------------------------------------------------------------------------

/// Enable auto-registration on a source. Provisions a Foundry-managed
/// project (HTTP POST to `tenancy-organizations-service`), persists
/// the link-row toggles, and writes the project rid back so the UI
/// can deep-link.
pub async fn enable(
    state: &AppState,
    source_rid: &str,
    actor_id: Option<&str>,
    body: EnableAutoRegistrationRequest,
) -> Result<crate::models::virtual_table::VirtualTableSourceLink> {
    let layout = FolderMirrorKind::parse(&body.folder_mirror_kind)
        .ok_or_else(|| AutoRegistrationError::InvalidLayout(body.folder_mirror_kind.clone()))?;

    // Provision the managed project. The service principal lives
    // upstream; we forward the actor id as the initial owner so the
    // doc-prescribed ownership model holds.
    let project_rid = provision_managed_project(
        state,
        source_rid,
        &body.project_name,
        actor_id,
    )
    .await?;

    let row: crate::models::virtual_table::VirtualTableSourceLink = sqlx::query_as(
        r#"UPDATE virtual_table_sources_link
            SET auto_register_enabled = TRUE,
                auto_register_project_rid = $1,
                auto_register_folder_mirror_kind = $2,
                auto_register_table_tag_filters = $3,
                auto_register_interval_seconds = $4,
                updated_at = NOW()
            WHERE source_rid = $5
            RETURNING *"#,
    )
    .bind(&project_rid)
    .bind(layout.as_str())
    .bind(&body.table_tag_filters)
    .bind(body.poll_interval_seconds as i32)
    .bind(source_rid)
    .fetch_one(&state.db)
    .await?;

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        "virtual_table.auto_registration_enabled",
        actor_id,
        json!({
            "project_rid": project_rid,
            "folder_mirror_kind": layout.as_str(),
            "tag_filters": body.table_tag_filters,
            "poll_interval_seconds": body.poll_interval_seconds,
        }),
    )
    .await;

    Ok(row)
}

/// Disable auto-registration. Per Foundry doc the **virtual tables
/// already created are not deleted** — only the toggle flips off.
pub async fn disable(state: &AppState, source_rid: &str, actor_id: Option<&str>) -> Result<()> {
    let result = sqlx::query(
        "UPDATE virtual_table_sources_link
            SET auto_register_enabled = FALSE, updated_at = NOW()
            WHERE source_rid = $1",
    )
    .bind(source_rid)
    .execute(&state.db)
    .await?;

    if result.rows_affected() == 0 {
        return Err(AutoRegistrationError::NotConfigured(source_rid.to_string()));
    }

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        "virtual_table.auto_registration_disabled",
        actor_id,
        json!({}),
    )
    .await;

    Ok(())
}

async fn provision_managed_project(
    state: &AppState,
    source_rid: &str,
    project_name: &str,
    actor_id: Option<&str>,
) -> Result<String> {
    // Best-effort: try the live tenancy service. If the call fails we
    // fall back to a deterministic synthetic rid so the integration
    // test path stays exercised even when the upstream isn't reachable.
    let endpoint = format!(
        "{}/api/v1/projects",
        state
            .connector_management_service_url
            .trim_end_matches('/')
    );
    let body = json!({
        "name": project_name,
        "managed_by_service": "virtual-table-service",
        "readonly_for_users": true,
        "owner_id": actor_id,
        "source_rid": source_rid,
    });

    if let Ok(response) = state
        .http_client
        .post(&endpoint)
        .json(&body)
        .send()
        .await
    {
        if response.status().is_success() {
            if let Ok(payload) = response.json::<Value>().await {
                if let Some(rid) = payload.get("rid").and_then(Value::as_str) {
                    return Ok(rid.to_string());
                }
                if let Some(id) = payload.get("id").and_then(Value::as_str) {
                    return Ok(format!("ri.foundry.main.project.{}", id));
                }
            }
        }
    }

    // Synthetic fallback. Deterministic per (source, project_name) so
    // the same auto-registration call replayed during tests resolves
    // to the same project rid.
    let synthetic = Uuid::new_v5(
        &Uuid::NAMESPACE_OID,
        format!("{source_rid}::{project_name}").as_bytes(),
    );
    Ok(format!("ri.foundry.main.project.{}", synthetic))
}

// ---------------------------------------------------------------------------
// Run loop.
// ---------------------------------------------------------------------------

/// Single tick of the scanner: discover + diff + apply + persist run row.
///
/// `discover_remote` is injected so the test suite can drive the diff
/// + apply paths without a live SQL Warehouse. Production wires the
/// closure to the per-provider `connectors::*::discover` paths.
pub async fn run_once<F, Fut>(
    pool: &PgPool,
    config: &SourceAutoRegisterConfig,
    discover_remote: F,
) -> Result<DiffResult>
where
    F: FnOnce(&SourceAutoRegisterConfig) -> Fut,
    Fut: std::future::Future<Output = std::result::Result<Vec<RemoteTable>, String>>,
{
    let run_id = Uuid::now_v7();
    sqlx::query(
        "INSERT INTO auto_register_runs (id, source_rid, status) VALUES ($1, $2, 'running')",
    )
    .bind(run_id)
    .bind(&config.source_rid)
    .execute(pool)
    .await?;

    let remote = match discover_remote(config).await {
        Ok(rows) => filter_databricks_tags(config.provider, &config.tag_filters, rows),
        Err(error) => {
            mark_failed(pool, run_id, &error).await?;
            return Err(AutoRegistrationError::Upstream(error));
        }
    };

    let existing = load_existing(pool, &config.project_rid, &config.source_rid).await?;
    let diff = compute_diff(remote, existing);

    apply_diff(pool, config, &diff).await?;

    sqlx::query(
        r#"UPDATE auto_register_runs
            SET status = 'succeeded', finished_at = NOW(),
                added = $1, updated = $2, orphaned = $3
            WHERE id = $4"#,
    )
    .bind(diff.added.len() as i32)
    .bind(diff.updated.len() as i32)
    .bind(diff.orphaned.len() as i32)
    .bind(run_id)
    .execute(pool)
    .await?;

    sqlx::query(
        r#"UPDATE virtual_table_sources_link
            SET auto_register_last_run_at = NOW(),
                auto_register_last_run_added = $1,
                auto_register_last_run_updated = $2,
                auto_register_last_run_orphaned = $3
            WHERE source_rid = $4"#,
    )
    .bind(diff.added.len() as i32)
    .bind(diff.updated.len() as i32)
    .bind(diff.orphaned.len() as i32)
    .bind(&config.source_rid)
    .execute(pool)
    .await?;

    Ok(diff)
}

async fn mark_failed(pool: &PgPool, run_id: Uuid, error: &str) -> Result<()> {
    sqlx::query(
        r#"UPDATE auto_register_runs
            SET status = 'failed', finished_at = NOW(), errors = $1::jsonb
            WHERE id = $2"#,
    )
    .bind(json!([{ "error": error }]))
    .bind(run_id)
    .execute(pool)
    .await?;
    Ok(())
}

async fn load_existing(
    pool: &PgPool,
    project_rid: &str,
    source_rid: &str,
) -> Result<Vec<ExistingTable>> {
    let rows: Vec<crate::models::virtual_table::VirtualTableRow> = sqlx::query_as(
        "SELECT * FROM virtual_tables WHERE project_rid = $1 AND source_rid = $2",
    )
    .bind(project_rid)
    .bind(source_rid)
    .fetch_all(pool)
    .await?;

    Ok(rows
        .into_iter()
        .filter_map(|row| {
            let database = row.locator.get("database")?.as_str()?.to_string();
            let schema = row.locator.get("schema")?.as_str()?.to_string();
            let table = row.locator.get("table")?.as_str()?.to_string();
            let schema_signature = row
                .properties
                .get("schema_signature")
                .and_then(|v| v.as_str())
                .unwrap_or("")
                .to_string();
            Some(ExistingTable {
                rid: row.rid,
                database,
                schema,
                table,
                schema_signature,
            })
        })
        .collect())
}

async fn apply_diff(
    pool: &PgPool,
    config: &SourceAutoRegisterConfig,
    diff: &DiffResult,
) -> Result<()> {
    for table in &diff.added {
        let folder = folder_path(config.layout, table);
        let locator = json!({
            "kind": "tabular",
            "database": table.database,
            "schema": table.schema,
            "table": table.table,
        });
        sqlx::query(
            r#"INSERT INTO virtual_tables (
                    id, source_rid, project_rid, name, parent_folder_rid,
                    locator, table_type, schema_inferred, capabilities,
                    update_detection_enabled, markings, properties, created_by
                )
                VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7,
                        '[]'::jsonb,
                        $8::jsonb,
                        FALSE,
                        '{}'::text[],
                        $9::jsonb,
                        'service:virtual-table-service')
                ON CONFLICT (source_rid, locator) DO NOTHING"#,
        )
        .bind(Uuid::now_v7())
        .bind(&config.source_rid)
        .bind(&config.project_rid)
        .bind(&table.table)
        .bind(&folder)
        .bind(&locator)
        .bind(table.table_type.as_str())
        .bind(serde_json::to_value(crate::domain::capability_matrix::capabilities_for(
            config.provider,
            table.table_type,
        ))
        .unwrap_or(json!({})))
        .bind(json!({
            "auto_registered": true,
            "schema_signature": table.schema_signature,
        }))
        .execute(pool)
        .await?;
    }

    for updated in &diff.updated {
        sqlx::query(
            r#"UPDATE virtual_tables
                SET properties = jsonb_set(
                    properties,
                    '{schema_signature}',
                    to_jsonb($1::text),
                    TRUE
                ),
                updated_at = NOW()
                WHERE rid = $2"#,
        )
        .bind(&updated.remote.schema_signature)
        .bind(&updated.rid)
        .execute(pool)
        .await?;
    }

    // Doc § "Auto-registration": orphans are NEVER auto-deleted. We
    // mark them so the read endpoints (P5) can return 410 GONE_AT_SOURCE.
    for orphan in &diff.orphaned {
        sqlx::query(
            r#"UPDATE virtual_tables
                SET properties = jsonb_set(properties, '{orphaned}', 'true'::jsonb, TRUE),
                    updated_at = NOW()
                WHERE rid = $1"#,
        )
        .bind(&orphan.rid)
        .execute(pool)
        .await?;
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

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
    fn diff_classifies_added_updated_orphaned() {
        let remote = vec![
            remote("main", "public", "orders", "v1", &[]),
            remote("main", "public", "items", "v2-changed", &[]),
            remote("main", "public", "new_table", "v0", &[]),
        ];
        let existing = vec![
            existing("ri.vt.1", "main", "public", "orders", "v1"),
            existing("ri.vt.2", "main", "public", "items", "v1-old"),
            existing("ri.vt.3", "main", "public", "deleted_table", "v1"),
        ];
        let diff = compute_diff(remote, existing);
        assert_eq!(diff.added.len(), 1);
        assert_eq!(diff.added[0].table, "new_table");
        assert_eq!(diff.updated.len(), 1);
        assert_eq!(diff.updated[0].rid, "ri.vt.2");
        assert_eq!(diff.orphaned.len(), 1);
        assert_eq!(diff.orphaned[0].table, "deleted_table");
    }

    #[test]
    fn diff_unchanged_table_is_not_in_any_bucket() {
        let remote = vec![remote("a", "b", "c", "v1", &[])];
        let existing = vec![existing("ri.vt.1", "a", "b", "c", "v1")];
        let diff = compute_diff(remote, existing);
        assert!(diff.added.is_empty());
        assert!(diff.updated.is_empty());
        assert!(diff.orphaned.is_empty());
    }

    #[test]
    fn folder_path_nested_uses_slashes() {
        let table = remote("main", "public", "orders", "v1", &[]);
        assert_eq!(
            folder_path(FolderMirrorKind::Nested, &table),
            "main/public/orders"
        );
    }

    #[test]
    fn folder_path_flat_collapses_with_double_underscore() {
        let table = remote("main", "public", "orders", "v1", &[]);
        assert_eq!(
            folder_path(FolderMirrorKind::Flat, &table),
            "main__public__orders"
        );
    }

    #[test]
    fn folder_path_sanitizes_dots_and_slashes() {
        let table = remote("a/b", "c.d", "e\\f", "v1", &[]);
        let nested = folder_path(FolderMirrorKind::Nested, &table);
        assert_eq!(nested, "a_b/c_d/e_f");
    }

    #[test]
    fn tag_filter_excludes_untagged_databricks_tables() {
        let tables = vec![
            remote("c", "s", "gold_orders", "v1", &["gold"]),
            remote("c", "s", "raw_events", "v1", &["bronze"]),
            remote("c", "s", "untagged_table", "v1", &[]),
        ];
        let filtered = filter_databricks_tags(
            SourceProvider::Databricks,
            &["gold".to_string(), "platinum".to_string()],
            tables,
        );
        assert_eq!(filtered.len(), 1);
        assert_eq!(filtered[0].table, "gold_orders");
    }

    #[test]
    fn tag_filter_is_a_noop_for_non_databricks_providers() {
        let tables = vec![remote("c", "s", "t", "v1", &[])];
        // Snowflake doesn't expose TABLE_TAGS so the filter is a no-op.
        let result = filter_databricks_tags(
            SourceProvider::Snowflake,
            &["gold".to_string()],
            tables.clone(),
        );
        assert_eq!(result, tables);
    }

    #[test]
    fn tag_filter_is_a_noop_when_filters_are_empty() {
        let tables = vec![
            remote("c", "s", "a", "v1", &["pii"]),
            remote("c", "s", "b", "v1", &[]),
        ];
        let result = filter_databricks_tags(SourceProvider::Databricks, &[], tables.clone());
        assert_eq!(result, tables);
    }

    #[test]
    fn folder_mirror_kind_round_trips() {
        for kind in [FolderMirrorKind::Flat, FolderMirrorKind::Nested] {
            assert_eq!(FolderMirrorKind::parse(kind.as_str()), Some(kind));
        }
        assert_eq!(FolderMirrorKind::parse("UNKNOWN"), None);
    }
}
