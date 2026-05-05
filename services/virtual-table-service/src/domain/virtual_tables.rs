//! First-class virtual table operations: enable on a source, register
//! manually or in bulk, list / get / delete, refresh schema, update
//! markings.
//!
//! Doc anchors:
//!   * "Set up a connection for a virtual table" — enable on the source.
//!   * "Create virtual tables" / "Bulk registration" — register paths.
//!   * "Viewing virtual table details" — schema + capabilities surface.
//!
//! All write paths emit a `virtual_table.*` audit event via
//! [`crate::domain::audit`] and bump a Prometheus counter
//! (`virtual_tables_registered_total{provider, kind=manual|bulk|auto}`,
//! see `crate::metrics`).

use chrono::Utc;
use serde_json::{Value, json};
use sqlx::PgPool;
use uuid::Uuid;

use crate::AppState;
use crate::domain::audit;
use crate::domain::capability_matrix::{Capabilities, SourceProvider, TableType, capabilities_for};
use crate::domain::iceberg_catalogs::{self, CatalogKind};
use crate::domain::schema_inference;
use crate::domain::source_validation::{self, RejectionReason};
use crate::models::virtual_table::{
    BulkRegisterError, BulkRegisterRequest, BulkRegisterResponse, DiscoveredEntry, EnableSourceRequest,
    ListVirtualTablesQuery, ListVirtualTablesResponse, Locator, RegisterVirtualTableRequest,
    UpdateMarkingsRequest, VirtualTableRow, VirtualTableSourceLink,
};

/// Registration kind label, fed into Prometheus
/// `virtual_tables_registered_total{kind}`.
#[derive(Debug, Clone, Copy)]
pub enum RegistrationKind {
    Manual,
    Bulk,
    Auto,
}

impl RegistrationKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Manual => "manual",
            Self::Bulk => "bulk",
            Self::Auto => "auto",
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum VirtualTableError {
    #[error("source not registered for virtual tables: {0}")]
    SourceNotEnabled(String),
    #[error("invalid table_type: {0}")]
    InvalidTableType(String),
    #[error("invalid provider: {0}")]
    InvalidProvider(String),
    #[error("virtual table not found: {0}")]
    NotFound(String),
    #[error("unique violation — locator already registered for this source")]
    LocatorAlreadyRegistered,
    #[error("unique violation — name already taken in target folder")]
    NameAlreadyTaken,
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
    #[error("schema inference error: {0}")]
    SchemaInference(String),
    /// Doc § "Limitations of using virtual tables" rule violated. The
    /// payload carries the structured rejection reason so the HTTP
    /// handler can surface a 412 with a stable error code and the
    /// remediation hint.
    #[error("source not compatible with virtual tables: {0:?}")]
    SourceIncompatible(RejectionReason),
    #[error("iceberg catalog error: {0}")]
    IcebergCatalog(String),
}

pub type Result<T> = std::result::Result<T, VirtualTableError>;

// ---------------------------------------------------------------------------
// Source-level toggles.
// ---------------------------------------------------------------------------

/// Idempotent: enable the virtual-tables surface for a source. If the
/// link row already exists, the toggle is set to `true` and the
/// metadata fields (provider, iceberg catalog) are refreshed.
pub async fn enable_source(
    state: &AppState,
    source_rid: &str,
    body: EnableSourceRequest,
) -> Result<VirtualTableSourceLink> {
    let provider = SourceProvider::parse(&body.provider)
        .ok_or_else(|| VirtualTableError::InvalidProvider(body.provider.clone()))?;

    let row: VirtualTableSourceLink = sqlx::query_as(
        r#"INSERT INTO virtual_table_sources_link (
                source_rid, provider, virtual_tables_enabled,
                iceberg_catalog_kind, iceberg_catalog_config
            )
            VALUES ($1, $2, TRUE, $3, $4)
            ON CONFLICT (source_rid) DO UPDATE SET
                virtual_tables_enabled = TRUE,
                provider = EXCLUDED.provider,
                iceberg_catalog_kind = EXCLUDED.iceberg_catalog_kind,
                iceberg_catalog_config = EXCLUDED.iceberg_catalog_config,
                updated_at = NOW()
            RETURNING *"#,
    )
    .bind(source_rid)
    .bind(provider.as_str())
    .bind(body.iceberg_catalog_kind)
    .bind(body.iceberg_catalog_config)
    .fetch_one(&state.db)
    .await?;

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        "virtual_table.source_enabled",
        None,
        json!({ "provider": provider.as_str() }),
    )
    .await;

    Ok(row)
}

pub async fn disable_source(state: &AppState, source_rid: &str) -> Result<()> {
    let result = sqlx::query(
        "UPDATE virtual_table_sources_link
            SET virtual_tables_enabled = FALSE, updated_at = NOW()
            WHERE source_rid = $1",
    )
    .bind(source_rid)
    .execute(&state.db)
    .await?;

    if result.rows_affected() == 0 {
        return Err(VirtualTableError::SourceNotEnabled(source_rid.to_string()));
    }

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        "virtual_table.source_disabled",
        None,
        json!({}),
    )
    .await;

    Ok(())
}

/// Configure the Iceberg catalog kind + config blob for a source
/// (Foundry doc § "Iceberg catalogs"). Validates the (provider,
/// catalog_kind) combination against the published compatibility
/// matrix before persisting.
pub async fn set_iceberg_catalog(
    state: &AppState,
    source_rid: &str,
    actor_id: Option<&str>,
    catalog_kind: CatalogKind,
    catalog_config: Value,
) -> Result<VirtualTableSourceLink> {
    let link = get_source_link(&state.db, source_rid).await?;
    let provider = link
        .provider_enum()
        .ok_or_else(|| VirtualTableError::InvalidProvider(link.provider.clone()))?;
    if !iceberg_catalogs::compatibility(provider, catalog_kind).is_supported() {
        return Err(VirtualTableError::IcebergCatalog(format!(
            "{:?} × {:?} is not supported per Foundry doc",
            provider, catalog_kind
        )));
    }
    // Probe-build the catalog so a malformed config blob fails fast.
    iceberg_catalogs::build_catalog(provider, catalog_kind, &catalog_config)
        .map_err(|err| VirtualTableError::IcebergCatalog(err.to_string()))?;

    let row: VirtualTableSourceLink = sqlx::query_as(
        r#"UPDATE virtual_table_sources_link
            SET iceberg_catalog_kind = $1,
                iceberg_catalog_config = $2,
                updated_at = NOW()
            WHERE source_rid = $3
            RETURNING *"#,
    )
    .bind(catalog_kind.as_str())
    .bind(&catalog_config)
    .bind(source_rid)
    .fetch_one(&state.db)
    .await?;

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        "virtual_table.iceberg_catalog_configured",
        actor_id,
        json!({
            "catalog_kind": catalog_kind.as_str(),
            "provider": provider.as_str(),
        }),
    )
    .await;

    Ok(row)
}

pub async fn get_source_link(
    pool: &PgPool,
    source_rid: &str,
) -> Result<VirtualTableSourceLink> {
    let row: Option<VirtualTableSourceLink> =
        sqlx::query_as("SELECT * FROM virtual_table_sources_link WHERE source_rid = $1")
            .bind(source_rid)
            .fetch_optional(pool)
            .await?;
    row.ok_or_else(|| VirtualTableError::SourceNotEnabled(source_rid.to_string()))
}

// ---------------------------------------------------------------------------
// Discovery (P1 stub — connectors return real data in P2).
// ---------------------------------------------------------------------------

/// Best-effort remote-catalog browse. P1 returns a deterministic stub
/// derived from the source link so the UI flow can be exercised
/// end-to-end; P2 swaps the body for the per-connector live calls
/// inside `crate::connectors`.
pub async fn discover_remote_catalog(
    state: &AppState,
    source_rid: &str,
    path: Option<&str>,
) -> Result<Vec<DiscoveredEntry>> {
    let link = get_source_link(&state.db, source_rid).await?;
    let provider = link
        .provider_enum()
        .ok_or_else(|| VirtualTableError::InvalidProvider(link.provider.clone()))?;
    let path = path.unwrap_or("").trim();

    let entries = match provider {
        SourceProvider::BigQuery | SourceProvider::Databricks | SourceProvider::Snowflake => {
            warehouse_catalog_stub(path)
        }
        SourceProvider::AmazonS3 | SourceProvider::AzureAbfs | SourceProvider::Gcs => {
            object_store_catalog_stub(path)
        }
        SourceProvider::FoundryIceberg => iceberg_catalog_stub(path),
    };

    Ok(entries)
}

fn warehouse_catalog_stub(path: &str) -> Vec<DiscoveredEntry> {
    let parts: Vec<&str> = path.split('/').filter(|p| !p.is_empty()).collect();
    match parts.len() {
        0 => vec![DiscoveredEntry {
            display_name: "main".into(),
            path: "main".into(),
            kind: "database".into(),
            registrable: false,
            inferred_table_type: None,
        }],
        1 => vec![DiscoveredEntry {
            display_name: "public".into(),
            path: format!("{}/public", parts[0]),
            kind: "schema".into(),
            registrable: false,
            inferred_table_type: None,
        }],
        2 => vec![DiscoveredEntry {
            display_name: "stub_table".into(),
            path: format!("{}/{}/stub_table", parts[0], parts[1]),
            kind: "table".into(),
            registrable: true,
            inferred_table_type: Some(TableType::Table.as_str().to_string()),
        }],
        _ => vec![],
    }
}

fn object_store_catalog_stub(path: &str) -> Vec<DiscoveredEntry> {
    let parts: Vec<&str> = path.split('/').filter(|p| !p.is_empty()).collect();
    if parts.is_empty() {
        return vec![DiscoveredEntry {
            display_name: "openfoundry-default".into(),
            path: "openfoundry-default".into(),
            kind: "file_prefix".into(),
            registrable: false,
            inferred_table_type: None,
        }];
    }
    vec![DiscoveredEntry {
        display_name: "stub_parquet".into(),
        path: format!("{}/stub_parquet", path),
        kind: "table".into(),
        registrable: true,
        inferred_table_type: Some(TableType::ParquetFiles.as_str().to_string()),
    }]
}

fn iceberg_catalog_stub(path: &str) -> Vec<DiscoveredEntry> {
    let parts: Vec<&str> = path.split('/').filter(|p| !p.is_empty()).collect();
    match parts.len() {
        0 => vec![DiscoveredEntry {
            display_name: "default".into(),
            path: "default".into(),
            kind: "iceberg_namespace".into(),
            registrable: false,
            inferred_table_type: None,
        }],
        _ => vec![DiscoveredEntry {
            display_name: "stub_iceberg".into(),
            path: format!("{}/stub_iceberg", path),
            kind: "iceberg_table".into(),
            registrable: true,
            inferred_table_type: Some(TableType::ManagedIceberg.as_str().to_string()),
        }],
    }
}

// ---------------------------------------------------------------------------
// Register / bulk register / delete.
// ---------------------------------------------------------------------------

pub async fn register_virtual_table(
    state: &AppState,
    source_rid: &str,
    actor_id: Option<&str>,
    body: RegisterVirtualTableRequest,
    kind: RegistrationKind,
) -> Result<VirtualTableRow> {
    // Foundry doc § "Limitations of using virtual tables" — rejects
    // agent workers, agent-proxy / bucket-endpoint / self-service
    // private link egress policies. Bumps the Prometheus counter for
    // every rejection so SREs can alert on a misconfigured upstream.
    if let Err(reason) = source_validation::validate_for_virtual_tables(state, source_rid).await {
        state
            .metrics
            .record_source_validation_failure(reason.metric_reason());
        return Err(VirtualTableError::SourceIncompatible(reason));
    }

    let link = get_source_link(&state.db, source_rid).await?;
    if !link.virtual_tables_enabled {
        return Err(VirtualTableError::SourceNotEnabled(source_rid.to_string()));
    }
    let provider = link
        .provider_enum()
        .ok_or_else(|| VirtualTableError::InvalidProvider(link.provider.clone()))?;
    let table_type = TableType::parse(&body.table_type)
        .ok_or_else(|| VirtualTableError::InvalidTableType(body.table_type.clone()))?;

    let capabilities = capabilities_for(provider, table_type);
    let capabilities_json = serde_json::to_value(capabilities)
        .map_err(|err| VirtualTableError::SchemaInference(err.to_string()))?;

    let name = body
        .name
        .clone()
        .unwrap_or_else(|| body.locator.default_display_name());
    let locator_json = body.locator.canonicalize();

    let inferred = schema_inference::infer_for_provider(provider, &body.locator)
        .await
        .unwrap_or_default();
    let inferred_value = serde_json::to_value(&inferred)
        .map_err(|err| VirtualTableError::SchemaInference(err.to_string()))?;

    let id = Uuid::now_v7();
    let row: VirtualTableRow = sqlx::query_as(
        r#"INSERT INTO virtual_tables (
                id, source_rid, project_rid, name, parent_folder_rid,
                locator, table_type, schema_inferred, capabilities,
                update_detection_enabled, update_detection_interval_seconds,
                markings, properties, created_by
            )
            VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::jsonb, $9::jsonb,
                    FALSE, NULL, $10, '{}'::jsonb, $11)
            RETURNING *"#,
    )
    .bind(id)
    .bind(source_rid)
    .bind(&body.project_rid)
    .bind(&name)
    .bind(body.parent_folder_rid.as_deref())
    .bind(&locator_json)
    .bind(table_type.as_str())
    .bind(&inferred_value)
    .bind(&capabilities_json)
    .bind(&body.markings)
    .bind(actor_id)
    .fetch_one(&state.db)
    .await
    .map_err(translate_unique)?;

    state
        .metrics
        .record_registration(provider.as_str(), kind.as_str());
    state
        .metrics
        .observe_table_count(provider.as_str(), &row.project_rid);

    audit::record(
        &state.db,
        Some(source_rid),
        Some(row.id),
        "virtual_table.created",
        actor_id,
        json!({
            "kind": kind.as_str(),
            "provider": provider.as_str(),
            "table_type": table_type.as_str(),
            "project_rid": body.project_rid,
            "rid": row.rid,
        }),
    )
    .await;

    Ok(row)
}

pub async fn bulk_register(
    state: &AppState,
    source_rid: &str,
    actor_id: Option<&str>,
    body: BulkRegisterRequest,
) -> Result<BulkRegisterResponse> {
    let max_batch = state.max_bulk_register_batch.max(1);
    let mut registered = Vec::new();
    let mut errors = Vec::new();

    for entry in body.entries.into_iter().take(max_batch) {
        let merged = RegisterVirtualTableRequest {
            project_rid: body.project_rid.clone(),
            ..entry
        };
        let display_name = merged
            .name
            .clone()
            .unwrap_or_else(|| merged.locator.default_display_name());
        match register_virtual_table(state, source_rid, actor_id, merged, RegistrationKind::Bulk)
            .await
        {
            Ok(row) => registered.push(row),
            Err(err) => errors.push(BulkRegisterError {
                name: display_name,
                error: err.to_string(),
            }),
        }
    }

    Ok(BulkRegisterResponse { registered, errors })
}

pub async fn list_virtual_tables(
    pool: &PgPool,
    query: ListVirtualTablesQuery,
) -> Result<ListVirtualTablesResponse> {
    let limit = query.limit.unwrap_or(50).clamp(1, 500);
    // Simple cursor: created_at descending. Cursor encodes the boundary
    // `created_at` ISO-8601 string.
    let cursor_ts = query
        .cursor
        .as_deref()
        .and_then(|raw| chrono::DateTime::parse_from_rfc3339(raw).ok())
        .map(|ts| ts.with_timezone(&chrono::Utc));

    let mut sql = String::from("SELECT * FROM virtual_tables WHERE TRUE");
    let mut idx = 1;
    if query.project.is_some() {
        sql.push_str(&format!(" AND project_rid = ${idx}"));
        idx += 1;
    }
    if query.source.is_some() {
        sql.push_str(&format!(" AND source_rid = ${idx}"));
        idx += 1;
    }
    if query.name.is_some() {
        sql.push_str(&format!(" AND name ILIKE ${idx}"));
        idx += 1;
    }
    if query.table_type.is_some() {
        sql.push_str(&format!(" AND table_type = ${idx}"));
        idx += 1;
    }
    if cursor_ts.is_some() {
        sql.push_str(&format!(" AND created_at < ${idx}"));
        idx += 1;
    }
    sql.push_str(&format!(
        " ORDER BY created_at DESC LIMIT ${idx}"
    ));

    let mut q = sqlx::query_as::<_, VirtualTableRow>(&sql);
    if let Some(ref project) = query.project {
        q = q.bind(project);
    }
    if let Some(ref source) = query.source {
        q = q.bind(source);
    }
    if let Some(ref name) = query.name {
        q = q.bind(format!("%{name}%"));
    }
    if let Some(ref table_type) = query.table_type {
        q = q.bind(table_type);
    }
    if let Some(ts) = cursor_ts {
        q = q.bind(ts);
    }
    q = q.bind(limit + 1);

    let mut rows = q.fetch_all(pool).await?;
    let next_cursor = if rows.len() as i64 > limit {
        rows.pop()
            .map(|extra| extra.created_at.to_rfc3339())
    } else {
        None
    };

    Ok(ListVirtualTablesResponse {
        items: rows,
        next_cursor,
    })
}

pub async fn get_virtual_table(pool: &PgPool, rid: &str) -> Result<VirtualTableRow> {
    let row: Option<VirtualTableRow> =
        sqlx::query_as("SELECT * FROM virtual_tables WHERE rid = $1")
            .bind(rid)
            .fetch_optional(pool)
            .await?;
    row.ok_or_else(|| VirtualTableError::NotFound(rid.to_string()))
}

pub async fn delete_virtual_table(
    state: &AppState,
    rid: &str,
    actor_id: Option<&str>,
) -> Result<()> {
    let row = get_virtual_table(&state.db, rid).await?;
    let mut tx = state.db.begin().await?;
    sqlx::query("DELETE FROM virtual_table_imports WHERE virtual_table_id = $1")
        .bind(row.id)
        .execute(&mut *tx)
        .await?;
    let result = sqlx::query("DELETE FROM virtual_tables WHERE id = $1")
        .bind(row.id)
        .execute(&mut *tx)
        .await?;
    tx.commit().await?;

    if result.rows_affected() == 0 {
        return Err(VirtualTableError::NotFound(rid.to_string()));
    }

    audit::record(
        &state.db,
        Some(&row.source_rid),
        Some(row.id),
        "virtual_table.deleted",
        actor_id,
        json!({ "rid": row.rid }),
    )
    .await;

    Ok(())
}

pub async fn update_markings(
    state: &AppState,
    rid: &str,
    actor_id: Option<&str>,
    body: UpdateMarkingsRequest,
) -> Result<VirtualTableRow> {
    let existing = get_virtual_table(&state.db, rid).await?;
    let row: VirtualTableRow = sqlx::query_as(
        "UPDATE virtual_tables
            SET markings = $1, updated_at = NOW()
            WHERE rid = $2
            RETURNING *",
    )
    .bind(&body.markings)
    .bind(rid)
    .fetch_one(&state.db)
    .await?;

    audit::record(
        &state.db,
        Some(&row.source_rid),
        Some(row.id),
        "virtual_table.markings_updated",
        actor_id,
        json!({
            "previous_markings": existing.markings,
            "new_markings": body.markings,
        }),
    )
    .await;

    Ok(row)
}

pub async fn refresh_schema(
    state: &AppState,
    rid: &str,
    actor_id: Option<&str>,
) -> Result<VirtualTableRow> {
    let existing = get_virtual_table(&state.db, rid).await?;
    let link = get_source_link(&state.db, &existing.source_rid).await?;
    let provider = link
        .provider_enum()
        .ok_or_else(|| VirtualTableError::InvalidProvider(link.provider.clone()))?;
    let locator: Locator = serde_json::from_value(existing.locator.clone())
        .map_err(|err| VirtualTableError::SchemaInference(err.to_string()))?;

    let inferred = schema_inference::infer_for_provider(provider, &locator)
        .await
        .unwrap_or_default();
    let inferred_value = serde_json::to_value(&inferred)
        .map_err(|err| VirtualTableError::SchemaInference(err.to_string()))?;

    let row: VirtualTableRow = sqlx::query_as(
        "UPDATE virtual_tables
            SET schema_inferred = $1::jsonb, updated_at = NOW()
            WHERE rid = $2
            RETURNING *",
    )
    .bind(&inferred_value)
    .bind(rid)
    .fetch_one(&state.db)
    .await?;

    audit::record(
        &state.db,
        Some(&row.source_rid),
        Some(row.id),
        "virtual_table.schema_refreshed",
        actor_id,
        json!({
            "rid": rid,
            "column_count": inferred.len(),
        }),
    )
    .await;

    Ok(row)
}

#[allow(dead_code)]
pub async fn capabilities_summary(provider: SourceProvider) -> Vec<(TableType, Capabilities)> {
    crate::domain::capability_matrix::iter_cells()
        .filter(|(p, _, _)| *p == provider)
        .map(|(_, tt, c)| (tt, c))
        .collect()
}

#[allow(dead_code)]
pub fn ensure_recent(value: chrono::DateTime<Utc>) -> bool {
    Utc::now().signed_duration_since(value).num_seconds() < 86_400
}

fn translate_unique(error: sqlx::Error) -> VirtualTableError {
    if let sqlx::Error::Database(db) = &error {
        if let Some(constraint) = db.constraint() {
            if constraint == "virtual_tables_unique_locator" {
                return VirtualTableError::LocatorAlreadyRegistered;
            }
            if constraint == "virtual_tables_unique_name" {
                return VirtualTableError::NameAlreadyTaken;
            }
        }
    }
    VirtualTableError::Database(error)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn registration_kind_str_is_label_safe() {
        for kind in [
            RegistrationKind::Manual,
            RegistrationKind::Bulk,
            RegistrationKind::Auto,
        ] {
            let label = kind.as_str();
            assert!(label.chars().all(|c| c.is_ascii_lowercase()));
        }
    }

    #[test]
    fn warehouse_catalog_stub_drills_three_levels() {
        let l0 = warehouse_catalog_stub("");
        assert_eq!(l0.len(), 1);
        assert_eq!(l0[0].kind, "database");
        let l1 = warehouse_catalog_stub("main");
        assert_eq!(l1[0].kind, "schema");
        let l2 = warehouse_catalog_stub("main/public");
        assert_eq!(l2[0].kind, "table");
        assert!(l2[0].registrable);
    }

    #[test]
    fn object_store_catalog_stub_uses_file_prefix_at_root() {
        let l0 = object_store_catalog_stub("");
        assert_eq!(l0[0].kind, "file_prefix");
        let l1 = object_store_catalog_stub("openfoundry-default");
        assert!(l1[0].registrable);
        assert_eq!(
            l1[0].inferred_table_type.as_deref(),
            Some(TableType::ParquetFiles.as_str())
        );
    }
}

// Avoid a `Value` warning in builds where the generic helpers are
// emitted but not exercised by feature combinations.
#[allow(dead_code)]
fn _value_used(_: Value) {}
