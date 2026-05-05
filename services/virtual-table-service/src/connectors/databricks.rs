//! Dedicated Databricks connector for virtual tables.
//!
//! Foundry doc § "Supported sources" lists Databricks as a first-class
//! source for virtual tables (Tables, Views, Materialized Views,
//! External Delta, Managed Delta, Managed Iceberg). The default-build
//! body of this module ships the orchestration logic — config
//! validation, capability detection, locator parsing — and stubs the
//! live SQL Warehouse round trip behind a deterministic in-process
//! response. Set the `provider-databricks` cargo feature to swap the
//! stub for a real Databricks SQL client (P2.next).

use serde::Deserialize;
use serde_json::{Value, json};

use crate::domain::capability_matrix::{Capabilities, SourceProvider, TableType, capabilities_for};
use crate::models::registration::DiscoveredSource;

const CONNECTOR_NAME: &str = "databricks";
const DEFAULT_SOURCE_KIND: &str = "databricks_table";

/// Minimal set of fields we read from the source's `config` JSONB.
/// Forward-compatible — extra fields are ignored.
#[derive(Debug, Clone, Deserialize)]
pub struct DatabricksConfig {
    /// Workspace base URL (e.g. `https://acme.cloud.databricks.com`).
    /// Required.
    pub workspace_url: String,
    /// SQL Warehouse HTTP path (e.g. `/sql/1.0/warehouses/abc`).
    /// Required.
    pub warehouse_http_path: String,
    /// Authentication mode: `pat` (personal access token) or
    /// `oauth_m2m`. Required.
    pub auth_mode: String,
    /// Default Unity Catalog name. Optional.
    #[serde(default)]
    pub default_catalog: Option<String>,
}

pub fn validate_config(config: &Value) -> Result<DatabricksConfig, String> {
    let parsed: DatabricksConfig = serde_json::from_value(config.clone())
        .map_err(|e| format!("databricks: invalid config: {e}"))?;
    if !parsed.workspace_url.starts_with("https://") {
        return Err("databricks.workspace_url must be an https URL".into());
    }
    if !parsed.warehouse_http_path.starts_with("/sql/") {
        return Err(
            "databricks.warehouse_http_path must start with '/sql/' (SQL Warehouse path)".into(),
        );
    }
    if !matches!(parsed.auth_mode.as_str(), "pat" | "oauth_m2m") {
        return Err("databricks.auth_mode must be 'pat' or 'oauth_m2m'".into());
    }
    Ok(parsed)
}

/// Three-part Databricks identifier (`catalog.schema.table`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ThreePartName {
    pub catalog: String,
    pub schema: String,
    pub table: String,
}

impl ThreePartName {
    pub fn parse(value: &str) -> Result<Self, String> {
        let parts: Vec<_> = value.split('.').collect();
        if parts.len() != 3 || parts.iter().any(|p| p.is_empty()) {
            return Err(format!(
                "databricks identifier must be 'catalog.schema.table' (got '{value}')"
            ));
        }
        Ok(Self {
            catalog: parts[0].to_string(),
            schema: parts[1].to_string(),
            table: parts[2].to_string(),
        })
    }

    pub fn to_locator_value(&self) -> Value {
        json!({
            "kind": "tabular",
            "database": self.catalog,
            "schema": self.schema,
            "table": self.table,
        })
    }
}

/// Heuristic table_type detection from a `DESCRIBE TABLE EXTENDED`
/// payload. The string we receive in P2 is a deterministic stub; the
/// real `Type:` and `Provider:` fields are pulled from the response
/// in P2.next under the `provider-databricks` feature.
pub fn detect_table_type(describe_output: &str) -> TableType {
    let upper = describe_output.to_uppercase();
    if upper.contains("TYPE: VIEW") {
        TableType::View
    } else if upper.contains("TYPE: MATERIALIZED_VIEW")
        || upper.contains("TYPE: MATERIALIZED VIEW")
    {
        TableType::MaterializedView
    } else if upper.contains("PROVIDER: ICEBERG") {
        TableType::ManagedIceberg
    } else if upper.contains("PROVIDER: DELTA") {
        if upper.contains("LOCATION: EXTERNAL") || upper.contains("EXTERNAL: TRUE") {
            TableType::ExternalDelta
        } else {
            TableType::ManagedDelta
        }
    } else {
        TableType::Other
    }
}

/// Capability lookup for a Databricks table type. Always uses the
/// canonical matrix — never grants more than the doc allows.
pub fn capabilities_for_table(table_type: TableType) -> Capabilities {
    capabilities_for(SourceProvider::Databricks, table_type)
}

/// Stub `SHOW CATALOGS` / `SHOW SCHEMAS` / `SHOW TABLES` browse output
/// keyed off the configured workspace. The shape (one entry per
/// level) matches what the live SQL Warehouse returns; P2.next will
/// replace the body with a real `databricks-sql` round trip.
pub fn discover_catalogs(config: &DatabricksConfig) -> Vec<DiscoveredSource> {
    let catalog = config.default_catalog.clone().unwrap_or_else(|| "main".into());
    vec![DiscoveredSource {
        selector: catalog.clone(),
        display_name: catalog,
        source_kind: DEFAULT_SOURCE_KIND.into(),
        supports_sync: true,
        supports_zero_copy: true,
        source_signature: None,
        metadata: json!({
            "level": "catalog",
            "workspace_url": config.workspace_url,
        }),
    }]
}

pub fn discover_schemas(config: &DatabricksConfig, catalog: &str) -> Vec<DiscoveredSource> {
    vec![DiscoveredSource {
        selector: format!("{}.default", catalog),
        display_name: format!("{}.default", catalog),
        source_kind: DEFAULT_SOURCE_KIND.into(),
        supports_sync: true,
        supports_zero_copy: true,
        source_signature: None,
        metadata: json!({
            "level": "schema",
            "workspace_url": config.workspace_url,
        }),
    }]
}

pub fn discover_tables(
    config: &DatabricksConfig,
    catalog: &str,
    schema: &str,
) -> Vec<DiscoveredSource> {
    vec![DiscoveredSource {
        selector: format!("{}.{}.stub_databricks_table", catalog, schema),
        display_name: "stub_databricks_table".into(),
        source_kind: DEFAULT_SOURCE_KIND.into(),
        supports_sync: true,
        supports_zero_copy: true,
        source_signature: None,
        metadata: json!({
            "level": "table",
            "workspace_url": config.workspace_url,
            "table_type": TableType::ManagedDelta.as_str(),
        }),
    }]
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg() -> DatabricksConfig {
        DatabricksConfig {
            workspace_url: "https://acme.cloud.databricks.com".into(),
            warehouse_http_path: "/sql/1.0/warehouses/abc".into(),
            auth_mode: "pat".into(),
            default_catalog: Some("main".into()),
        }
    }

    #[test]
    fn validate_config_rejects_http_workspace_url() {
        let err = validate_config(&json!({
            "workspace_url": "http://insecure.example.com",
            "warehouse_http_path": "/sql/1.0/warehouses/abc",
            "auth_mode": "pat"
        }))
        .expect_err("must reject");
        assert!(err.contains("https"));
    }

    #[test]
    fn validate_config_rejects_unknown_auth_mode() {
        let err = validate_config(&json!({
            "workspace_url": "https://x",
            "warehouse_http_path": "/sql/1.0/warehouses/abc",
            "auth_mode": "anonymous"
        }))
        .expect_err("must reject");
        assert!(err.contains("auth_mode"));
    }

    #[test]
    fn three_part_name_parses_round_trip() {
        let n = ThreePartName::parse("main.public.events").expect("parse");
        assert_eq!(n.catalog, "main");
        assert_eq!(n.schema, "public");
        assert_eq!(n.table, "events");
        let val = n.to_locator_value();
        assert_eq!(val["database"], "main");
        assert_eq!(val["table"], "events");
    }

    #[test]
    fn three_part_name_rejects_short_identifiers() {
        assert!(ThreePartName::parse("main.public").is_err());
        assert!(ThreePartName::parse("public.events").is_err());
        assert!(ThreePartName::parse(".x.y").is_err());
    }

    #[test]
    fn detect_table_type_recognises_managed_delta() {
        assert_eq!(
            detect_table_type("Provider: delta\nLocation: dbfs:/foo"),
            TableType::ManagedDelta
        );
    }

    #[test]
    fn detect_table_type_recognises_external_delta() {
        assert_eq!(
            detect_table_type("Provider: DELTA\nLocation: external\n"),
            TableType::ExternalDelta
        );
    }

    #[test]
    fn detect_table_type_recognises_managed_iceberg() {
        assert_eq!(
            detect_table_type("Provider: ICEBERG\n"),
            TableType::ManagedIceberg
        );
    }

    #[test]
    fn detect_table_type_recognises_view() {
        assert_eq!(
            detect_table_type("Type: VIEW\nView Text: SELECT 1\n"),
            TableType::View
        );
    }

    #[test]
    fn capabilities_managed_delta_is_read_only() {
        let caps = capabilities_for_table(TableType::ManagedDelta);
        assert!(caps.read);
        assert!(!caps.write);
    }

    #[test]
    fn capabilities_external_delta_is_read_write() {
        let caps = capabilities_for_table(TableType::ExternalDelta);
        assert!(caps.read);
        assert!(caps.write);
    }

    #[test]
    fn discovery_levels_emit_one_entry_each() {
        let c = cfg();
        assert_eq!(discover_catalogs(&c).len(), 1);
        assert_eq!(discover_schemas(&c, "main").len(), 1);
        assert_eq!(discover_tables(&c, "main", "public").len(), 1);
    }
}
